package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// --- DuckDuckGo Search Provider ---

type duckDuckGoSearchProvider struct {
	maxResults int
	client     *http.Client
}

func newDuckDuckGoSearchProvider(maxResults int) *duckDuckGoSearchProvider {
	return &duckDuckGoSearchProvider{
		maxResults: normalizeProviderMaxResults(maxResults),
		client:     &http.Client{Timeout: time.Duration(searchTimeoutSeconds) * time.Second},
	}
}

func (p *duckDuckGoSearchProvider) Name() string { return searchProviderDuckDuckGo }

func (p *duckDuckGoSearchProvider) Search(ctx context.Context, params searchParams) ([]searchResult, error) {
	count := clampProviderResultCount(params.Count, p.maxResults)
	searchURL := fmt.Sprintf("https://html.duckduckgo.com/html/?q=%s", url.QueryEscape(params.Query))

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
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

	return extractDDGResults(string(body), count)
}

var (
	ddgLinkRe    = regexp.MustCompile(`<a[^>]*class="[^"]*result__a[^"]*"[^>]*href="([^"]+)"[^>]*>([\s\S]*?)</a>`)
	ddgSnippetRe = regexp.MustCompile(`<a class="result__snippet[^"]*".*?>([\s\S]*?)</a>`)
	htmlTagRe    = regexp.MustCompile(`<[^>]+>`)
)

func extractDDGResults(html string, count int) ([]searchResult, error) {
	linkMatches := ddgLinkRe.FindAllStringSubmatch(html, count+5)
	if len(linkMatches) == 0 {
		return nil, nil
	}

	snippetMatches := ddgSnippetRe.FindAllStringSubmatch(html, count+5)

	var results []searchResult
	for i := 0; i < len(linkMatches) && i < count; i++ {
		rawURL := linkMatches[i][1]
		title := strings.TrimSpace(htmlTagRe.ReplaceAllString(linkMatches[i][2], ""))

		// DDG wraps URLs with redirect — extract real URL from uddg= param
		if strings.Contains(rawURL, "uddg=") {
			if u, err := url.QueryUnescape(rawURL); err == nil {
				if _, after, ok := strings.Cut(u, "uddg="); ok {
					extracted := after
					// uddg value may have trailing &params
					if ampIdx := strings.Index(extracted, "&"); ampIdx != -1 {
						extracted = extracted[:ampIdx]
					}
					rawURL = extracted
				}
			}
		}

		desc := ""
		if i < len(snippetMatches) {
			desc = strings.TrimSpace(htmlTagRe.ReplaceAllString(snippetMatches[i][1], ""))
		}

		results = append(results, searchResult{
			Title:       title,
			URL:         rawURL,
			Description: desc,
		})
	}

	return results, nil
}
