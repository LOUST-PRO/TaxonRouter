// Package domain provides shared pure types for TaxonRouter.
// No I/O, no external dependencies — this package is safe to import in both
// the MCP server and the auto-tagger daemon.
package domain

import "time"

// Classification is the canonical output of one PR classification run.
// Labels are GitHub label slugs. ProjectFields are key→value pairs that map
// onto GitHub ProjectV2 single-select fields.
type Classification struct {
	Labels        []string          `json:"labels"`
	ProjectFields map[string]string `json:"project_fields"`
	Confidence    float64           `json:"confidence"`
	Reasons       []string          `json:"reasons"`
	ManualReview  bool              `json:"manual_review"`
	ManualReasons []string          `json:"manual_reasons,omitempty"`
}

// PR is the minimum-viable input shape for classification.
// Webhook payloads are normalised into this shape before classification.
type PR struct {
	Branch string   `json:"branch"`
	Title  string   `json:"title"`
	Body   string   `json:"body"`
	Files  []string `json:"files"`
	AddDel int      `json:"add_del"` // total LOC changed (added + deleted)
}

// Repo identifies a GitHub repository and the PR number.
type Repo struct {
	Owner  string `json:"owner"`
	Repo   string `json:"repo"`
	Number int    `json:"number"`
}

// Item is a manual-review candidate pending application to GitHub.
type Item struct {
	Repo          Repo            `json:"repo"`
	Labels        []string        `json:"labels"`
	ProjectFields map[string]string `json:"project_fields"`
	Confidence    float64         `json:"confidence"`
	Reasons       []string        `json:"reasons"`
	EnqueuedAt   time.Time       `json:"enqueued_at"`
}

// FieldOption is a single option of a SINGLE_SELECT field.
type FieldOption struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Color       string `json:"color,omitempty"`
	Description string `json:"description,omitempty"`
}

// Field is one ProjectV2 field.
type Field struct {
	ID       string        `json:"id"`
	Name     string        `json:"name"`
	DataType string        `json:"data_type"`
	Options  []FieldOption `json:"options,omitempty"`
}

// FieldValue is the value of one field on a card.
type FieldValue struct {
	OptionID   string `json:"option_id,omitempty"`
	OptionName string `json:"option_name,omitempty"`
	Text       string `json:"text,omitempty"`
}

// Card is one ProjectV2 item (card).
type Card struct {
	ID           string                 `json:"id"`
	Title       string                 `json:"title"`
	BodyExcerpt string                 `json:"body_excerpt"`
	Fields      map[string]FieldValue `json:"fields"`
	UpdatedAt   string                 `json:"updated_at"`
	Truncated   bool                   `json:"truncated,omitempty"`
}

// CardsListResult is the output of cards_list.
type CardsListResult struct {
	Items      []Card `json:"items"`
	TotalCount int    `json:"total_count"`
	Truncated  bool   `json:"truncated,omitempty"`
	Source     string `json:"source"` // "graphql"
}

// FieldOptionsResult is the output of field_options.
type FieldOptionsResult struct {
	Field           Field  `json:"field"`
	CacheAgeSeconds int64  `json:"cache_age_seconds"`
	Source          string `json:"source"` // "cache" | "graphql"
}

// Config holds TaxonRouter configuration.
type Config struct {
	ProjectID    string        `toml:"project_id"`
	DefaultLimit int           `toml:"default_limit"`
	CacheTTL     time.Duration `toml:"cache_ttl"`
	MaxLimit     int           `toml:"max_limit"`
	BodyExcerpt  int           `toml:"body_excerpt_chars"`
}
