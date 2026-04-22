package skills

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Sentinel errors returned by the GitHub API client.
var (
	ErrGitHubNotFound     = errors.New("github: release not found")
	ErrGitHubUnauthorized = errors.New("github: unauthorized (check token)")
	ErrGitHubRateLimited  = errors.New("github: rate limited")
	ErrGitHubServer       = errors.New("github: server error")
)

// GitHubAsset describes a single release asset.
type GitHubAsset struct {
	Name        string `json:"name"`
	DownloadURL string `json:"browser_download_url"`
	SizeBytes   int64  `json:"size"`
	ContentType string `json:"content_type"`
}

// GitHubRelease is a simplified projection of the GitHub release payload.
type GitHubRelease struct {
	TagName     string        `json:"tag_name"`
	Name        string        `json:"name"`
	PublishedAt time.Time     `json:"published_at"`
	Prerelease  bool          `json:"prerelease"`
	Draft       bool          `json:"draft"`
	Assets      []GitHubAsset `json:"assets"`
}

// releaseCacheEntry is a single cached release lookup.
type releaseCacheEntry struct {
	data      any
	expiresAt time.Time
}

// GitHubClient is a minimal REST client for the GitHub Releases API.
// Supports optional bearer token (private repos + higher rate limit) and
// an in-memory 10-minute TTL cache keyed by "owner/repo:tag".
type GitHubClient struct {
	Token      string
	BaseURL    string // default "https://api.github.com" — overridable for tests
	HTTPClient *http.Client

	mu    sync.Mutex
	cache map[string]releaseCacheEntry
	ttl   time.Duration
}

// NewGitHubClient creates a client. If httpClient is nil, a default with 30s timeout is used.
func NewGitHubClient(token string) *GitHubClient {
	return &GitHubClient{
		Token:      token,
		BaseURL:    "https://api.github.com",
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
		cache:      make(map[string]releaseCacheEntry),
		ttl:        10 * time.Minute,
	}
}

func (c *GitHubClient) cacheGet(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.cache[key]
	if !ok || time.Now().After(e.expiresAt) {
		return nil, false
	}
	return e.data, true
}

// cacheMaxEntries is a SOFT sweep trigger, not a hard cap: once the map
// reaches this size we scan for expired entries and drop them before
// inserting the new one. If every entry is still live the map can briefly
// exceed the threshold — in practice the 10-minute TTL keeps growth bounded
// by the request rate. Prevents unbounded growth from many distinct repos
// being queried over long uptime.
const cacheMaxEntries = 256

func (c *GitHubClient) cacheSet(key string, v any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.cache) >= cacheMaxEntries {
		now := time.Now()
		for k, e := range c.cache {
			if now.After(e.expiresAt) {
				delete(c.cache, k)
			}
		}
	}
	c.cache[key] = releaseCacheEntry{data: v, expiresAt: time.Now().Add(c.ttl)}
}

// GetRelease fetches a single release by tag. If tag is empty, "latest" is used.
func (c *GitHubClient) GetRelease(ctx context.Context, owner, repo, tag string) (*GitHubRelease, error) {
	key := fmt.Sprintf("rel:%s/%s:%s", owner, repo, tag)
	if v, ok := c.cacheGet(key); ok {
		r := v.(*GitHubRelease)
		return r, nil
	}

	var path string
	if tag == "" {
		path = fmt.Sprintf("/repos/%s/%s/releases/latest",
			url.PathEscape(owner), url.PathEscape(repo))
	} else {
		// PathEscape the tag so characters valid in git refs but URL-special
		// (#, ?, %, +) don't silently corrupt the path (# → fragment, ? → query).
		path = fmt.Sprintf("/repos/%s/%s/releases/tags/%s",
			url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(tag))
	}

	var rel GitHubRelease
	if err := c.doJSON(ctx, path, &rel); err != nil {
		return nil, err
	}
	c.cacheSet(key, &rel)
	return &rel, nil
}

// ListReleases returns the most recent releases (at most `limit`, max 100).
func (c *GitHubClient) ListReleases(ctx context.Context, owner, repo string, limit int) ([]GitHubRelease, error) {
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}
	key := fmt.Sprintf("list:%s/%s:%d", owner, repo, limit)
	if v, ok := c.cacheGet(key); ok {
		return v.([]GitHubRelease), nil
	}
	path := fmt.Sprintf("/repos/%s/%s/releases?per_page=%d",
		url.PathEscape(owner), url.PathEscape(repo), limit)
	var releases []GitHubRelease
	if err := c.doJSON(ctx, path, &releases); err != nil {
		return nil, err
	}
	c.cacheSet(key, releases)
	return releases, nil
}

// doJSON performs a GET + JSON decode, mapping status codes to sentinel errors.
func (c *GitHubClient) doJSON(ctx context.Context, path string, out any) error {
	// Avoid shadowing the "net/url" package import used elsewhere in this file.
	apiURL := c.BaseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("github: http request failed: %w", err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusOK:
		// fall through
	case resp.StatusCode == http.StatusNotFound:
		return ErrGitHubNotFound
	case resp.StatusCode == http.StatusUnauthorized:
		return ErrGitHubUnauthorized
	case resp.StatusCode == http.StatusForbidden:
		// Rate limit check
		remaining := resp.Header.Get("X-RateLimit-Remaining")
		if remaining == "0" {
			reset := resp.Header.Get("X-RateLimit-Reset")
			if n, errConv := strconv.ParseInt(reset, 10, 64); errConv == nil {
				return fmt.Errorf("%w (resets at %s)", ErrGitHubRateLimited, time.Unix(n, 0).UTC().Format(time.RFC3339))
			}
			return ErrGitHubRateLimited
		}
		return ErrGitHubUnauthorized
	case resp.StatusCode == http.StatusTooManyRequests:
		// GitHub secondary rate limits (abuse detection, search, unauthenticated
		// bursts) return 429 rather than 403+X-RateLimit-Remaining:0. Map both
		// onto the same sentinel so the HTTP handler renders a 429 "rate limit
		// reached" instead of a 502 "failed to fetch releases".
		return ErrGitHubRateLimited
	case resp.StatusCode >= 500:
		return ErrGitHubServer
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("github: unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	// Cap the response body at a generous 8 MiB. GitHub's release/list
	// payloads are well under this (a 100-release list with rich asset
	// metadata sits around 1 MiB). Belt-and-braces in case a future caller
	// adds a path that could return a much larger document, or a
	// man-in-the-middle / misbehaving upstream sends an oversized body.
	const maxAPIResponseBytes = 8 * 1024 * 1024
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxAPIResponseBytes)).Decode(out); err != nil {
		return fmt.Errorf("github: decode response: %w", err)
	}
	return nil
}
