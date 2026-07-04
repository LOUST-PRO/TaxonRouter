package mcp

import (
	"github.com/LOUST-PRO/TaxonRouter/pkg/rules"
)

// RulesConfig holds thresholds for the rules-only classification path.
type RulesConfig struct {
	ConfidenceThreshold float64
	MaxLOCAuto         int
}

// CombineRules runs the rules engine against a PR.
func CombineRules(pr rules.PR, cfg RulesConfig) rules.Classification {
	return rules.Combine(pr, rules.Config{
		ConfidenceThreshold: cfg.ConfidenceThreshold,
		MaxLOCAuto:        cfg.MaxLOCAuto,
		TitleRules:        rules.DefaultTitleRules(),
		BodyRules:         rules.DefaultBodyRules(),
		PathRules:         rules.DefaultPathRules(),
		EffortRules:       rules.DefaultEffortRules(),
	})
}
