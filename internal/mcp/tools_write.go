package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/LOUST-PRO/TaxonRouter/pkg/github"
)

func cardsAddExisting(ctx context.Context, gh *github.Client, contentID, contentType string) (map[string]any, error) {
	projectID := gh.ProjectID()
	if projectID == "" {
		return nil, fmt.Errorf("GITHUB_PROJECT_ID not set")
	}

	query := `mutation {
	  addProjectV2ItemById(input: {projectId: "` + projectID + `", contentId: "` + contentID + `"}) {
	    item { id }
	  }
	}`

	raw, err := gh.Query(ctx, query, nil)
	if err != nil {
		return nil, fmt.Errorf("cards_add_existing: %w", err)
	}

	var wrapper struct {
		Data struct {
			AddProjectV2ItemById struct {
				Item struct {
					ID string `json:"id"`
				} `json:"item"`
			} `json:"addProjectV2ItemById"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil, fmt.Errorf("parse addProjectV2ItemById response: %w", err)
	}

	return map[string]any{
		"item_id":      wrapper.Data.AddProjectV2ItemById.Item.ID,
		"added":       true,
		"content_type": contentType,
	}, nil
}

func cardsUpdateFields(ctx context.Context, gh *github.Client, itemID string, fields map[string]string) (map[string]any, error) {
	projectID := gh.ProjectID()
	if projectID == "" {
		return nil, fmt.Errorf("GITHUB_PROJECT_ID not set")
	}

	var results []string
	for fieldName, value := range fields {
		optionID := value
		if !isLikelyOptionID(value) {
			cacheKey := projectID + ":" + fieldName
			optionID = gh.ResolveOptionID(ctx, cacheKey, fieldName, value)
		}

		mutation := `mutation {
		  updateProjectV2ItemFieldValue(input: {
		    projectId: "` + projectID + `",
		    itemId: "` + itemID + `",
		    fieldId: "` + fieldName + `",
		    value: { singleSelectOptionId: "` + optionID + `" }
		  }) { projectV2Item { id } }
		}`
		_, err := gh.Query(ctx, mutation, nil)
		if err != nil {
			results = append(results, "error: "+err.Error())
		} else {
			results = append(results, fieldName+": ok")
		}
	}

	return map[string]any{
		"updated": len(results),
		"details": results,
	}, nil
}

func isLikelyOptionID(s string) bool {
	if len(s) < 10 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') || c == '_') {
			return false
		}
	}
	return true
}
