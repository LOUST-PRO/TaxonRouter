// Package rules provides the table-driven PR classification engine.
// It is pure functions with no I/O — safe to use in both the MCP server
// and the auto-tagger daemon.
package rules

import (
	"math"
	"regexp"
	"sort"
	"strings"
)

// Classification is the output of one rules-only classification run.
type Classification struct {
	Labels        []string          `json:"labels"`
	ProjectFields map[string]string `json:"project_fields"`
	Confidence    float64           `json:"confidence"`
	Reasons       []string          `json:"reasons"`
	ManualReview  bool              `json:"manual_review"`
	ManualReasons []string          `json:"manual_reasons,omitempty"`
}

// PR is the minimum-viable input shape for classification.
type PR struct {
	Branch  string   `json:"branch"`
	Title   string   `json:"title"`
	Body    string   `json:"body"`
	Files   []string `json:"files"`
	AddDel  int      `json:"add_del"` // total LOC changed (added + deleted)
}

// Config holds thresholds and rule tables.
type Config struct {
	ConfidenceThreshold float64
	MaxLOCAuto         int
	TitleRules         []TitleRule
	BodyRules          []BodyRule
	PathRules          []PathRule
	EffortRules        []EffortRule
}

// DefaultConfig returns the v1 default configuration.
func DefaultConfig() Config {
	return Config{
		ConfidenceThreshold: 0.6,
		MaxLOCAuto:         1500,
		TitleRules:         DefaultTitleRules(),
		BodyRules:          DefaultBodyRules(),
		PathRules:          DefaultPathRules(),
		EffortRules:        DefaultEffortRules(),
	}
}

// TitleRule matches a branch prefix or title prefix.
type TitleRule struct {
	Prefix  string
	Label   string
	Phase   string
	Weight  float64
	TitleRE *regexp.Regexp
}

// MatchTitle returns all TitleRule matches for the given branch and title.
func MatchTitle(branch, title string, rules []TitleRule) []TitleMatch {
	var matches []TitleMatch
	for _, r := range rules {
		if r.TitleRE != nil {
			if r.TitleRE.MatchString(title) {
				matches = append(matches, TitleMatch{Label: r.Label, Phase: r.Phase, Weight: r.Weight, Prefix: r.Prefix})
			}
			continue
		}
		if strings.HasPrefix(branch, r.Prefix) {
			matches = append(matches, TitleMatch{Label: r.Label, Phase: r.Phase, Weight: r.Weight, Prefix: r.Prefix})
		}
	}
	return matches
}

// TitleMatch is the result of a title rule match.
type TitleMatch struct {
	Label  string
	Phase  string
	Weight float64
	Prefix string
}

// BodyRule matches keywords in the PR body.
type BodyRule struct {
	Keyword   string
	Label     string
	Category  string
	Weight    float64
	WordRE    *regexp.Regexp
	CaseInsensitive bool
}

// MatchBody returns all BodyRule matches for the given body.
func MatchBody(body string, rules []BodyRule) []BodyMatch {
	var matches []BodyMatch
	lower := strings.ToLower(body)
	for _, r := range rules {
		if r.WordRE != nil {
			if r.WordRE.MatchString(body) {
				matches = append(matches, BodyMatch{Label: r.Label, Category: r.Category, Weight: r.Weight, Keyword: r.Keyword})
			}
			continue
		}
		search := r.Keyword
		if r.CaseInsensitive {
			search = strings.ToLower(search)
			if !strings.Contains(lower, search) {
				continue
			}
		} else {
			if !strings.Contains(body, search) {
				continue
			}
		}
		matches = append(matches, BodyMatch{Label: r.Label, Category: r.Category, Weight: r.Weight, Keyword: r.Keyword})
	}
	return matches
}

// BodyMatch is the result of a body rule match.
type BodyMatch struct {
	Label    string
	Category string
	Weight   float64
	Keyword  string
}

// PathRule matches file paths against glob patterns.
type PathRule struct {
	Glob         string
	Label        string
	Lang         string
	ProxyAffects string
	TouchesInfra string
	TouchesCI    string
	DBSchema     string
	Weight       float64
}

// MatchPaths returns all PathRule matches for the given file list.
func MatchPaths(files []string, rules []PathRule) []PathMatch {
	var matches []PathMatch
	for _, f := range files {
		for _, r := range rules {
			if globMatch(r.Glob, f) {
				matches = append(matches, PathMatch{
					Glob:         r.Glob,
					Label:        r.Label,
					Lang:         r.Lang,
					ProxyAffects: r.ProxyAffects,
					TouchesInfra: r.TouchesInfra,
					TouchesCI:    r.TouchesCI,
					DBSchema:     r.DBSchema,
					Weight:       r.Weight,
				})
			}
		}
	}
	return matches
}

// PathMatch is the result of a path rule match.
type PathMatch struct {
	Glob         string
	Label        string
	Lang         string
	ProxyAffects string
	TouchesInfra string
	TouchesCI    string
	DBSchema     string
	Weight       float64
}

