package tools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Matching TS src/agents/tools/web-fetch.ts constants.
const (
	defaultFetchMaxChars    = 60000
	defaultFetchMaxRedirect = 3
	defaultErrorMaxChars    = 4000
	fetchTimeoutSeconds     = 30
	fetchUserAgent          = "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_7_2) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

// WebFetchTool implements the web_fetch tool matching TS src/agents/tools/web-fetch.ts.
type WebFetchTool struct {
	maxChars       int
	cache          *webCache
	policy         string   // "allow_all" (default), "allowlist"
	allowedDomains []string // domains when policy="allowlist" (supports "*.example.com")
	blockedDomains []string // always checked regardless of policy (supports "*.example.com")
	mu             sync.RWMutex
}

// WebFetchConfig holds configuration for the web fetch tool.
type WebFetchConfig struct {
	MaxChars       int
	CacheTTL       time.Duration
	Policy         string   // "allow_all" (default), "allowlist"
	AllowedDomains []string // domains when policy="allowlist"
	BlockedDomains []string // always blocked regardless of policy
}

func NewWebFetchTool(cfg WebFetchConfig) *WebFetchTool {
	maxChars := cfg.MaxChars
	if maxChars <= 0 {
		maxChars = defaultFetchMaxChars
	}
	ttl := cfg.CacheTTL
	if ttl <= 0 {
		ttl = defaultCacheTTL
	}
	policy := cfg.Policy
	if policy == "" {
		policy = "allow_all"
	}
	return &WebFetchTool{
		maxChars:       maxChars,
		cache:          newWebCache(defaultCacheMaxEntries, ttl),
		policy:         policy,
		allowedDomains: cfg.AllowedDomains,
		blockedDomains: cfg.BlockedDomains,
	}
}

// UpdatePolicy replaces the domain policy at runtime (called via pub/sub on config change).
func (t *WebFetchTool) UpdatePolicy(policy string, allowed, blocked []string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if policy == "" {
		policy = "allow_all"
	}
	t.policy = policy
	t.allowedDomains = allowed
	t.blockedDomains = blocked
	slog.Info("web_fetch policy updated", "policy", policy, "allowed", len(allowed), "blocked", len(blocked))
}

// matchDomainList checks if a hostname matches any pattern in the list.
// Supports exact match ("github.com") and wildcard prefix ("*.example.com").
func matchDomainList(hostname string, patterns []string) bool {
	hostname = strings.ToLower(hostname)
	for _, pattern := range patterns {
		pattern = strings.ToLower(strings.TrimSpace(pattern))
		if pattern == hostname {
			return true
		}
		// Wildcard: *.example.com matches sub.example.com, a.b.example.com
		if strings.HasPrefix(pattern, "*.") {
			suffix := pattern[1:] // ".example.com"
			if strings.HasSuffix(hostname, suffix) && hostname != suffix[1:] {
				return true
			}
		}
	}
	return false
}

// isDomainAllowed checks if a hostname matches the allowlist.
func (t *WebFetchTool) isDomainAllowed(hostname string) bool {
	t.mu.RLock()
	domains := t.allowedDomains
	t.mu.RUnlock()
	return matchDomainList(hostname, domains)
}

// isDomainBlocked checks if a hostname matches the blocklist.
func (t *WebFetchTool) isDomainBlocked(hostname string) bool {
	t.mu.RLock()
	domains := t.blockedDomains
	t.mu.RUnlock()
	return matchDomainList(hostname, domains)
}

func (t *WebFetchTool) Name() string { return "web_fetch" }

func (t *WebFetchTool) Description() string {
	return "Fetch a URL and extract its content. Supports HTML (converted to markdown/text), JSON, and plain text. If content exceeds the character limit, full content is saved to a temp file — use shell or read_file to access it. Includes SSRF protection."
}

func (t *WebFetchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "HTTP or HTTPS URL to fetch.",
			},
			"extractMode": map[string]any{
				"type":        "string",
				"description": `Extraction mode ("markdown" or "text"). Default: "markdown".`,
				"enum":        []string{"markdown", "text"},
			},
			"maxChars": map[string]any{
				"type":        "number",
				"description": "Maximum characters to return (truncates when exceeded).",
				"minimum":     100.0,
			},
		},
		"required": []string{"url"},
	}
}

