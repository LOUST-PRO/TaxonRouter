package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/LOUST-PRO/TaxonRouter/pkg/github"
)

func cardsList(ctx context.Context, gh *github.Client, limit int) (map[string]any, error) {
	query := `query {
	  node(id: "` + gh.ProjectID() + `") {
	    ... on ProjectV2 {
	      items(first: ` + fmt.Sprintf("%d", limit) + `) {
	        nodes {
	          id
	          fieldValues(first: 20) {
	            nodes {
	              ... on ProjectV2ItemFieldText { text field { ... on ProjectV2Field { name } } }
	              ... on ProjectV2ItemFieldSingleSelect { name field { ... on ProjectV2SingleSelectField { name } } singleSelect { id name } }
	            }
	          }
	          content {
	            ... on PullRequest { title number url }
	            ... on Issue { title number url }
	          }
	        }
	      }
	    }
	  }
	}`

	raw, err := gh.Query(ctx, query, nil)
	if err != nil {
		return nil, fmt.Errorf("cards_list: %w", err)
	}

	var wrapper struct {
		Data struct {
			Node struct {
				Items struct {
					Nodes []struct {
						ID           string `json:"id"`
						FieldValues struct {
							Nodes []map[string]any `json:"nodes"`
						} `json:"fieldValues"`
						Content struct {
							Title  string `json:"title"`
							Number int    `json:"number"`
							URL    string `json:"url"`
						} `json:"content"`
					} `json:"nodes"`
				} `json:"items"`
			} `json:"node"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil, fmt.Errorf("parse cards_list response: %w", err)
	}

	cards := make([]map[string]any, 0, len(wrapper.Data.Node.Items.Nodes))
	for _, n := range wrapper.Data.Node.Items.Nodes {
		card := map[string]any{
			"id":     n.ID,
			"title":  n.Content.Title,
			"number": n.Content.Number,
			"url":    n.Content.URL,
		}
		fields := make(map[string]string)
		for _, fv := range n.FieldValues.Nodes {
			if name, ok := fv["name"].(string); ok {
				if text, ok := fv["text"].(string); ok {
					fields[name] = text
				}
				if ss, ok := fv["singleSelect"].(map[string]any); ok {
					if optName, ok := ss["name"].(string); ok {
						fields[name] = optName
					}
				}
			}
		}
		if len(fields) > 0 {
			card["fields"] = fields
		}
		cards = append(cards, card)
	}

	return map[string]any{
		"items":       cards,
		"total_count": len(cards),
		"source":     "graphql",
	}, nil
}

func fieldOptions(ctx context.Context, gh *github.Client, fieldName string) (map[string]any, error) {
	cacheKey := gh.ProjectID() + ":" + fieldName
	if opts, ok := gh.GetCachedFieldOptions(cacheKey); ok {
		return map[string]any{
			"field":             map[string]any{"name": fieldName, "options": opts},
			"cache_age_seconds": 0,
			"source":            "cache",
		}, nil
	}

	query := `query {
	  node(id: "` + gh.ProjectID() + `") {
	    ... on ProjectV2 {
	      fields(first: 50) {
	        nodes {
	          ... on ProjectV2SingleSelectField {
	            id
	            name
	            options { id name color description }
	          }
	          ... on ProjectV2Field {
	            id
	            name
	          }
	        }
	      }
	    }
	  }
	}`

	raw, err := gh.Query(ctx, query, nil)
	if err != nil {
		return nil, fmt.Errorf("field_options: %w", err)
	}

	var wrapper struct {
		Data struct {
			Node struct {
				Fields struct {
					Nodes []map[string]any `json:"nodes"`
				} `json:"fields"`
			} `json:"node"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil, fmt.Errorf("parse field_options response: %w", err)
	}

	var foundOptions []github.FieldOption
	for _, f := range wrapper.Data.Node.Fields.Nodes {
		if name, ok := f["name"].(string); ok && name == fieldName {
			if opts, ok := f["options"].([]any); ok {
				for _, o := range opts {
					if om, ok := o.(map[string]any); ok {
						foundOptions = append(foundOptions, github.FieldOption{
							ID:   toString(om["id"]),
							Name: toString(om["name"]),
						})
					}
				}
			}
		}
	}

	if foundOptions != nil {
		gh.CacheFieldOptions(cacheKey, foundOptions)
	}

	return map[string]any{
		"field":             map[string]any{"name": fieldName, "options": foundOptions},
		"cache_age_seconds": 300,
		"source":            "graphql",
	}, nil
}

func currentFields(ctx context.Context, gh *github.Client, itemID string) (map[string]string, error) {
	query := `query {
	  node(id: "` + itemID + `") {
	    ... on ProjectV2Item {
	      fieldValues(first: 20) {
	        nodes {
	          ... on ProjectV2ItemFieldText { text field { ... on ProjectV2Field { name } } }
	          ... on ProjectV2ItemFieldSingleSelect { name field { ... on ProjectV2SingleSelectField { name } } singleSelect { id name } }
	        }
	      }
	    }
	  }
	}`

	raw, err := gh.Query(ctx, query, nil)
	if err != nil {
		return nil, fmt.Errorf("current_fields: %w", err)
	}

	var wrapper struct {
		Data struct {
			Node struct {
				FieldValues struct {
					Nodes []map[string]any `json:"nodes"`
				} `json:"fieldValues"`
			} `json:"node"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil, fmt.Errorf("parse current_fields response: %w", err)
	}

	fields := make(map[string]string)
	for _, fv := range wrapper.Data.Node.FieldValues.Nodes {
		if name, ok := fv["name"].(string); ok {
			if text, ok := fv["text"].(string); ok {
				fields[name] = text
			}
			if ss, ok := fv["singleSelect"].(map[string]any); ok {
				if optName, ok := ss["name"].(string); ok {
					fields[name] = optName
				}
			}
		}
	}

	return fields, nil
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
