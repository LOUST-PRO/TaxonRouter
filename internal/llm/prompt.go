package llm

import (
	"encoding/json"
	"fmt"
	"strings"
)

// SystemPrompt is prepended to every LLM call.
const SystemPrompt = `You are an expert PR classifier for a multi-repo security/devops org.
Given a PR's title, body, and diff excerpt, classify it into GitHub labels and project fields.

Return ONLY a JSON object with this exact schema (no prose):

{
  "labels": ["label1", "label2"],
  "project_fields": {"Phase": "fix|feat|hardening|...", "Category": "...", "Lang": "..."},
  "confidence": 0.0..1.0,
  "reason": "one short sentence explaining the classification"
}

Allowed labels (use slugs exactly): fix, enhancement, security,
audit, breaking, deprecation, performance, docs, tests, hardening,
experiment, lab, chore, refactor, review, spike, baremetal,
proxy-affects, infra, db-schema, ci, docs-only.

Rules of thumb:
- Prefer fewer labels over more.
- Confidence reflects how certain you are. 0.9+ only when at least two
  strong signals agree (e.g. branch prefix AND body keyword).
- If the PR is genuinely ambiguous, return labels: [] and confidence < 0.4.
- Do NOT invent labels not in the allowed list.`

// LLMResponseJSON is the expected LLM output schema.
type LLMResponseJSON struct {
	Labels        []string          `json:"labels"`
	ProjectFields map[string]string `json:"project_fields"`
	Confidence    float64           `json:"confidence"`
	Reason        string            `json:"reason"`
}

// BuildUserPrompt composes the per-PR prompt.
func BuildUserPrompt(pr PR, candidateLabels []string) string {
	var b strings.Builder
	b.WriteString("PR title: ")
	b.WriteString(strings.TrimSpace(pr.Title))
	b.WriteString("\n\nPR branch: ")
	b.WriteString(strings.TrimSpace(pr.Branch))
	b.WriteString("\n\nPR body:\n")

	body := pr.Body
	if len(body) > 2048 {
		body = body[:2048] + "\n... [body truncated for length]"
	}
	b.WriteString(body)
	b.WriteString("\n\n")

	diff := pr.DiffExcerpt
	if diff == "" {
		diff = pr.Body
	}
	if len(diff) > 4096 {
		diff = diff[:4096] + "\n... [diff truncated for length]"
	}
	if diff != "" {
		b.WriteString("Diff excerpt:\n")
		b.WriteString(diff)
		b.WriteString("\n\n")
	}

	if len(pr.Files) > 0 {
		shown := pr.Files
		if len(shown) > 50 {
			shown = shown[:50]
		}
		b.WriteString("Files changed:\n")
		for _, f := range shown {
			b.WriteString("  ")
			b.WriteString(f)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	b.WriteString("Candidate labels (from rules engine):\n")
	if len(candidateLabels) == 0 {
		b.WriteString("  (none)\n")
	} else {
		for _, l := range candidateLabels {
			b.WriteString("  - ")
			b.WriteString(l)
			b.WriteString("\n")
		}
	}
	b.WriteString("\nReturn JSON only.")
	return b.String()
}

var allowedLabels = map[string]struct{}{
	"fix": {}, "enhancement": {}, "security": {}, "audit": {},
	"breaking": {}, "deprecation": {}, "performance": {}, "docs": {},
	"tests": {}, "hardening": {}, "experiment": {}, "lab": {},
	"chore": {}, "refactor": {}, "review": {}, "spike": {},
	"baremetal": {}, "proxy-affects": {}, "infra": {}, "db-schema": {},
	"ci": {}, "docs-only": {},
}

// ParseError indicates the LLM returned non-parseable output.
type ParseError struct {
	Raw   string
	Cause error
}

func (e *ParseError) Error() string {
	return "llm: parse error: " + e.Cause.Error()
}

func (e *ParseError) Unwrap() error { return e.Cause }

// ParseLLMResponse extracts and validates the LLM JSON output.
func ParseLLMResponse(raw string) (Classification, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Classification{}, &ParseError{Raw: raw, Cause: fmt.Errorf("empty response")}
	}
	if strings.HasPrefix(raw, "```") {
		end := strings.Index(raw, "\n")
		if end >= 0 {
			raw = raw[end+1:]
		}
		if i := strings.LastIndex(raw, "```"); i >= 0 {
			raw = raw[:i]
		}
		raw = strings.TrimSpace(raw)
	}
	var resp LLMResponseJSON
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		return Classification{}, &ParseError{Raw: raw, Cause: err}
	}
	cleaned := make([]string, 0, len(resp.Labels))
	seen := make(map[string]struct{}, len(resp.Labels))
	for _, l := range resp.Labels {
		if _, ok := allowedLabels[l]; !ok {
			continue
		}
		if _, dup := seen[l]; !dup {
			seen[l] = struct{}{}
			cleaned = append(cleaned, l)
		}
	}
	if resp.Confidence < 0 {
		resp.Confidence = 0
	}
	if resp.Confidence > 1 {
		resp.Confidence = 1
	}
	c := Classification{
		Labels:        cleaned,
		ProjectFields: resp.ProjectFields,
		Confidence:    resp.Confidence,
		Reasons:       []string{"llm:" + resp.Reason},
	}
	if c.ProjectFields == nil {
		c.ProjectFields = map[string]string{}
	}
	return c, nil
}
