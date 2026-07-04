package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/LOUST-PRO/TaxonRouter/internal/adapter"
	"github.com/LOUST-PRO/TaxonRouter/internal/classifier"
	"github.com/LOUST-PRO/TaxonRouter/internal/llm"
	"github.com/LOUST-PRO/TaxonRouter/internal/webhook"
)

// Env holds auto-tagger configuration from environment variables.
type Env struct {
	Port               string
	WebhookSecret     string
	GitHubToken       string
	ProjectNumber      int
	ProjectID         string
	FieldMapping      map[string]string
	FieldValueMapping map[string]string
	LLMProvider       string
	LLMAPISecret      string
	ApplyToken        string
	DryRun            bool
}

func main() {
	env := loadEnv()
	logger := slog.Default()

	cfg := webhook.Config{
		WebhookSecret: env.WebhookSecret,
		ApplyToken:   env.ApplyToken,
		ProjectNumber: env.ProjectNumber,
		Logger:       logger,
		Now:          time.Now,
		MaxBodyBytes: 1 << 20,
	}

	// Build the classifier engine.
	classifierCfg := classifier.DefaultConfig()
	var engine webhook.Classifier

	if env.LLMProvider != "" && env.LLMAPISecret != "" {
		var provider llm.Provider
		switch env.LLMProvider {
		case "openai":
			provider = llm.NewOpenAIProvider(env.LLMAPISecret)
		case "anthropic":
			provider = llm.NewAnthropicProvider(env.LLMAPISecret)
		}
		if provider != nil {
			hybrid := adapter.NewHybridEngine(classifierCfg, provider)
			engine = hybrid
		} else {
			engine = adapter.NewRulesOnlyEngine(classifierCfg)
		}
	} else {
		engine = adapter.NewRulesOnlyEngine(classifierCfg)
	}

	cfg.ClassifierEngine = engine

	srv := webhook.NewServer(cfg)
	mux := srv.Handler()

	addr := ":" + env.Port
	if env.Port == "" {
		addr = ":3013"
	}
	server := &http.Server{Addr: addr, Handler: mux}

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(ctx)
	}()

	logger.Info("taxonrouter-auto-tagger listening", slog.String("addr", addr))
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server error", slog.String("err", err.Error()))
		os.Exit(1)
	}
}

func loadEnv() Env {
	return Env{
		Port:              os.Getenv("PORT"),
		WebhookSecret:     os.Getenv("WEBHOOK_SECRET"),
		GitHubToken:       os.Getenv("GITHUB_TOKEN"),
		ProjectNumber:     atoiEnv("GITHUB_PROJECT_NUMBER"),
		ProjectID:         os.Getenv("GITHUB_PROJECT_ID"),
		LLMProvider:      os.Getenv("LLM_PROVIDER"),
		LLMAPISecret:     os.Getenv("LLM_API_KEY"),
		ApplyToken:        os.Getenv("APPLY_TOKEN"),
		DryRun:            os.Getenv("DRY_RUN") == "1",
		FieldMapping:      parseMapping(os.Getenv("GITHUB_PROJECT_FIELD_MAPPING")),
		FieldValueMapping: parseMapping(os.Getenv("GITHUB_PROJECT_FIELD_VALUE_MAPPING")),
	}
}

func atoiEnv(key string) int {
	v := os.Getenv(key)
	if v == "" {
		return 0
	}
	var n int
	for _, c := range v {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	return n
}

func parseMapping(s string) map[string]string {
	if s == "" {
		return nil
	}
	m := make(map[string]string)
	for _, part := range strings.Split(s, ",") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 {
			m[kv[0]] = kv[1]
		}
	}
	return m
}
