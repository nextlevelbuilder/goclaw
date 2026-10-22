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

// --- Keenable Search Provider ---
//
// Keenable answers without a key: requests go to the public endpoint and must
// name the calling app in an X-Keenable-Title header. A tenant-supplied key is
// optional; when stored, requests go to the keyed endpoint with X-API-Key,
// which lifts the keyless rate limit. Like Parallel, the provider is absent
// from the default order and only runs when a tenant names it.

const (
	keenableSearchEndpoint       = "https://api.keenable.ai/v1/search"
	keenablePublicSearchEndpoint = "https://api.keenable.ai/v1/search/public"
	keenableAppTitle             = "goclaw"
)

type keenableSearchProvider struct {
	apiKey     string
	maxResults int
	client     *http.Client
	endpoint   string
}

func newKeenableSearchProvider(apiKey string, maxResults int) *keenableSearchProvider {
	endpoint := keenablePublicSearchEndpoint
	if apiKey != "" {
		endpoint = keenableSearchEndpoint
	}
	return &keenableSearchProvider{
		apiKey:     apiKey,
		maxResults: normalizeProviderMaxResults(maxResults),
		client:     &http.Client{Timeout: time.Duration(searchTimeoutSeconds) * time.Second},
		endpoint:   endpoint,
	}
}

func (p *keenableSearchProvider) Name() string { return searchProviderKeenable }

// keenableDateRange maps the tool's freshness filter onto Keenable's
// published_after / published_before dates (YYYY-MM-DD, both inclusive).
// Shortcuts count back from now; a YYYY-MM-DDtoYYYY-MM-DD range is passed
// through. Anything else leaves both dates empty.
func keenableDateRange(freshness string, now time.Time) (after, before string) {
	v := normalizeFreshness(freshness)
	switch v {
	case "":
		return "", ""
	case "pd":
		return now.AddDate(0, 0, -1).Format(time.DateOnly), ""
	case "pw":
		return now.AddDate(0, 0, -7).Format(time.DateOnly), ""
	case "pm":
		return now.AddDate(0, -1, 0).Format(time.DateOnly), ""
	case "py":
		return now.AddDate(-1, 0, 0).Format(time.DateOnly), ""
	}
	if m := freshnessRangeRe.FindStringSubmatch(v); len(m) == 3 {
		return m[1], m[2]
	}
	return "", ""
}

func (p *keenableSearchProvider) Search(ctx context.Context, params searchParams) ([]searchResult, error) {
	limit := clampProviderResultCount(params.Count, p.maxResults)

	payload := map[string]any{
		"query":       params.Query,
		"max_results": limit,
	}
	after, before := keenableDateRange(params.Freshness, time.Now().UTC())
	if after != "" {
		payload["published_after"] = after
	}
	if before != "" {
		payload["published_before"] = before
	}
	requestBody, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(requestBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", keenableAppTitle)
	// Required on keyless requests (the public endpoint answers 400 without it).
	req.Header.Set("X-Keenable-Title", keenableAppTitle)
	if p.apiKey != "" {
		req.Header.Set("X-API-Key", p.apiKey)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("keenable API returned %d: %s", resp.StatusCode, truncateStr(string(body), 200))
	}

	var keenableResp struct {
		Results []struct {
			Title       string `json:"title"`
			URL         string `json:"url"`
			Snippet     string `json:"snippet"`
			Description string `json:"description"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &keenableResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	results := make([]searchResult, 0, limit)
	for _, r := range keenableResp.Results {
		if len(results) >= limit {
			break
		}
		link := strings.TrimSpace(r.URL)
		if link == "" {
			continue
		}
		// The text lives in snippet (multi-line page text); description is
		// read only as a fallback. Collapse whitespace to keep one line.
		text := strings.Join(strings.Fields(coalesceSearchText(r.Snippet, r.Description)), " ")
		results = append(results, searchResult{
			Title:       coalesceSearchText(r.Title, link, "Untitled"),
			URL:         link,
			Description: truncateStr(text, 240),
		})
	}
	return results, nil
}
