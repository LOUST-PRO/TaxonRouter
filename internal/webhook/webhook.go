// Package webhook provides the HTTP listener for GitHub webhook events.
package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Config wires the handler with its dependencies.
type Config struct {
	ClassifierEngine Classifier
	WebhookSecret    string
	Logger           *slog.Logger
	Now              func() time.Time
	MaxBodyBytes     int64
	ApplyPipeline    interface {
		Submit(ctx context.Context, owner string, repo string, number int, labels []string, fields map[string]string) error
	}
	ApplyToken     string
	ProjectNumber  int
}

// Classifier is the minimum interface the webhook needs from a classification engine.
type Classifier interface {
	Classify(ctx context.Context, pr PR) (Classification, error)
}

// PR is the canonical input shape for the classifier.
type PR struct {
	Branch      string   `json:"branch"`
	Title       string   `json:"title"`
	Body        string   `json:"body"`
	Files       []string `json:"files"`
	AddDel      int      `json:"add_del"`
	DiffExcerpt string   `json:"diff_excerpt,omitempty"`
}

// Classification is the output from the classifier.
type Classification struct {
	Labels        []string          `json:"labels"`
	ProjectFields map[string]string `json:"project_fields"`
	Confidence    float64           `json:"confidence"`
	Reasons       []string          `json:"reasons"`
	ManualReview  bool              `json:"manual_review"`
	ManualReasons []string          `json:"manual_reasons,omitempty"`
}

// Server is the HTTP layer.
type Server struct {
	cfg    Config
	logger *slog.Logger
	now    func() time.Time
	queue  *suggestionQueue
}

// NewServer creates a Server with sensible defaults.
func NewServer(cfg Config) *Server {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = 1 << 20
	}
	return &Server{
		cfg:    cfg,
		logger: cfg.Logger,
		now:    cfg.Now,
		queue:  newSuggestionQueue(),
	}
}

// Handler returns the configured *http.ServeMux.
func (s *Server) Handler() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", s.handleWebhook)
	mux.HandleFunc("/classify", s.handleClassify)
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/readyz", s.handleReadyz)
	mux.HandleFunc("/admin/suggestions", s.handleSuggestions)
	return mux
}

func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST")
		return
	}
	if s.cfg.WebhookSecret == "" {
		writeJSONError(w, http.StatusInternalServerError, "config_error", "WEBHOOK_SECRET not configured")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, s.cfg.MaxBodyBytes+1))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "read_failed", err.Error())
		return
	}
	if int64(len(body)) > s.cfg.MaxBodyBytes {
		writeJSONError(w, http.StatusRequestEntityTooLarge, "body_too_large", "request body exceeds limit")
		return
	}

	sig := r.Header.Get("X-Hub-Signature-256")
	if err := verifySignature(s.cfg.WebhookSecret, sig, body); err != nil {
		s.logger.Warn("webhook signature verify failed", slog.String("err", err.Error()))
		writeJSONError(w, http.StatusUnauthorized, "invalid_signature", "HMAC mismatch")
		return
	}

	event := r.Header.Get("X-GitHub-Event")
	if event != "pull_request" {
		writeJSON(w, http.StatusAccepted, map[string]any{"status": "ignored", "reason": "event not handled", "event": event})
		return
	}

	pr, err := normalizePullRequest(body)
	if err != nil {
		if errors.Is(err, errUnsupportedAction) {
			writeJSON(w, http.StatusAccepted, map[string]any{"status": "ignored", "reason": "unsupported action"})
			return
		}
		writeJSONError(w, http.StatusBadRequest, "parse_failed", err.Error())
		return
	}

	owner, repo := extractOwnerRepo(body)
	number := extractPRNumber(body)

	class, err := s.cfg.ClassifierEngine.Classify(r.Context(), pr)
	if err != nil {
		s.logger.Error("classify failed", slog.String("err", err.Error()), slog.String("branch", pr.Branch))
		writeJSONError(w, http.StatusInternalServerError, "classify_failed", err.Error())
		return
	}

	if class.ManualReview {
		s.queue.add(suggestion{PR: pr, Classification: class, ClassifiedAt: s.now()})
	}

	if s.cfg.ApplyPipeline != nil && owner != "" && number > 0 {
		if err := s.cfg.ApplyPipeline.Submit(r.Context(), owner, repo, number, class.Labels, class.ProjectFields); err != nil {
			s.logger.Warn("apply submit failed", slog.String("err", err.Error()), slog.String("repo", owner+"/"+repo), slog.Int("pr", number))
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "classified",
		"labels":         class.Labels,
		"project":        class.ProjectFields,
		"confidence":     class.Confidence,
		"reasons":        class.Reasons,
		"manual_review":  class.ManualReview,
		"manual_reasons": class.ManualReasons,
	})
}

