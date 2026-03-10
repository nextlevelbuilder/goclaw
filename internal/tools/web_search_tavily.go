package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// --- Tavily Search Provider ---

const tavilySearchEndpoint = "https://api.tavily.com/search"

type tavilySearchProvider struct {
	apiKey string
	client *http.Client
}

func newTavilySearchProvider(apiKey string) *tavilySearchProvider {
	return &tavilySearchProvider{
		apiKey: apiKey,
		client: &http.Client{Timeout: time.Duration(searchTimeoutSeconds) * time.Second},
	}
}

func (p *tavilySearchProvider) Name() string { return "tavily" }

func (p *tavilySearchProvider) Search(ctx context.Context, params searchParams) ([]searchResult, error) {
	// Tavily request body
	reqBody := map[string]any{
		"query":              params.Query,
		"max_results":        params.Count,
		"include_raw_content": false,
		"include_images":     false,
		"include_answer":     false,
	}

	// Map freshness to Tavily's days parameter
	if params.Freshness != "" {
		days := mapFreshnessToDays(params.Freshness)
		if days > 0 {
			reqBody["days"] = days
		}
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", tavilySearchEndpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	// Tavily expects the body in the URL for some reason (form-encoded)
	// Actually, let's check - Tavily API accepts JSON body with Content-Type: application/json
	req, err = http.NewRequestWithContext(ctx, "POST", tavilySearchEndpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tavily API returned %d: %s", resp.StatusCode, truncateStr(string(body), 200))
	}

	var tavilyResp struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}

	if err := json.Unmarshal(body, &tavilyResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	results := make([]searchResult, 0, len(tavilyResp.Results))
	for _, r := range tavilyResp.Results {
		results = append(results, searchResult{
			Title:       r.Title,
			URL:         r.URL,
			Description: r.Content,
		})
	}
	return results, nil
}

// mapFreshnessToDays converts GoClaw freshness to Tavily days parameter
func mapFreshnessToDays(freshness string) int {
	switch freshness {
	case "pd":
		return 1
	case "pw":
		return 7
	case "pm":
		return 30
	case "py":
		return 365
	}
	return 0
}
