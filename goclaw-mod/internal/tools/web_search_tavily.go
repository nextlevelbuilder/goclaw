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

type tavilySearchProvider struct {
	apiKey     string
	maxResults int
	client     *http.Client
}

func newTavilySearchProvider(apiKey string, maxResults int) *tavilySearchProvider {
	return &tavilySearchProvider{
		apiKey:     apiKey,
		maxResults: normalizeProviderMaxResults(maxResults),
		client:     &http.Client{Timeout: time.Duration(searchTimeoutSeconds) * time.Second},
	}
}

func (p *tavilySearchProvider) Name() string { return searchProviderTavily }

func (p *tavilySearchProvider) Search(ctx context.Context, params searchParams) ([]searchResult, error) {
	requestBody, err := json.Marshal(map[string]any{
		"query":               params.Query,
		"search_depth":        "basic",
		"max_results":         clampProviderResultCount(params.Count, p.maxResults),
		"include_answer":      false,
		"include_raw_content": false,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tavilySearchEndpoint, bytes.NewReader(requestBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", webSearchUserAgent)

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
			Title:       coalesceSearchText(r.Title, r.URL, "Untitled"),
			URL:         r.URL,
			Description: truncateStr(r.Content, 240),
		})
	}
	return results, nil
}