func (s *Server) handleClassify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, s.cfg.MaxBodyBytes+1))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "read_failed", err.Error())
		return
	}
	if int64(len(body)) > s.cfg.MaxBodyBytes {
		writeJSONError(w, http.StatusRequestEntityTooLarge, "body_too_large", "request body exceeds limit")
		return
	}
	var req classifyRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "parse_failed", err.Error())
		return
	}
	pr, err := req.toPR()
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	class, err := s.cfg.ClassifierEngine.Classify(r.Context(), pr)
	if err != nil {
		s.logger.Error("classify failed", slog.String("err", err.Error()))
		writeJSONError(w, http.StatusInternalServerError, "classify_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "classified",
		"labels":         class.Labels,
		"project":        class.ProjectFields,
		"confidence":     class.Confidence,
		"reasons":        class.Reasons,
		"manual_review":  class.ManualReview,
		"manual_reasons": class.ManualReasons,
	})
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) handleReadyz(w http.ResponseWriter, _ *http.Request) {
	if s.cfg.ClassifierEngine == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "not_ready", "classifier engine not configured")
		return
	}
	pr := PR{}
	class, err := s.cfg.ClassifierEngine.Classify(context.Background(), pr)
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "classifier_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ready", "labels": class.Labels})
}

func (s *Server) handleSuggestions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use GET")
		return
	}
	since := time.Time{}
	if v := strings.TrimSpace(r.URL.Query().Get("since")); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_since", "must be RFC3339: "+err.Error())
			return
		}
		since = t
	}
	limit := 100
	out := s.queue.snapshot(since, limit)
	writeJSON(w, http.StatusOK, map[string]any{"count": len(out), "suggestions": out})
}

type suggestion struct {
	PR            PR
	Classification Classification
	ClassifiedAt  time.Time
}

type suggestionQueue struct {
	mu       sync.Mutex
	items    []suggestion
	queueCap int
}

const queueCap = 1024

func newSuggestionQueue() *suggestionQueue {
	return &suggestionQueue{queueCap: queueCap}
}

func (q *suggestionQueue) add(s suggestion) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items = append(q.items, s)
	if len(q.items) > q.queueCap {
		q.items = q.items[len(q.items)-q.queueCap:]
	}
}

func (q *suggestionQueue) snapshot(since time.Time, limit int) []map[string]any {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]map[string]any, 0, limit)
	for i := len(q.items) - 1; i >= 0 && len(out) < limit; i-- {
		it := q.items[i]
		if !since.IsZero() && it.ClassifiedAt.Before(since) {
			continue
		}
		out = append(out, map[string]any{
			"branch":          it.PR.Branch,
			"title":           it.PR.Title,
			"labels":          it.Classification.Labels,
			"project":         it.Classification.ProjectFields,
			"confidence":      it.Classification.Confidence,
			"reasons":         it.Classification.Reasons,
			"manual_review":   it.Classification.ManualReview,
			"manual_reasons":  it.Classification.ManualReasons,
			"classified_at":   it.ClassifiedAt.UTC().Format(time.RFC3339),
		})
	}
	return out
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeJSONError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]any{"error": code, "message": msg})
}

