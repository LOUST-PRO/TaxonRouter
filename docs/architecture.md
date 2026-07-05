# TaxonRouter Architecture

## Overview

TaxonRouter is a dual-binary Go project that classifies GitHub PRs and applies labels automatically.

```
┌─────────────────────────────────────────────────────────────────────┐
│                         TaxonRouter                                  │
├─────────────────────────────┬───────────────────────────────────────┤
│   cmd/taxonrouter-mcp      │    cmd/taxonrouter-auto-tagger         │
│   (MCP server)              │    (Webhook daemon)                    │
│   Port 3000                 │    Port 3001                           │
├─────────────────────────────┼───────────────────────────────────────┤
│   internal/mcp/             │    internal/webhook/                  │
│   - tools_read.go           │    - webhook.go                        │
│   - tools_write.go          │    internal/classifier/                │
│   internal/llm/             │    - classifier.go                     │
│   - hybrid.go               │    - rules.go                          │
│   - providers.go            │    internal/apply/                     │
│   - prompt.go               │    - pipeline.go                        │
├─────────────────────────────┴───────────────────────────────────────┤
│                          pkg/                                        │
│   domain/types.go  — shared pure types (no I/O)                      │
│   github/client.go  — GitHub API client                              │
│   rules/classifier.go — rule-based classification                     │
│   rules/config.go — configuration types                              │
└─────────────────────────────────────────────────────────────────────┘
```

## Binary: MCP Server (`cmd/taxonrouter-mcp`)

An MCP server that LLMs can call to:
- Classify a PR description and diff
- Get suggested labels
- Get project field values

### Tool: `classify_pr`

**Input**: `ClassifyInput { description, diff, context? }`

**Output**: `Classification { labels[], project_fields{}, confidence, reasons[], manual_review }`

### Tool: `get_rules`

Returns the current rule set used for classification.

## Binary: Auto-Tagger (`cmd/taxonrouter-auto-tagger`)

A webhook daemon that listens for GitHub webhook events (`pull_request`, `pull_request_target`) and applies labels automatically.

### Pipeline

```
GitHub webhook
    │
    ▼
webhook.Validate()        ← verify signature
    │
    ▼
classifier.Classify()      ← rule-based + LLM hybrid
    │
    ▼
pipeline.ApplyLabels()     ← batch label + project field application
    │
    ▼
GitHub API
```

## Classification Strategy

### Rule-Based (Primary)

Rules in `pkg/rules/classifier.go` match file patterns, keywords, and code structures:

```go
type Rule struct {
    Name        string
    Labels      []string
    FilePatterns []string  // glob patterns
    Keywords    []string   // in description or diff
    Confidence  float64
}
```

### LLM-Assisted (Secondary)

When rules are uncertain (`confidence < 0.8`), the hybrid classifier calls an LLM:
- `internal/llm/hybrid.go` — orchestrates rule-first, LLM-second
- `internal/llm/prompt.go` — prompt templates
- `internal/llm/providers.go` — OpenAI / Anthropic / Azure OpenAI / Local

### Project Fields

GitHub Projects V2 support single-select fields. The classifier can set:
- `Area`: which subsystem the PR affects
- `Priority`: low / medium / high / critical
- `Effort`: estimated review time

## Key Types

See `pkg/domain/types.go` for the canonical type definitions:

- `Classification` — output of classification
- `PR` — minimum viable input
- `Rule` — rule definition
- `ClassifierConfig` — configuration

## Configuration

Environment variables (see `.env.example`):
- `GITHUB_TOKEN` — API token
- `LLM_PROVIDER` — which LLM to use
- `WEBHOOK_SECRET` — webhook signature verification
