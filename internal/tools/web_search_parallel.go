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

const parallelSearchEndpoint = "https://search.parallel.ai/mcp"

type parallelSearchProvider struct {
	maxResults int
	client     *http.Client
	endpoint   string
}

func newParallelSearchProvider(maxResults int) *parallelSearchProvider {
	return &parallelSearchProvider{
		maxResults: normalizeProviderMaxResults(maxResults),
		client:     &http.Client{Timeout: time.Duration(searchTimeoutSeconds) * time.Second},
		endpoint:   parallelSearchEndpoint,
	}
}

func (p *parallelSearchProvider) Name() string { return searchProviderParallel }

type parallelRPCResponse struct {
	Result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	} `json:"result"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type parallelSearchPayload struct {
	Results []struct {
		URL      string   `json:"url"`
		Title    string   `json:"title"`
		Excerpts []string `json:"excerpts"`
	} `json:"results"`
}

func (p *parallelSearchProvider) Search(ctx context.Context, params searchParams) ([]searchResult, error) {
	limit := clampProviderResultCount(params.Count, p.maxResults)
	requestBody, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "web_search",
			"arguments": map[string]any{
				"objective":      params.Query,
				"search_queries": []string{params.Query},
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(requestBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", webSearchUserAgent)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("parallel Search MCP returned %d", resp.StatusCode)
	}

	var rpc parallelRPCResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&rpc); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	if rpc.Error != nil {
		return nil, fmt.Errorf("parallel Search MCP error %d: %s", rpc.Error.Code, rpc.Error.Message)
	}
	if rpc.Result.IsError {
		return nil, fmt.Errorf("parallel web_search returned an error")
	}

	results := make([]searchResult, 0, limit)
	foundText := false
	for _, content := range rpc.Result.Content {
		if content.Type != "text" || strings.TrimSpace(content.Text) == "" {
			continue
		}
		foundText = true

		var payload parallelSearchPayload
		if err := json.Unmarshal([]byte(content.Text), &payload); err != nil {
			return nil, fmt.Errorf("parse search results: %w", err)
		}
		for _, raw := range payload.Results {
			if len(results) >= limit {
				break
			}
			url := strings.TrimSpace(raw.URL)
			if url == "" {
				continue
			}
			description := ""
			for _, excerpt := range raw.Excerpts {
				if excerpt = strings.TrimSpace(excerpt); excerpt != "" {
					description = truncateStr(excerpt, 240)
					break
				}
			}
			results = append(results, searchResult{
				Title:       coalesceSearchText(raw.Title, url, "Untitled"),
				URL:         url,
				Description: description,
			})
		}
		if len(results) >= limit {
			break
		}
	}
	if !foundText {
		return nil, fmt.Errorf("parallel Search MCP response contained no text results")
	}
	return results, nil
}
