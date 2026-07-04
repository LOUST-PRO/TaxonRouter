// Package mcp implements the TaxonRouter MCP stdio server.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/LOUST-PRO/TaxonRouter/internal/classifier"
	"github.com/LOUST-PRO/TaxonRouter/pkg/domain"
	"github.com/LOUST-PRO/TaxonRouter/pkg/github"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Env reads configuration from environment variables.
type Env struct {
	AppID              string
	InstallationID     string
	PrivateKeyFile     string
	Token              string
	ProjectID          string
	ProjectNumber      int
	ConfigFile         string
	FieldMapping      map[string]string
	FieldValueMapping map[string]string
}

// LoadEnv reads from environment variables with safe defaults.
func LoadEnv() Env {
	return Env{
		AppID:             os.Getenv("GITHUB_APP_ID"),
		InstallationID:    os.Getenv("GITHUB_APP_INSTALLATION_ID"),
		PrivateKeyFile:    os.Getenv("GITHUB_APP_PRIVATE_KEY_FILE"),
		Token:             os.Getenv("GITHUB_TOKEN"),
		ProjectID:         os.Getenv("GITHUB_PROJECT_ID"),
		ProjectNumber:     0,
		ConfigFile:        os.Getenv("LZT_GITHUB_PROJECTS_CONFIG"),
		FieldMapping:      parseMapping(os.Getenv("GITHUB_PROJECT_FIELD_MAPPING")),
		FieldValueMapping: parseMapping(os.Getenv("GITHUB_PROJECT_FIELD_VALUE_MAPPING")),
	}
}

func parseMapping(s string) map[string]string {
	if s == "" {
		return nil
	}
	m := make(map[string]string)
	for _, part := range splitBy(s, ',') {
		kv := splitBy(part, '=')
		if len(kv) == 2 {
			m[kv[0]] = kv[1]
		}
	}
	return m
}

func splitBy(s string, sep rune) []string {
	var parts []string
	var cur string
	for _, c := range s {
		if c == sep {
			if cur != "" || len(parts) > 0 {
				parts = append(parts, cur)
			}
			cur = ""
		} else {
			cur += string(c)
		}
	}
	if cur != "" || len(parts) > 0 {
		parts = append(parts, cur)
	}
	return parts
}

