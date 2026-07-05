package classifier

import (
	"testing"
)

func TestMatchTitle_BranchPrefix(t *testing.T) {
	cfg := DefaultConfig()
	tests := []struct {
		branch string
		title  string
		want   string
	}{
		{"feat/new-feature", "any title", "enhancement"},
		{"fix/auth-bug", "any title", "fix"},
		{"hardening/rate-limit", "any title", "hardening"},
		{"chore/update-deps", "any title", "chore"},
		{"refactor/parser", "any title", "refactor"},
		{"audit/security", "any title", "audit"},
		{"experiment/new-idea", "any title", "experiment"},
		{"lab/spike-ai", "any title", "lab"},
		{"baremetal/optimization", "any title", "baremetal"},
		{"review/codemotion", "any title", "review"},
		{"spike/explore-db", "any title", "spike"},
		{"main", "Fix: auth bug", "fix"},
		{"main", "feat: new feature", "enhancement"},
		{"main", "Harden TLS config", "hardening"},
	}

	for _, tt := range tests {
		t.Run(tt.branch+"/"+tt.want, func(t *testing.T) {
			matches := MatchTitle(tt.branch, tt.title, cfg.TitleRules)
			if len(matches) == 0 {
				t.Errorf("MatchTitle(%q, %q) = [], want label %q", tt.branch, tt.title, tt.want)
				return
			}
			if matches[0].Label != tt.want {
				t.Errorf("MatchTitle(%q, %q)[0].Label = %q, want %q", tt.branch, tt.title, matches[0].Label, tt.want)
			}
		})
	}
}

func TestMatchBody_Keywords(t *testing.T) {
	cfg := DefaultConfig()
	tests := []struct {
		body    string
		want    string // any label from matches
	}{
		{"This PR fixes a security vulnerability in the auth module", "security"},
		{"Breaking change: the API signature has changed", "breaking"},
		{"Adds performance improvements to the hot path", "performance"},
		{"Updates documentation for the new API", "docs"},
		{"Adds unit tests for the classifier", "tests"},
		{"Configures nginx reverse proxy for the API", "proxy-affects"},
		{"Adds database migration for new schema", "db-schema"},
		{"Adds TLS hardening for the webhook endpoint", "hardening"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			matches := MatchBody(tt.body, cfg.BodyRules)
			found := false
			for _, m := range matches {
				if m.Label == tt.want {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("MatchBody(%q) missing label %q, got %v", tt.body, tt.want, matches)
			}
		})
	}
}

func TestMatchPaths(t *testing.T) {
	cfg := DefaultConfig()
	tests := []struct {
		files []string
		want  string // any label/lang/field from matches
	}{
		{[]string{"pkg/github/client.go"}, "lang:go"},
		{[]string{"internal/webhook/webhook.go"}, "lang:go"},
		{[]string{".github/workflows/ci.yml"}, "ci"},
		{[]string{"nginx/loust.pro.conf"}, "nginx"},
		{[]string{"prisma/schema.prisma"}, "prisma"},
		{[]string{"README.md"}, "docs-only"},
		{[]string{"docs/architecture.md"}, "docs-only"},
		{[]string{"pkg/rules/config.go"}, "lang:go"},
		{[]string{"internal/llm/hybrid.go"}, "lang:go"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			matches := MatchPaths(tt.files, cfg.PathRules)
			found := false
			for _, m := range matches {
				if m.Label == tt.want || m.Lang == tt.want || m.ProxyAffects == tt.want || m.TouchesCI == tt.want || m.TouchesInfra == tt.want || m.DBSchema == tt.want {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("MatchPaths(%v) missing %q, got %v", tt.files, tt.want, matches)
			}
		})
	}
}

func TestMatchEffort(t *testing.T) {
	cfg := DefaultConfig()
	tests := []struct {
		loc  int
		want string
	}{
		{50, "M-days"},    // <=100 → M-days
		{150, "M-days"},   // <=200 → M-days
		{300, "L-days"},   // <=500 → L-days
		{800, "XL-weeks"}, // <=1000 → XL-weeks
		{1200, "XL-weeks"},
		{2000, "XXL-months"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			m, ok := MatchEffort(tt.loc, cfg.EffortRules)
			if !ok {
				t.Errorf("MatchEffort(%d) = false, want true", tt.loc)
				return
			}
			if m.Effort != tt.want {
				t.Errorf("MatchEffort(%d).Effort = %q, want %q", tt.loc, m.Effort, tt.want)
			}
		})
	}
}

