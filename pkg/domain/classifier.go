// Package domain provides shared pure types for TaxonRouter.
// No I/O, no external dependencies — this package is safe to import in both
// the MCP server and the auto-tagger daemon.
package domain

import "context"

// Classifier is the interface satisfied by any classification engine
// (rules-only, hybrid, LLM-only). Both the MCP server and the auto-tagger
// daemon use this interface so the classification strategy is pluggable.
type Classifier interface {
	Classify(ctx context.Context, pr PR) (Classification, error)
}
