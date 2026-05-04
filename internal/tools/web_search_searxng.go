package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type searxngSearchProvider struct {
	apiKey     string
	maxResults int
	baseURL    string
	client     *http.Client
}

func newSearxngSearchProvider(apiKey string, maxResults int, baseURL string) *searxngSearchProvider {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = searxngSearchEndpoint
	}
	return &searxngSearchProvider{
		apiKey:     apiKey,
		maxResults: normalizeProviderMaxResults(maxResults),
		baseURL:    strings.TrimRight(baseURL, "/"),
		client:     &http.Client{Timeout: time.Duration(searchTimeoutSeconds) * time.Second},
	}
}

func (p *searxngSearchProvider) Name() string { return searchProviderSearxng }

func (p *searxngSearchProvider) Search(ctx context.Context, params searchParams) ([]searchResult, error) {
	count := clampProviderResultCount(params.Count, p.maxResults)

	q := url.Values{}
	q.Set("q", params.Query)
	q.Set("format", "json")
	q.Set("language", coalesceSearchText(params.SearchLang, params.UILang, "en"))
	q.Set("pageno", "1")

	reqURL := p.baseURL + "?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", webSearchUserAgent)
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

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
		return nil, fmt.Errorf("searxng API returned %d: %s", resp.StatusCode, truncateStr(string(body), 200))
	}

	var searxResp struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &searxResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	results := make([]searchResult, 0, min(count, len(searxResp.Results)))
	for i, r := range searxResp.Results {
		if i >= count {
			break
		}
		results = append(results, searchResult{
			Title:       coalesceSearchText(r.Title, r.URL, "Untitled"),
			URL:         r.URL,
			Description: truncateStr(r.Content, 240),
		})
	}
	return results, nil
}
