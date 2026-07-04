// Package adapter bridges the webhook, classifier, and LLM packages.
package adapter

import (
	"context"

	"github.com/LOUST-PRO/TaxonRouter/internal/classifier"
	"github.com/LOUST-PRO/TaxonRouter/internal/llm"
	"github.com/LOUST-PRO/TaxonRouter/internal/webhook"
)

// RulesEngine adapts the classifier to llm.RulesEngine.
type RulesEngine struct {
	cfg classifier.Config
}

// NewRulesEngine creates a RulesEngine.
func NewRulesEngine(cfg classifier.Config) *RulesEngine {
	return &RulesEngine{cfg: cfg}
}

// Classify implements llm.RulesEngine (llm.PR → llm.Classification).
func (r *RulesEngine) Classify(_ context.Context, pr llm.PR) (llm.Classification, error) {
	out := classifier.Combine(classifier.PR{
		Branch: pr.Branch,
		Title:  pr.Title,
		Body:   pr.Body,
		Files:  pr.Files,
		AddDel: pr.AddDel,
	}, r.cfg)
	return toLLM(out), nil
}

// RulesOnlyEngine adapts the classifier to webhook.Classifier (non-LLM path).
type RulesOnlyEngine struct {
	cfg classifier.Config
}

// NewRulesOnlyEngine creates an engine for the non-LLM path.
func NewRulesOnlyEngine(cfg classifier.Config) *RulesOnlyEngine {
	return &RulesOnlyEngine{cfg: cfg}
}

// Classify implements webhook.Classifier (webhook.PR → webhook.Classification).
func (r *RulesOnlyEngine) Classify(_ context.Context, pr webhook.PR) (webhook.Classification, error) {
	out := classifier.Combine(classifier.PR{
		Branch: pr.Branch,
		Title:  pr.Title,
		Body:   pr.Body,
		Files:  pr.Files,
		AddDel: pr.AddDel,
	}, r.cfg)
	return toWebhook(out), nil
}

// HybridEngine wraps llm.HybridClassifier and satisfies webhook.Classifier.
type HybridEngine struct {
	Hybrid *llm.HybridClassifier
}

// Classify implements webhook.Classifier.
func (h *HybridEngine) Classify(ctx context.Context, pr webhook.PR) (webhook.Classification, error) {
	c, err := h.Hybrid.Classify(ctx, llmPR(pr))
	if err != nil {
		return webhook.Classification{}, err
	}
	return toWebhookFromLLM(c), nil
}

// NewHybridEngine creates a HybridEngine from a classifier config and LLM provider.
func NewHybridEngine(cfg classifier.Config, provider llm.Provider) *HybridEngine {
	rules := &RulesEngine{cfg: cfg}
	hybrid := llm.NewHybridClassifier(rules, provider)
	return &HybridEngine{Hybrid: hybrid}
}

func toLLM(c classifier.Classification) llm.Classification {
	return llm.Classification{
		Labels:        c.Labels,
		ProjectFields: c.ProjectFields,
		Confidence:    c.Confidence,
		Reasons:       c.Reasons,
		ManualReview:  c.ManualReview,
		ManualReasons: c.ManualReasons,
	}
}

func toWebhook(c classifier.Classification) webhook.Classification {
	return webhook.Classification{
		Labels:        c.Labels,
		ProjectFields: c.ProjectFields,
		Confidence:    c.Confidence,
		Reasons:       c.Reasons,
		ManualReview:  c.ManualReview,
		ManualReasons: c.ManualReasons,
	}
}

func toWebhookFromLLM(c llm.Classification) webhook.Classification {
	return webhook.Classification{
		Labels:        c.Labels,
		ProjectFields: c.ProjectFields,
		Confidence:    c.Confidence,
		Reasons:       c.Reasons,
		ManualReview:  c.ManualReview,
		ManualReasons: c.ManualReasons,
	}
}

func llmPR(pr webhook.PR) llm.PR {
	return llm.PR{
		Branch:      pr.Branch,
		Title:       pr.Title,
		Body:        pr.Body,
		Files:       pr.Files,
		AddDel:      pr.AddDel,
		DiffExcerpt: pr.DiffExcerpt,
	}
}
