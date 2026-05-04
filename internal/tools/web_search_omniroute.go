package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type omniRouteSearchProvider struct {
	apiKey     string
	maxResults int
	baseURL    string
	client     *http.Client
}

func newOmniRouteSearchProvider(apiKey string, maxResults int, baseURL string) *omniRouteSearchProvider {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = omniRouteSearchEndpoint
	}
	return &omniRouteSearchProvider{
		apiKey:     apiKey,
		maxResults: normalizeProviderMaxResults(maxResults),
		baseURL:    strings.TrimRight(baseURL, "/"),
		client:     &http.Client{Timeout: time.Duration(searchTimeoutSeconds) * time.Second},
	}
}

func (p *omniRouteSearchProvider) Name() string { return searchProviderOmniRoute }

func (p *omniRouteSearchProvider) Search(ctx context.Context, params searchParams) ([]searchResult, error) {
	count := clampProviderResultCount(params.Count, p.maxResults)
	bodyJSON, err := json.Marshal(map[string]any{
		"query":     params.Query,
		"count":     count,
		"country":   params.Country,
		"freshness": normalizeFreshness(params.Freshness),
	})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL, bytes.NewReader(bodyJSON))
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
		return nil, fmt.Errorf("omniroute API returned %d: %s", resp.StatusCode, truncateStr(string(body), 200))
	}

	parsed, err := parseOmniRouteSearch(body)
	if err != nil {
		return nil, err
	}

	results := make([]searchResult, 0, min(count, len(parsed)))
	for i, r := range parsed {
		if i >= count {
			break
		}
		results = append(results, r)
	}
	return results, nil
}

func parseOmniRouteSearch(body []byte) ([]searchResult, error) {
	var direct struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Snippet string `json:"snippet"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &direct); err == nil && len(direct.Results) > 0 {
		out := make([]searchResult, 0, len(direct.Results))
		for _, r := range direct.Results {
			out = append(out, searchResult{
				Title:       coalesceSearchText(r.Title, r.URL, "Untitled"),
				URL:         r.URL,
				Description: truncateStr(coalesceSearchText(r.Snippet, r.Content), 240),
			})
		}
		return out, nil
	}

	var openAICompat struct {
		Data []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Snippet string `json:"snippet"`
			Content string `json:"content"`
			Text    string `json:"text"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &openAICompat); err == nil && len(openAICompat.Data) > 0 {
		out := make([]searchResult, 0, len(openAICompat.Data))
		for _, r := range openAICompat.Data {
			out = append(out, searchResult{
				Title:       coalesceSearchText(r.Title, r.URL, "Untitled"),
				URL:         r.URL,
				Description: truncateStr(coalesceSearchText(r.Snippet, r.Content, r.Text), 240),
			})
		}
		return out, nil
	}

	return nil, fmt.Errorf("parse response: unsupported OmniRoute search payload")
}
