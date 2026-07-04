// Package llm provides LLM-assisted PR classification.
package llm

import (
	"context"
	"sort"
)

// PR mirrors the PR shape used by the classifier.
type PR struct {
	Branch      string
	Title       string
	Body        string
	Files       []string
	AddDel      int
	DiffExcerpt string
}

// Classification mirrors the classifier.Classification shape.
type Classification struct {
	Labels        []string
	ProjectFields map[string]string
	Confidence    float64
	Reasons       []string
	ManualReview  bool
	ManualReasons []string
}

// Provider is the interface for an LLM backend.
type Provider interface {
	Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
}

// CompletionRequest is the input to a Provider.
type CompletionRequest struct {
	SystemPrompt string
	UserPrompt   string
	MaxTokens    int
	Temperature  float64
	JSONMode     bool
}

// CompletionResponse is the output from a Provider.
type CompletionResponse struct {
	Text       string
	TokensIn   int
	TokensOut  int
	StopReason string
}

// ConfidenceThreshold for the hybrid classifier.
const ConfidenceThreshold = 0.6

// LLMWeight for the hybrid confidence blend.
const LLMWeight = 0.7

// RulesEngine is the subset of the rules classifier the hybrid needs.
type RulesEngine interface {
	Classify(ctx context.Context, pr PR) (Classification, error)
}

// HybridClassifier runs rules first; falls back to LLM on low confidence.
type HybridClassifier struct {
	rules     RulesEngine
	provider  Provider
	threshold float64
}

// NewHybridClassifier creates a HybridClassifier.
func NewHybridClassifier(rules RulesEngine, provider Provider) *HybridClassifier {
	return &HybridClassifier{
		rules:     rules,
		provider:  provider,
		threshold: ConfidenceThreshold,
	}
}

// Classify implements the hybrid decision tree.
func (h *HybridClassifier) Classify(ctx context.Context, pr PR) (Classification, error) {
	rules, err := h.rules.Classify(ctx, pr)
	if err != nil {
		return Classification{}, err
	}

	if rules.Confidence >= h.threshold && !rules.ManualReview {
		return rules, nil
	}

	if h.provider == nil {
		if !rules.ManualReview {
			rules.ManualReview = true
			rules.ManualReasons = append(rules.ManualReasons, "llm_unavailable")
		}
		return rules, nil
	}

	llmOut, err := h.llmClassify(ctx, pr)
	if err != nil {
		if !rules.ManualReview {
			rules.ManualReview = true
		}
		rules.ManualReasons = append(rules.ManualReasons, "llm_error")
		return rules, nil
	}

	return merge(rules, llmOut, h.threshold), nil
}

func (h *HybridClassifier) llmClassify(ctx context.Context, pr PR) (Classification, error) {
	if h.provider == nil {
		return Classification{}, nil
	}
	prompt := BuildUserPrompt(pr, nil)
	resp, err := h.provider.Complete(ctx, CompletionRequest{
		SystemPrompt: SystemPrompt,
		UserPrompt:   prompt,
		MaxTokens:    512,
		Temperature:  0.1,
		JSONMode:    true,
	})
	if err != nil {
		return Classification{}, err
	}
	return ParseLLMResponse(resp.Text)
}

func merge(rules, llmOut Classification, threshold float64) Classification {
	out := Classification{
		Labels:        append([]string{}, rules.Labels...),
		ProjectFields: map[string]string{},
		Confidence:    rules.Confidence,
		Reasons:       append([]string{}, rules.Reasons...),
		ManualReview:  rules.ManualReview,
		ManualReasons: append([]string{}, rules.ManualReasons...),
	}
	for k, v := range rules.ProjectFields {
		out.ProjectFields[k] = v
	}

	existing := make(map[string]struct{}, len(rules.Labels))
	for _, l := range rules.Labels {
		existing[l] = struct{}{}
	}
	for _, l := range llmOut.Labels {
		if _, ok := existing[l]; !ok {
			out.Labels = append(out.Labels, l)
			out.Reasons = append(out.Reasons, "llm_label:"+l)
			existing[l] = struct{}{}
		}
	}

	for k, v := range llmOut.ProjectFields {
		if _, ok := out.ProjectFields[k]; !ok {
			out.ProjectFields[k] = v
		}
	}

	out.Labels = sortedUniq(out.Labels)
	out.Reasons = sortedUniq(out.Reasons)
	out.ManualReasons = sortedUniq(out.ManualReasons)

	if rules.Confidence == 0 {
		out.Confidence = llmOut.Confidence * LLMWeight
	} else {
		out.Confidence = (1-LLMWeight)*rules.Confidence + LLMWeight*llmOut.Confidence
	}
	if len(llmOut.Reasons) > 0 {
		out.Reasons = append(out.Reasons, "llm_reason:"+llmOut.Reasons[0])
	}

	if out.Confidence < threshold {
		out.ManualReview = true
		out.ManualReasons = append(out.ManualReasons, "confidence_below_threshold")
	}
	out.ManualReasons = sortedUniq(out.ManualReasons)
	return out
}

func sortedUniq(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// RulesClassifier wraps a rules engine for the hybrid path.
type RulesClassifier struct {
	engine RulesEngine
}

func (r *RulesClassifier) Classify(ctx context.Context, pr PR) (Classification, error) {
	return r.engine.Classify(ctx, pr)
}

// NewRulesClassifier creates a RulesClassifier.
func NewRulesClassifier(engine RulesEngine) *RulesClassifier {
	return &RulesClassifier{engine: engine}
}

// NoopProvider returns empty responses.
type NoopProvider struct{}

func (NoopProvider) Complete(_ context.Context, _ CompletionRequest) (CompletionResponse, error) {
	return CompletionResponse{Text: "", StopReason: "noop"}, nil
}
