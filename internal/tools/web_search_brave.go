package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// --- Brave Search Provider ---

type braveSearchProvider struct {
	apiKey     string
	maxResults int
	client     *http.Client
}

func newBraveSearchProvider(apiKey string, maxResults int) *braveSearchProvider {
	return &braveSearchProvider{
		apiKey:     apiKey,
		maxResults: normalizeProviderMaxResults(maxResults),
		client:     &http.Client{Timeout: time.Duration(searchTimeoutSeconds) * time.Second},
	}
}

func (p *braveSearchProvider) Name() string { return searchProviderBrave }

func (p *braveSearchProvider) Search(ctx context.Context, params searchParams) ([]searchResult, error) {
	count := clampProviderResultCount(params.Count, p.maxResults)

	q := url.Values{}
	q.Set("q", params.Query)
	q.Set("count", fmt.Sprintf("%d", count))

	if params.Country != "" {
		q.Set("country", params.Country)
	}
	if params.SearchLang != "" {
		q.Set("search_lang", params.SearchLang)
	}
	if params.UILang != "" {
		q.Set("ui_lang", params.UILang)
	}
	if f := normalizeFreshness(params.Freshness); f != "" {
		q.Set("freshness", f)
	}

	reqURL := braveSearchEndpoint + "?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", p.apiKey)

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
		return nil, fmt.Errorf("brave API returned %d: %s", resp.StatusCode, truncateStr(string(body), 200))
	}

	var braveResp struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}

	if err := json.Unmarshal(body, &braveResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	results := make([]searchResult, 0, len(braveResp.Web.Results))
	for _, r := range braveResp.Web.Results {
		results = append(results, searchResult{
			Title:       r.Title,
			URL:         r.URL,
			Description: r.Description,
		})
	}
	return results, nil
}
