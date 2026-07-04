package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// HTTPClient is the minimal interface used by providers.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// OpenAIProvider calls the OpenAI Chat Completions API.
type OpenAIProvider struct {
	APIKey    string
	Model     string
	Endpoint  string
	HTTPDo    HTTPClient
	UserAgent string
}

// NewOpenAIProvider creates an OpenAIProvider.
func NewOpenAIProvider(apiKey string) *OpenAIProvider {
	return &OpenAIProvider{
		APIKey:    apiKey,
		Model:     "gpt-4o-mini",
		Endpoint:  "https://api.openai.com/v1/chat/completions",
		HTTPDo:    &http.Client{Timeout: 30 * time.Second},
		UserAgent: "TaxonRouter/0.1.0",
	}
}

// Complete implements Provider.
func (p *OpenAIProvider) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	body := map[string]any{
		"model": p.Model,
		"messages": []map[string]string{
			{"role": "system", "content": req.SystemPrompt},
			{"role": "user", "content": req.UserPrompt},
		},
		"temperature":     req.Temperature,
		"max_tokens":      pickMaxTokens(req.MaxTokens, 512),
		"response_format": map[string]string{"type": "json_object"},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("openai marshal: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.Endpoint, bytes.NewReader(raw))
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("openai new request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", p.UserAgent)

	resp, err := p.HTTPDo.Do(httpReq)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("openai http: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return CompletionResponse{}, fmt.Errorf("openai status %d: %s", resp.StatusCode, string(snippet))
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return CompletionResponse{}, fmt.Errorf("openai decode: %w", err)
	}
	if len(out.Choices) == 0 {
		return CompletionResponse{}, fmt.Errorf("openai returned no choices")
	}
	return CompletionResponse{
		Text:       out.Choices[0].Message.Content,
		TokensIn:   out.Usage.PromptTokens,
		TokensOut:  out.Usage.CompletionTokens,
		StopReason: out.Choices[0].FinishReason,
	}, nil
}

// AnthropicProvider calls the Anthropic Messages API.
type AnthropicProvider struct {
	APIKey    string
	Model     string
	Endpoint  string
	Version   string
	HTTPDo    HTTPClient
	UserAgent string
}

// NewAnthropicProvider creates an AnthropicProvider.
func NewAnthropicProvider(apiKey string) *AnthropicProvider {
	return &AnthropicProvider{
		APIKey:    apiKey,
		Model:     "claude-3-5-sonnet-latest",
		Endpoint:  "https://api.anthropic.com/v1/messages",
		Version:   "2023-06-01",
		HTTPDo:    &http.Client{Timeout: 30 * time.Second},
		UserAgent: "TaxonRouter/0.1.0",
	}
}

// Complete implements Provider.
func (p *AnthropicProvider) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	body := map[string]any{
		"model":      p.Model,
		"max_tokens": pickMaxTokens(req.MaxTokens, 1024),
		"system":     req.SystemPrompt,
		"messages": []map[string]string{
			{"role": "user", "content": req.UserPrompt},
		},
		"temperature": req.Temperature,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("anthropic marshal: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.Endpoint, bytes.NewReader(raw))
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("anthropic new request: %w", err)
	}
	httpReq.Header.Set("x-api-key", p.APIKey)
	httpReq.Header.Set("anthropic-version", p.Version)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", p.UserAgent)

	resp, err := p.HTTPDo.Do(httpReq)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("anthropic http: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return CompletionResponse{}, fmt.Errorf("anthropic status %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
		Usage      struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return CompletionResponse{}, fmt.Errorf("anthropic decode: %w", err)
	}
	var text string
	for _, c := range out.Content {
		if c.Type == "text" {
			text += c.Text
		}
	}
	if text == "" {
		return CompletionResponse{}, fmt.Errorf("anthropic returned no text content")
	}
	return CompletionResponse{
		Text:       text,
		TokensIn:   out.Usage.InputTokens,
		TokensOut:  out.Usage.OutputTokens,
		StopReason: out.StopReason,
	}, nil
}

func pickMaxTokens(req int, def int) int {
	if req > 0 {
		return req
	}
	return def
}