// EffortRule classifies PR size by lines of code.
type EffortRule struct {
	MaxLOC int
	Label  string
	Effort string
	Weight float64
}

// MatchEffort returns the EffortRule that matches the given LOC.
func MatchEffort(loc int, rules []EffortRule) (EffortMatch, bool) {
	for _, r := range rules {
		if loc <= r.MaxLOC {
			return EffortMatch{Label: r.Label, Effort: r.Effort, Weight: r.Weight}, true
		}
	}
	return EffortMatch{}, false
}

// EffortMatch is the result of an effort rule match.
type EffortMatch struct {
	Label  string
	Effort string
	Weight float64
}

// Combine runs all rule sets against pr and returns the merged Classification.
func Combine(pr PR, cfg Config) Classification {
	c := Classification{ProjectFields: map[string]string{}}

	var weights []float64
	var informativeMatches int

	for _, m := range MatchTitle(pr.Branch, pr.Title, cfg.TitleRules) {
		c.Labels = append(c.Labels, m.Label)
		if m.Phase != "" {
			c.ProjectFields["Phase"] = m.Phase
		}
		c.Reasons = append(c.Reasons, "branch:"+strings.TrimSuffix(m.Prefix, "/"))
		weights = append(weights, m.Weight)
		informativeMatches++
	}

	for _, m := range MatchBody(pr.Body, cfg.BodyRules) {
		c.Labels = append(c.Labels, m.Label)
		if m.Category != "" {
			c.ProjectFields["Category"] = m.Category
		}
		c.Reasons = append(c.Reasons, "keyword:"+m.Keyword)
		weights = append(weights, m.Weight)
		informativeMatches++
	}

	var pathMatches []PathMatch
	for _, m := range MatchPaths(pr.Files, cfg.PathRules) {
		pathMatches = append(pathMatches, m)
	}
	docsOnly := false
	for _, m := range pathMatches {
		if m.Label == "docs-only" {
			docsOnly = true
			break
		}
	}
	for _, m := range pathMatches {
		if m.Label == "docs-only" {
			c.Labels = append(c.Labels, m.Label)
			c.Reasons = append(c.Reasons, "path:"+m.Glob)
			weights = append(weights, m.Weight)
			informativeMatches++
			continue
		}
		if docsOnly && strings.HasPrefix(m.Label, "lang:") {
			continue
		}
		c.Labels = append(c.Labels, m.Label)
		if m.Lang != "" {
			c.ProjectFields["Lang"] = m.Lang
		}
		if m.ProxyAffects != "" {
			c.ProjectFields["Proxy-affects"] = m.ProxyAffects
		}
		if m.TouchesInfra != "" {
			c.ProjectFields["Touches-infra"] = m.TouchesInfra
		}
		if m.TouchesCI != "" {
			c.ProjectFields["Touches-CI"] = m.TouchesCI
		}
		if m.DBSchema != "" {
			c.ProjectFields["DB-schema"] = m.DBSchema
		}
		c.Reasons = append(c.Reasons, "path:"+m.Glob)
		weights = append(weights, m.Weight)
		informativeMatches++
	}

	if m, ok := MatchEffort(pr.AddDel, cfg.EffortRules); ok {
		c.ProjectFields["Effort"] = m.Effort
		c.Reasons = append(c.Reasons, "effort:"+m.Effort)
		weights = append(weights, m.Weight)
	}

	c.Labels = dedupeSorted(c.Labels)
	c.Reasons = dedupeSorted(c.Reasons)
	c.Confidence = confidence(weights)

	switch {
	case informativeMatches == 0:
		c.ManualReview = true
		c.ManualReasons = append(c.ManualReasons, "zero_rules_matched")
	case pr.AddDel >= cfg.MaxLOCAuto:
		c.ManualReview = true
		c.ManualReasons = append(c.ManualReasons, "loc_over_threshold")
	case c.Confidence < cfg.ConfidenceThreshold:
		c.ManualReview = true
		c.ManualReasons = append(c.ManualReasons, "confidence_below_threshold")
	}

	return c
}

func confidence(weights []float64) float64 {
	if len(weights) == 0 {
		return 0.0
	}
	prod := 1.0
	for _, w := range weights {
		if w <= 0 {
			return 0.0
		}
		prod *= w
	}
	return math.Pow(prod, 1.0/float64(len(weights)))
}

func dedupeSorted(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// globMatch is a simple glob matcher (* matches any number of chars, ? matches one).
// It does not support character classes like [abc].
func globMatch(pattern, name string) bool {
	// Convert glob to regex.
	pattern = regexp.QuoteMeta(pattern)
	pattern = strings.ReplaceAll(pattern, `\*`, ".*")
	pattern = strings.ReplaceAll(pattern, `\?`, ".")
	pattern = "^" + pattern + "$"
	matched, _ := regexp.MatchString(pattern, name)
	return matched
}