// Run starts the MCP stdio server and blocks.
func Run(ctx context.Context, env Env) error {
	ghClient, err := github.NewClient(
		env.AppID,
		env.InstallationID,
		env.PrivateKeyFile,
		env.ProjectID,
		env.ProjectNumber,
	)
	if err != nil {
		return fmt.Errorf("new github client: %w", err)
	}

	srv := mcp.NewServer(&mcp.Implementation{
		Name:    "taxonrouter-mcp",
		Version: "0.1.0",
	}, nil)

	// Cards list tool.
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "cards_list",
		Description: "List project cards with field values. Returns up to `limit` items (1-100, default 20).",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"limit": {"type": "integer", "default": 20, "minimum": 1, "maximum": 100}
			}
		}`),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args struct {
		Limit int `json:"limit"`
	}) (*mcp.CallToolResult, any, error) {
		limit := args.Limit
		if limit <= 0 {
			limit = 20
		}
		if limit > 100 {
			limit = 100
		}
		result, err := cardsList(ctx, ghClient, limit)
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: "error: " + err.Error()}},
			}, nil, nil
		}
		data, _ := json.Marshal(result)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
		}, nil, nil
	})

	// Field options tool.
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "field_options",
		Description: "Get field metadata and option list for a SINGLE_SELECT field. Cached 5 minutes.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"field_name": {"type": "string"}
			},
			"required": ["field_name"]
		}`),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args struct {
		FieldName string `json:"field_name"`
	}) (*mcp.CallToolResult, any, error) {
		if args.FieldName == "" {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: "error: field_name required"}},
			}, nil, nil
		}
		result, err := fieldOptions(ctx, ghClient, args.FieldName)
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: "error: " + err.Error()}},
			}, nil, nil
		}
		data, _ := json.Marshal(result)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
		}, nil, nil
	})

	// Cards add existing tool.
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "cards_add_existing",
		Description: "Add an existing PR or issue to the project by node ID.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"content_id": {"type": "string", "description": "GitHub node ID of the PR or issue (starts with PR_, ISSUE_, etc.)"},
				"content_type": {"type": "string", "enum": ["PullRequest", "Issue"], "default": "PullRequest"}
			},
			"required": ["content_id"]
		}`),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args struct {
		ContentID   string `json:"content_id"`
		ContentType string `json:"content_type"`
	}) (*mcp.CallToolResult, any, error) {
		if args.ContentID == "" {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: "error: content_id required"}},
			}, nil, nil
		}
		ct := args.ContentType
		if ct == "" {
			ct = "PullRequest"
		}
		result, err := cardsAddExisting(ctx, ghClient, args.ContentID, ct)
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: "error: " + err.Error()}},
			}, nil, nil
		}
		data, _ := json.Marshal(result)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
		}, nil, nil
	})

	// Current fields tool.
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "current_fields",
		Description: "Read the current field values on a project card. Use for drift detection before updating.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"item_id": {"type": "string", "description": "Project V2 item node ID"}
			},
			"required": ["item_id"]
		}`),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args struct {
		ItemID string `json:"item_id"`
	}) (*mcp.CallToolResult, any, error) {
		if args.ItemID == "" {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: "error: item_id required"}},
			}, nil, nil
		}
		result, err := currentFields(ctx, ghClient, args.ItemID)
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: "error: " + err.Error()}},
			}, nil, nil
		}
		data, _ := json.Marshal(result)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
		}, nil, nil
	})

	// Cards update fields tool.
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "cards_update_fields",
		Description: "Update single-select fields on a project card. Accepts option names (e.g. 'In Progress') or option IDs (e.g. '47fc9ee4').",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"item_id": {"type": "string"},
				"fields": {"type": "object", "additionalProperties": {"type": "string"}}
			},
			"required": ["item_id", "fields"]
		}`),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args struct {
		ItemID string                 `json:"item_id"`
		Fields map[string]string `json:"fields"`
	}) (*mcp.CallToolResult, any, error) {
		if args.ItemID == "" || len(args.Fields) == 0 {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: "error: item_id and fields required"}},
			}, nil, nil
		}
		result, err := cardsUpdateFields(ctx, ghClient, args.ItemID, args.Fields)
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: "error: " + err.Error()}},
			}, nil, nil
		}
		data, _ := json.Marshal(result)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
		}, nil, nil
	})

	// Classify tool (pure rules-based PR classification).
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "classify_pr",
		Description: "Classify a PR using rules-only classification. Returns labels, project fields, and confidence.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"title": {"type": "string"},
				"body": {"type": "string"},
				"branch": {"type": "string"},
				"files": {"type": "array", "items": {"type": "string"}},
				"add_del": {"type": "integer"}
			}
		}`),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args struct {
		Title  string   `json:"title"`
		Body   string   `json:"body"`
		Branch string   `json:"branch"`
		Files  []string `json:"files"`
		AddDel int      `json:"add_del"`
	}) (*mcp.CallToolResult, any, error) {
		cfg := classifier.DefaultConfig()
		result := classifier.Combine(classifier.PR{
			Branch: args.Branch,
			Title:  args.Title,
			Body:   args.Body,
			Files:  args.Files,
			AddDel: args.AddDel,
		}, cfg)
		out := domain.Classification{
			Labels:        result.Labels,
			ProjectFields: result.ProjectFields,
			Confidence:    result.Confidence,
			Reasons:       result.Reasons,
			ManualReview:  result.ManualReview,
			ManualReasons: result.ManualReasons,
		}
		data, _ := json.Marshal(out)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
		}, nil, nil
	})

	return srv.Run(ctx, &mcp.StdioTransport{})
}