func TestCombine_SecurityPR(t *testing.T) {
	cfg := DefaultConfig()
	pr := PR{
		Branch: "fix/webhook-cve",
		Title:  "Fix critical CVE in webhook handler",
		Body:   "This PR fixes a security vulnerability where HMAC validation was bypassed",
		Files:  []string{"internal/webhook/webhook.go"},
		AddDel: 45,
	}

	got := Combine(pr, cfg)

	if !contains(got.Labels, "security") {
		t.Errorf("Combine security PR: missing 'security' label, got %v", got.Labels)
	}
	if got.ProjectFields["Category"] != "security" {
		t.Errorf("Combine security PR: Category = %q, want 'security'", got.ProjectFields["Category"])
	}
	if !got.ManualReview {
		t.Errorf("Combine security PR: ManualReview = false, want true (confidence below threshold)")
	}
}

func TestCombine_DocsOnly(t *testing.T) {
	cfg := DefaultConfig()
	pr := PR{
		Branch: "docs/update-readme",
		Title:  "docs: update README with new instructions",
		Body:   "Updates the README with installation instructions",
		Files:  []string{"README.md"},
		AddDel: 50,
	}

	got := Combine(pr, cfg)

	if !contains(got.Labels, "docs-only") {
		t.Errorf("Combine docs PR: missing 'docs-only' label, got %v", got.Labels)
	}
}

func TestCombine_LargePRManualReview(t *testing.T) {
	cfg := DefaultConfig()
	pr := PR{
		Branch: "feat/big-feature",
		Title:  "feat: add complete new feature",
		Body:   "Adds a large feature",
		Files:  []string{"pkg/feature/main.go", "pkg/feature/utils.go"},
		AddDel: 2000, // over MaxLOCAuto=1500
	}

	got := Combine(pr, cfg)

	if !got.ManualReview {
		t.Errorf("Combine large PR: ManualReview = false, want true (over MaxLOCAuto)")
	}
	if !contains(got.ManualReasons, "loc_over_threshold") {
		t.Errorf("Combine large PR: missing 'loc_over_threshold', got %v", got.ManualReasons)
	}
}

func TestCombine_ZeroMatches(t *testing.T) {
	cfg := DefaultConfig()
	pr := PR{
		Branch: "whatever",
		Title:  "stuff",
		Body:   "changes",
		Files:  []string{"some/random/path.go"},
		AddDel: 10,
	}

	got := Combine(pr, cfg)

	if !got.ManualReview {
		t.Errorf("Combine zero-match PR: ManualReview = false, want true")
	}
	// Zero matches → confidence 0 → below threshold → manual_review = true
	// with reason "confidence_below_threshold" (since zero matches < threshold)
}

func TestDedupSorted(t *testing.T) {
	tests := []struct {
		input []string
		want  []string
	}{
		{nil, nil},
		{[]string{}, nil},
		{[]string{"a"}, []string{"a"}},
		{[]string{"b", "a", "b", "a", "c"}, []string{"a", "b", "c"}},
		{[]string{"z", "a", "m", "z", "a"}, []string{"a", "m", "z"}},
	}

	for _, tt := range tests {
		got := dedupeSorted(tt.input)
		if !eq(got, tt.want) {
			t.Errorf("dedupeSorted(%v) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestGlobMatch(t *testing.T) {
	tests := []struct {
		pattern string
		name   string
		want   bool
	}{
		{"*.go", "foo.go", true},
		{"*.go", "foo.txt", false},
		{"pkg/**/*.go", "pkg/github/client.go", true},
		{".github/workflows/*.yml", ".github/workflows/ci.yml", true},
		{"README.md", "README.md", true},
		{"docs/*.md", "docs/architecture.md", true},
		{"internal/*/webhook.go", "internal/webhook/webhook.go", true},
		{"build/**/*.go", "build/pkg/foo.go", true},
		{"**/Dockerfile", "build/Dockerfile", true},
	}

	for _, tt := range tests {
		got := globMatch(tt.pattern, tt.name)
		if got != tt.want {
			t.Errorf("globMatch(%q, %q) = %v, want %v", tt.pattern, tt.name, got, tt.want)
		}
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