func (t *WebFetchTool) Execute(ctx context.Context, args map[string]any) *Result {
	rawURL, _ := args["url"].(string)
	if rawURL == "" {
		return ErrorResult("url is required")
	}

	// Validate URL scheme
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ErrorResult(fmt.Sprintf("invalid URL: %v", err))
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ErrorResult("only http and https URLs are supported")
	}
	if parsed.Host == "" {
		return ErrorResult("missing hostname in URL")
	}

	// SSRF protection
	if err := CheckSSRF(rawURL); err != nil {
		return ErrorResult(fmt.Sprintf("SSRF protection: %v", err))
	}

	hostname := parsed.Hostname()

	// Domain blocklist check (always enforced regardless of policy)
	if t.isDomainBlocked(hostname) {
		return ErrorResult(fmt.Sprintf("domain %q is blocked by policy", hostname))
	}

	// Domain allowlist check
	t.mu.RLock()
	policy := t.policy
	t.mu.RUnlock()
	if policy == "allowlist" && !t.isDomainAllowed(hostname) {
		return ErrorResult(fmt.Sprintf("domain %q is not in the allowed domains list", hostname))
	}

	extractMode := "markdown"
	if em, ok := args["extractMode"].(string); ok && (em == "markdown" || em == "text") {
		extractMode = em
	}

	maxChars := t.maxChars
	if mc, ok := args["maxChars"].(float64); ok && int(mc) >= 100 {
		maxChars = int(mc)
	}

	// Check cache (scoped per channel to prevent cross-channel cache poisoning)
	channel := ToolChannelFromCtx(ctx)
	cacheKey := fmt.Sprintf("fetch:%s:%s:%s:%d", channel, rawURL, extractMode, maxChars)
	if cached, ok := t.cache.get(cacheKey); ok {
		slog.Debug("web_fetch cache hit", "url", rawURL)
		return NewResult(cached)
	}

	// Fetch
	result, err := t.doFetch(ctx, rawURL, extractMode, maxChars, policy)
	if err != nil {
		errMsg := truncateStr(err.Error(), defaultErrorMaxChars)
		return ErrorResult(fmt.Sprintf("fetch failed: %s", errMsg))
	}

	wrapped := wrapExternalContent(result, "Web Fetch", true)
	t.cache.set(cacheKey, wrapped)
	return NewResult(wrapped)
}

