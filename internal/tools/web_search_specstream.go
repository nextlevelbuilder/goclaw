package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// --- SpecStream Search Provider ---
//
// SpecStream returns live web results as JSON on a tenant-supplied key.
// Requests are GET with the key as a Bearer token; results arrive under
// "data.sources" with title/url/snippet fields. speed=fast asks for the
// low-latency tier, which is what a tool call in an agent loop wants.

type specstreamSearchProvider struct {
	apiKey     string
	maxResults int
	client     *http.Client
	endpoint   string
	now        func() time.Time
}

func newSpecStreamSearchProvider(apiKey string, maxResults int) *specstreamSearchProvider {
	return &specstreamSearchProvider{
		apiKey:     apiKey,
		maxResults: normalizeProviderMaxResults(maxResults),
		client:     &http.Client{Timeout: time.Duration(searchTimeoutSeconds) * time.Second},
		endpoint:   specstreamSearchEndpoint,
		now:        time.Now,
	}
}

func (p *specstreamSearchProvider) Name() string { return searchProviderSpecStream }

// The API takes two-letter ISO codes for country and language and rejects
// anything else, so a value like "zh-hans" is cut to "zh" and a value that
// cannot be cut is left out rather than failing the whole search.
var specstreamTwoLetterRe = regexp.MustCompile(`^[a-zA-Z]{2}(?:[-_]|$)`)

func specstreamTwoLetterCode(value string) string {
	v := strings.TrimSpace(value)
	if !specstreamTwoLetterRe.MatchString(v) {
		return ""
	}
	return v[:2]
}

// specstreamDateWindow turns the tool's freshness value into the after: and
// before: operators SpecStream reads in the query. Both bounds are inclusive
// days, so the YYYY-MM-DDtoYYYY-MM-DD range is honoured as written.
func specstreamDateWindow(freshness string, now time.Time) string {
	v := normalizeFreshness(freshness)
	day := now.UTC()
	switch v {
	case "":
		return ""
	case "pd":
		return "after:" + day.AddDate(0, 0, -1).Format("2006-01-02")
	case "pw":
		return "after:" + day.AddDate(0, 0, -7).Format("2006-01-02")
	case "pm":
		return "after:" + day.AddDate(0, -1, 0).Format("2006-01-02")
	case "py":
		return "after:" + day.AddDate(-1, 0, 0).Format("2006-01-02")
	}
	if m := freshnessRangeRe.FindStringSubmatch(v); len(m) == 3 {
		return "after:" + m[1] + " before:" + m[2]
	}
	return ""
}

func (p *specstreamSearchProvider) Search(ctx context.Context, params searchParams) ([]searchResult, error) {
	limit := clampProviderResultCount(params.Count, p.maxResults)

	query := params.Query
	if window := specstreamDateWindow(params.Freshness, p.now()); window != "" {
		query = strings.TrimSpace(query) + " " + window
	}

	q := url.Values{}
	q.Set("q", query)
	q.Set("num", strconv.Itoa(limit))
	q.Set("speed", "fast")
	if country := specstreamTwoLetterCode(params.Country); country != "" {
		q.Set("country", country)
	}
	if lang := specstreamTwoLetterCode(coalesceSearchText(params.SearchLang, params.UILang)); lang != "" {
		q.Set("language", lang)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.endpoint+"?"+q.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("User-Agent", "goclaw")

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
		// Errors come back as {"error":{"code":...,"message":...}}; surface the
		// message so the chain log says why, and fall back to the raw body.
		var errResp struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(body, &errResp) == nil && errResp.Error.Message != "" {
			return nil, fmt.Errorf("specstream API returned %d (%s): %s", resp.StatusCode, errResp.Error.Code, truncateStr(errResp.Error.Message, 200))
		}
		return nil, fmt.Errorf("specstream API returned %d: %s", resp.StatusCode, truncateStr(string(body), 200))
	}

	var ssResp struct {
		Data struct {
			Sources []struct {
				Title   string `json:"title"`
				URL     string `json:"url"`
				Snippet string `json:"snippet"`
			} `json:"sources"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &ssResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	results := make([]searchResult, 0, len(ssResp.Data.Sources))
	for _, r := range ssResp.Data.Sources {
		if len(results) >= limit {
			break
		}
		link := strings.TrimSpace(r.URL)
		if link == "" {
			continue
		}
		results = append(results, searchResult{
			Title:       coalesceSearchText(r.Title, link, "Untitled"),
			URL:         link,
			Description: truncateStr(strings.TrimSpace(r.Snippet), 240),
		})
	}
	return results, nil
}
