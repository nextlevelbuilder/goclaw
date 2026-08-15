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

type exaSearchProvider struct {
	apiKey     string
	maxResults int
	client     *http.Client
}

func newExaSearchProvider(apiKey string, maxResults int) *exaSearchProvider {
	return &exaSearchProvider{
		apiKey:     apiKey,
		maxResults: normalizeProviderMaxResults(maxResults),
		client:     &http.Client{Timeout: time.Duration(searchTimeoutSeconds) * time.Second},
	}
}

func (p *exaSearchProvider) Name() string { return searchProviderExa }

func (p *exaSearchProvider) Search(ctx context.Context, params searchParams) ([]searchResult, error) {
	requestBody, err := json.Marshal(map[string]any{
		"query":      params.Query,
		"type":       "auto",
		"numResults": clampProviderResultCount(params.Count, p.maxResults),
		"text":       true,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, exaSearchEndpoint, bytes.NewReader(requestBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", webSearchUserAgent)
	req.Header.Set("x-api-key", p.apiKey)

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
		return nil, fmt.Errorf("exa API returned %d: %s", resp.StatusCode, truncateStr(string(body), 200))
	}

	var exaResp struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Text    string `json:"text"`
			Summary string `json:"summary"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &exaResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	results := make([]searchResult, 0, len(exaResp.Results))
	for _, r := range exaResp.Results {
		results = append(results, searchResult{
			Title:       coalesceSearchText(r.Title, r.URL, "Untitled"),
			URL:         r.URL,
			Description: truncateStr(coalesceSearchText(r.Text, r.Summary, ""), 240),
		})
	}
	return results, nil
}