func (t *WebFetchTool) doFetch(ctx context.Context, rawURL, extractMode string, maxChars int, policy string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", fetchUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	redirectCount := 0
	client := &http.Client{
		Timeout: time.Duration(fetchTimeoutSeconds) * time.Second,
		Transport: &http.Transport{
			ForceAttemptHTTP2:   true,
			MaxIdleConns:        10,
			IdleConnTimeout:     30 * time.Second,
			TLSHandshakeTimeout: 15 * time.Second,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			redirectCount++
			if redirectCount > defaultFetchMaxRedirect {
				return fmt.Errorf("stopped after %d redirects", defaultFetchMaxRedirect)
			}
			// Check SSRF on redirect target
			if err := CheckSSRF(req.URL.String()); err != nil {
				return fmt.Errorf("redirect SSRF protection: %w", err)
			}
			// Check domain blocklist on redirect target
			redirectHost := req.URL.Hostname()
			if t.isDomainBlocked(redirectHost) {
				return fmt.Errorf("redirect to %q blocked: domain is in blocklist", redirectHost)
			}
			// Check domain allowlist on redirect target
			if policy == "allowlist" && !t.isDomainAllowed(redirectHost) {
				return fmt.Errorf("redirect to %q blocked: domain not in allowlist", redirectHost)
			}
			return nil
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// Read enough HTML to reach <body> content — pages often have 30-50KB+ <head> sections.
	readLimit := int64(max(maxChars*10, 512*1024))
	limitReader := io.LimitReader(resp.Body, readLimit)
	body, err := io.ReadAll(limitReader)
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}

	contentType := resp.Header.Get("Content-Type")
	finalURL := resp.Request.URL.String()

	var text string
	var extractor string

	switch {
	case strings.Contains(contentType, "application/json"):
		text, extractor = extractJSON(body)

	case strings.Contains(contentType, "text/markdown"):
		text = string(body)
		extractor = "cf-markdown"
		if extractMode == "text" {
			text = markdownToText(text)
		}

	case strings.Contains(contentType, "text/html"),
		strings.Contains(contentType, "application/xhtml"):
		if extractMode == "markdown" {
			text = htmlToMarkdown(string(body))
			extractor = "html-to-markdown"
		} else {
			text = htmlToText(string(body))
			extractor = "html-to-text"
		}
		if text == "" && len(body) > 0 {
			text = "[No content extracted. The page may require JavaScript to render, " +
				"or returned a bot-protection challenge. Try using browser automation instead.]"
		}

	default:
		text = string(body)
		extractor = "raw"
	}

	// Format response metadata
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("URL: %s\n", finalURL))
	if finalURL != rawURL {
		sb.WriteString(fmt.Sprintf("Redirected from: %s\n", rawURL))
	}
	sb.WriteString(fmt.Sprintf("Status: %d\n", resp.StatusCode))
	sb.WriteString(fmt.Sprintf("Extractor: %s\n", extractor))

	// If content exceeds maxChars, save full content to a temp file
	if len(text) > maxChars {
		tmpPath, writeErr := writeWebFetchTempFile(text, finalURL)
		if writeErr != nil {
			// Fallback: truncate inline if temp file write fails
			slog.Warn("web_fetch: failed to write temp file, falling back to truncation", "error", writeErr)
			text = text[:maxChars]
			sb.WriteString(fmt.Sprintf("Truncated: true (limit: %d chars)\n", maxChars))
			sb.WriteString(fmt.Sprintf("Length: %d\n", len(text)))
			sb.WriteString("\n")
			sb.WriteString(text)
		} else {
			sb.WriteString(fmt.Sprintf("Content-Length: %d chars (exceeds %d char limit)\n", len(text), maxChars))
			sb.WriteString(fmt.Sprintf("Full-Content-File: %s\n", tmpPath))
			sb.WriteString(fmt.Sprintf("Length: %d\n", maxChars))
			sb.WriteString("\n")
			sb.WriteString(text[:maxChars])
			sb.WriteString(fmt.Sprintf("\n\n[Content truncated at %d chars. Full content (%d chars) saved to: %s — use shell/read_file to access the rest.]",
				maxChars, len(text), tmpPath))
		}
	} else {
		sb.WriteString(fmt.Sprintf("Length: %d\n", len(text)))
		sb.WriteString("\n")
		sb.WriteString(text)
	}

	return sb.String(), nil
}

// writeWebFetchTempFile saves fetched content to a temp file with security sanitization.
// Returns the file path. The file is created in os.TempDir() with a random name
// to prevent path prediction attacks.
func writeWebFetchTempFile(content, sourceURL string) (string, error) {
	// Generate cryptographically random filename to prevent path prediction
	var randBytes [8]byte
	if _, err := rand.Read(randBytes[:]); err != nil {
		return "", fmt.Errorf("generate random name: %w", err)
	}
	filename := fmt.Sprintf("web-fetch-%s.txt", hex.EncodeToString(randBytes[:]))
	tmpPath := filepath.Join(os.TempDir(), filename)

	// Sanitize content: strip any potential prompt injection markers
	sanitized := sanitizeMarkers(content)

	// Write with restrictive permissions (owner read/write only)
	if err := os.WriteFile(tmpPath, []byte(sanitized), 0600); err != nil {
		return "", fmt.Errorf("write temp file: %w", err)
	}

	slog.Info("web_fetch: content saved to temp file",
		"path", tmpPath,
		"chars", len(sanitized),
		"source_url", sourceURL,
	)
	return tmpPath, nil
}
