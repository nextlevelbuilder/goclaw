package tools

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// --- Keenable Search Provider ---
//
// Keenable answers without a key: requests go to the public endpoint and must
// name the calling app in an X-Keenable-Title header. A tenant-supplied key is
// optional; when stored, requests go to the keyed endpoint with X-API-Key,
// which lifts the keyless rate limit. Like Parallel, the provider is absent
// from the default order and only runs when a tenant names it.
//
// The keyless quota is counted per egress IP, so all tenants of one install
// share it. After a 429 the provider holds back for the Retry-After window
// instead of re-probing on every call, and its error says whose quota ran out.

const (
	keenableSearchEndpoint       = "https://api.keenable.ai/v1/search"
	keenablePublicSearchEndpoint = "https://api.keenable.ai/v1/search/public"
	keenableAppTitle             = "goclaw"

	// keenableDefaultCooldown applies after a 429 without a usable
	// Retry-After; keenableMaxCooldown caps what a Retry-After can ask for.
	keenableDefaultCooldown = 30 * time.Second
	keenableMaxCooldown     = time.Hour
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

// keenableCooldowns remembers 429s until their Retry-After has passed. Keyless
// requests share one entry per endpoint, matching the per-IP quota every
// tenant of the install draws on; keyed requests get one entry per key. The
// state lives outside the provider because the tenant chain cache rebuilds
// providers every minute.
var keenableCooldowns = &keenableCooldown{until: make(map[string]time.Time)}

type keenableCooldown struct {
	mu    sync.Mutex
	until map[string]time.Time
}

// remaining returns how long requests for key should still be held back.
func (c *keenableCooldown) remaining(key string, now time.Time) time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	until, ok := c.until[key]
	if !ok {
		return 0
	}
	if !now.Before(until) {
		delete(c.until, key)
		return 0
	}
	return until.Sub(now)
}

func (c *keenableCooldown) hold(key string, until time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.until[key] = until
}

// cooldownKey names the quota a request draws on: the endpoint for keyless
// requests, the endpoint plus a hash of the key for keyed ones.
func (p *keenableSearchProvider) cooldownKey() string {
	if p.apiKey == "" {
		return p.endpoint
	}
	sum := sha256.Sum256([]byte(p.apiKey))
	return p.endpoint + "#" + hex.EncodeToString(sum[:8])
}

// keenableRetryAfter reads a 429's Retry-After (delta-seconds or HTTP-date).
// A missing or unparsable header gives keenableDefaultCooldown; the result is
// kept between one second and keenableMaxCooldown.
func keenableRetryAfter(h http.Header, now time.Time) time.Duration {
	wait := keenableDefaultCooldown
	if v := strings.TrimSpace(h.Get("Retry-After")); v != "" {
		if secs, err := strconv.Atoi(v); err == nil {
			wait = time.Duration(secs) * time.Second
		} else if at, err := http.ParseTime(v); err == nil {
			wait = at.Sub(now)
		}
	}
	return min(max(wait, time.Second), keenableMaxCooldown)
}

// rateLimitError tells a throttled Keenable apart from an empty result, both
// in the chain's warning log and when a caller forces provider=keenable.
func (p *keenableSearchProvider) rateLimitError(wait time.Duration) error {
	wait = max(wait, time.Second).Round(time.Second)
	if p.apiKey == "" {
		return fmt.Errorf("keenable rate limited (429): the keyless quota is per egress IP and shared by every tenant of this install; next try in %s, a stored API key lifts the limit", wait)
	}
	return fmt.Errorf("keenable rate limited (429) on the stored API key; next try in %s", wait)
}

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
	cooldownKey := p.cooldownKey()
	if wait := keenableCooldowns.remaining(cooldownKey, time.Now()); wait > 0 {
		return nil, p.rateLimitError(wait)
	}

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
	// Required on keyless requests (the public endpoint answers 400 without
	// it) and sent on keyed ones too: the key names the account, the title
	// names the calling app, and Keenable records both per request.
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
	if resp.StatusCode == http.StatusTooManyRequests {
		now := time.Now()
		wait := keenableRetryAfter(resp.Header, now)
		keenableCooldowns.hold(cooldownKey, now.Add(wait))
		return nil, p.rateLimitError(wait)
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
