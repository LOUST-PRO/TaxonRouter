package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// verifySignature
// ---------------------------------------------------------------------------

func TestVerifySignature(t *testing.T) {
	secret := "test-secret"
	body := []byte(`{"action":"opened"}`)

	tests := []struct {
		name   string
		secret string
		sig    string
		body   []byte
		want   string
	}{
		{
			name:   "valid",
			secret: secret,
			sig:    computeHMAC(secret, body),
			body:   body,
			want:   "",
		},
		{
			name:   "empty_secret",
			secret: "",
			sig:    computeHMAC(secret, body),
			body:   body,
			want:   "webhook: empty secret",
		},
		{
			name:   "missing_sig",
			secret: secret,
			sig:    "",
			body:   body,
			want:   "webhook: missing signature header",
		},
		{
			name:   "bad_encoding",
			secret: secret,
			sig:    "not-hex",
			body:   body,
			want:   "webhook: invalid signature encoding",
		},
		{
			name:   "mismatch",
			secret: secret,
			sig:    "sha256=0000000000000000000000000000000000000000000000000000000000000000",
			body:   body,
			want:   "webhook: signature mismatch",
		},
		{
			name:   "with_prefix",
			secret: secret,
			sig:    "sha256=" + computeHMAC(secret, body),
			body:   body,
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := verifySignature(tt.secret, tt.sig, tt.body)
			if tt.want == "" {
				if err != nil {
					t.Errorf("verifySignature() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Errorf("verifySignature() = nil, want error containing %q", tt.want)
				return
			}
			if !errors.Is(err, errors.New(tt.want)) && !containsString(err.Error(), tt.want) {
				t.Errorf("verifySignature() = %q, want error containing %q", err.Error(), tt.want)
			}
		})
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && contains(s, substr))
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func computeHMAC(secret string, body []byte) string {	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// ---------------------------------------------------------------------------
// extractOwnerRepo
// ---------------------------------------------------------------------------

func TestExtractOwnerRepo(t *testing.T) {
	tests := []struct {
		name  string
		raw   []byte
		wantO string
		wantR string
	}{
		{
			name:  "valid",
			raw:   []byte(`{"repository":{"full_name":"louzt/TaxonRouter"}}`),
			wantO: "louzt",
			wantR: "TaxonRouter",
		},
		{
			name:  "with_spaces",
			raw:   []byte(`{"repository":{"full_name":" louzt / TaxonRouter "}}`),
			wantO: "louzt",
			wantR: "TaxonRouter",
		},
		{
			name:  "missing_repository",
			raw:   []byte(`{}`),
			wantO: "",
			wantR: "",
		},
		{
			name:  "invalid_owner_chars",
			raw:   []byte(`{"repository":{"full_name":"lou!zt/TaxonRouter"}}`),
			wantO: "",
			wantR: "",
		},
		{
			name:  "empty_full_name",
			raw:   []byte(`{"repository":{"full_name":""}}`),
			wantO: "",
			wantR: "",
		},
		{
			name:  "malformed_json",
			raw:   []byte(`not json`),
			wantO: "",
			wantR: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o, r := extractOwnerRepo(tt.raw)
			if o != tt.wantO || r != tt.wantR {
				t.Errorf("extractOwnerRepo(%q) = (%q, %q), want (%q, %q)", string(tt.raw), o, r, tt.wantO, tt.wantR)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// extractPRNumber
// ---------------------------------------------------------------------------

func TestExtractPRNumber(t *testing.T) {
	tests := []struct {
		name string
		raw  []byte
		want int
	}{
		{"valid", []byte(`{"number":42}`), 42},
		{"zero", []byte(`{"number":0}`), 0},
		{"missing", []byte(`{}`), 0},
		{"malformed", []byte(`not json`), 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractPRNumber(tt.raw)
			if got != tt.want {
				t.Errorf("extractPRNumber(%q) = %d, want %d", string(tt.raw), got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// normalizePullRequest
// ---------------------------------------------------------------------------

func TestNormalizePullRequest(t *testing.T) {
	tests := []struct {
		name    string
		raw     []byte
		want    PR
		wantErr string
	}{
		{
			name: "opened",
			raw: []byte(`{
				"action":"opened",
				"pull_request":{
					"title":"feat: add something",
					"body":"implements a feature",
					"head":{"ref":"feat/new"},
					"additions":50,
					"deletions":10
				}
			}`),
			want: PR{
				Branch: "feat/new",
				Title:  "feat: add something",
				Body:   "implements a feature",
				AddDel: 60,
			},
		},
		{
			name:    "closed_action",
			raw:     []byte(`{"action":"closed","pull_request":{"title":"t","body":"","head":{"ref":"x"},"additions":0,"deletions":0}}`),
			wantErr: "unsupported",
		},
		{
			name:    "malformed_json",
			raw:     []byte(`not json`),
			wantErr: "parse pull_request payload",
		},
		{
			name: "large_body_truncated",
			raw: []byte(`{
				"action":"opened",
				"pull_request":{
					"title":"t",
					"body":"` + strings.Repeat("x", 60*1024) + `",
					"head":{"ref":"x"},
					"additions":0,
					"deletions":0
				}
			}`),
			want: PR{
				Branch: "x",
				Title:  "t",
				AddDel: 0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pr, err := normalizePullRequest(tt.raw)
			if tt.wantErr != "" {
				if err == nil {
					t.Errorf("normalizePullRequest() = nil, want error containing %q", tt.wantErr)
					return
				}
				if !containsString(err.Error(), tt.wantErr) {
					t.Errorf("normalizePullRequest() error = %q, want %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Errorf("normalizePullRequest() = %v, want nil", err)
				return
			}
			if pr.Branch != tt.want.Branch || pr.Title != tt.want.Title || pr.AddDel != tt.want.AddDel {
				t.Errorf("normalizePullRequest() = %+v, want %+v", pr, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// classifyRequest.toPR
// ---------------------------------------------------------------------------

func TestClassifyRequestToPR(t *testing.T) {
	tests := []struct {
		name    string
		req     classifyRequest
		want    PR
		wantErr string
	}{
		{
			name: "full_request",
			req: classifyRequest{
				Title:  "feat: new feature",
				Body:   "implements x",
				Branch: "feat/new",
				Files:  []string{"pkg/foo.go"},
				AddDel: 100,
			},
			want: PR{
				Branch:      "feat/new",
				Title:       "feat: new feature",
				Body:        "implements x",
				Files:       []string{"pkg/foo.go"},
				AddDel:      100,
				DiffExcerpt: "implements x",
			},
		},
		{
			name: "uses_diff_excerpt",
			req: classifyRequest{
				Title:       "fix: bug",
				DiffExcerpt: "diff content here",
				AddDel:      20,
			},
			want: PR{
				Title:       "fix: bug",
				DiffExcerpt: "diff content here",
				AddDel:      20,
			},
		},
		{
			name:    "empty",
			req:     classifyRequest{},
			wantErr: "must include title or body",
		},
		{
			name: "trims_whitespace",
			req: classifyRequest{
				Title:  "  feat: spaces  ",
				Branch: "  feat/spaces  ",
			},
			want: PR{
				Title:  "feat: spaces",
				Branch: "feat/spaces",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pr, err := tt.req.toPR()
			if tt.wantErr != "" {
				if err == nil {
					t.Errorf("toPR() = nil, want error %q", tt.wantErr)
					return
				}
				if !containsString(err.Error(), tt.wantErr) {
					t.Errorf("toPR() error = %q, want %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Errorf("toPR() = %v, want nil", err)
				return
			}
			if pr.Title != tt.want.Title || pr.Branch != tt.want.Branch || pr.AddDel != tt.want.AddDel {
				t.Errorf("toPR() = %+v, want %+v", pr, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// suggestionQueue
// ---------------------------------------------------------------------------

func TestSuggestionQueue(t *testing.T) {
	q := newSuggestionQueue()
	now := time.Now()

	// Add 3 items
	for i := 0; i < 3; i++ {
		q.add(suggestion{
			PR:             PR{Branch: "branch", Title: "title"},
			Classification: Classification{Labels: []string{"label"}},
			ClassifiedAt:  now.Add(time.Duration(i) * time.Minute),
		})
	}

	snap := q.snapshot(time.Time{}, 10)
	if len(snap) != 3 {
		t.Errorf("snapshot(all) = %d items, want 3", len(snap))
	}

	// Filter by since
	sinceSnap := q.snapshot(now.Add(2*time.Minute), 10)
	if len(sinceSnap) != 1 {
		t.Errorf("snapshot(since) = %d items, want 1", len(sinceSnap))
	}

	// Limit
	limited := q.snapshot(time.Time{}, 2)
	if len(limited) != 2 {
		t.Errorf("snapshot(limit=2) = %d items, want 2", len(limited))
	}
}

// ---------------------------------------------------------------------------
// handler integration (httptest)
// ---------------------------------------------------------------------------

type mockClassifier struct {
	classifyFn func(context.Context, PR) (Classification, error)
}

func (m *mockClassifier) Classify(ctx context.Context, pr PR) (Classification, error) {
	if m.classifyFn != nil {
		return m.classifyFn(ctx, pr)
	}
	return Classification{Labels: []string{"test-label"}, Confidence: 0.9}, nil
}

func TestHandleWebhook_MethodNotAllowed(t *testing.T) {
	srv := NewServer(Config{
		WebhookSecret: "x",
		ClassifierEngine: &mockClassifier{},
	})
	mux := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/webhook", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /webhook = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleWebhook_Healthz(t *testing.T) {
	srv := NewServer(Config{ClassifierEngine: &mockClassifier{}})
	mux := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET /healthz = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestHandleWebhook_ClassifyEndpoint(t *testing.T) {
	var captured PR
	srv := NewServer(Config{
		WebhookSecret: "x",
		ClassifierEngine: &mockClassifier{
			classifyFn: func(ctx context.Context, pr PR) (Classification, error) {
				captured = pr
				return Classification{Labels: []string{"security"}, Confidence: 0.95}, nil
			},
		},
	})
	mux := srv.Handler()

	body, _ := json.Marshal(map[string]any{
		"title":  "Fix security bug",
		"body":   "fixes a security vulnerability",
		"branch": "fix/security",
		"files":  []string{"internal/auth.go"},
		"add_del": 45,
	})
	req := httptest.NewRequest(http.MethodPost, "/classify", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("POST /classify = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}

	if captured.Title != "Fix security bug" {
		t.Errorf("captured.Title = %q, want %q", captured.Title, "Fix security bug")
	}
	if captured.Branch != "fix/security" {
		t.Errorf("captured.Branch = %q, want %q", captured.Branch, "fix/security")
	}
}

func TestHandleWebhook_Readyz(t *testing.T) {
	srv := NewServer(Config{
		ClassifierEngine: &mockClassifier{},
	})
	mux := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET /readyz = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestHandleWebhook_Suggestions(t *testing.T) {
	srv := NewServer(Config{
		ClassifierEngine: &mockClassifier{},
	})
	mux := srv.Handler()

	// GET without since
	req := httptest.NewRequest(http.MethodGet, "/admin/suggestions", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET /admin/suggestions = %d, want %d", w.Code, http.StatusOK)
	}

	// GET with invalid since
	req = httptest.NewRequest(http.MethodGet, "/admin/suggestions?since=not-a-time", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("GET /admin/suggestions?since=bad = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleWebhook_SuggestionsPOST(t *testing.T) {
	srv := NewServer(Config{ClassifierEngine: &mockClassifier{}})
	mux := srv.Handler()

	req := httptest.NewRequest(http.MethodPost, "/admin/suggestions", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /admin/suggestions = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleWebhook_ClassifyEmpty(t *testing.T) {
	srv := NewServer(Config{
		WebhookSecret: "x",
		ClassifierEngine: &mockClassifier{},
	})
	mux := srv.Handler()

	body, _ := json.Marshal(map[string]any{"title": "", "body": ""})
	req := httptest.NewRequest(http.MethodPost, "/classify", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("POST /classify empty = %d, want %d", w.Code, http.StatusBadRequest)
	}
}