var validOwnerRepoRe = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

func extractOwnerRepo(raw []byte) (owner, repo string) {
	var p struct {
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	}
	if json.Unmarshal(raw, &p) != nil || p.Repository.FullName == "" {
		return "", ""
	}
	parts := strings.SplitN(p.Repository.FullName, "/", 2)
	if len(parts) != 2 {
		return "", ""
	}
	owner, repo = strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	if !validOwnerRepoRe.MatchString(owner) || !validOwnerRepoRe.MatchString(repo) {
		return "", ""
	}
	return owner, repo
}

func extractPRNumber(raw []byte) int {
	var p struct {
		Number int `json:"number"`
	}
	if json.Unmarshal(raw, &p) != nil {
		return 0
	}
	return p.Number
}

var errUnsupportedAction = errors.New("webhook: unsupported pull_request action")

func normalizePullRequest(raw []byte) (PR, error) {
	var p struct {
		Action string `json:"action"`
		PullRequest struct {
			Title    string `json:"title"`
			Body     string `json:"body"`
			HeadRef  struct {
				Ref string `json:"ref"`
			} `json:"head"`
			Additions int `json:"additions"`
			Deletions int `json:"deletions"`
		} `json:"pull_request"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return PR{}, fmt.Errorf("parse pull_request payload: %w", err)
	}
	if p.Action != "opened" && p.Action != "synchronize" && p.Action != "reopened" && p.Action != "edited" {
		return PR{}, errUnsupportedAction
	}
	body := p.PullRequest.Body
	if len(body) > 50*1024 {
		body = body[:50*1024]
	}
	return PR{
		Branch:      p.PullRequest.HeadRef.Ref,
		Title:       p.PullRequest.Title,
		Body:        body,
		AddDel:      p.PullRequest.Additions + p.PullRequest.Deletions,
		DiffExcerpt: body,
	}, nil
}

type classifyRequest struct {
	Title       string   `json:"title"`
	Body        string   `json:"body"`
	Branch      string   `json:"branch"`
	Files       []string `json:"files"`
	AddDel      int      `json:"add_del"`
	DiffExcerpt string   `json:"diff_excerpt,omitempty"`
	Owner       string   `json:"owner,omitempty"`
	Repo        string   `json:"repo,omitempty"`
	Number      int      `json:"number,omitempty"`
}

func (r classifyRequest) toPR() (PR, error) {
	if strings.TrimSpace(r.Title) == "" && strings.TrimSpace(r.Body) == "" {
		return PR{}, errors.New("classify request must include title or body")
	}
	diff := r.DiffExcerpt
	if diff == "" {
		diff = r.Body
	}
	if len(diff) > 50*1024 {
		diff = diff[:50*1024]
	}
	return PR{
		Branch:      strings.TrimSpace(r.Branch),
		Title:       strings.TrimSpace(r.Title),
		Body:        r.Body,
		Files:       r.Files,
		AddDel:      r.AddDel,
		DiffExcerpt: diff,
	}, nil
}

func verifySignature(secret string, sigHeader string, body []byte) error {
	if secret == "" {
		return errors.New("webhook: empty secret")
	}
	if sigHeader == "" {
		return errors.New("webhook: missing signature header")
	}
	sig := strings.TrimSpace(sigHeader)
	if i := strings.IndexByte(sig, '='); i >= 0 {
		sig = sig[i+1:]
	}
	got, err := hex.DecodeString(sig)
	if err != nil {
		return errors.New("webhook: invalid signature encoding: " + err.Error())
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	want := mac.Sum(nil)
	if !hmac.Equal(got, want) {
		return errors.New("webhook: signature mismatch")
	}
	return nil
}
