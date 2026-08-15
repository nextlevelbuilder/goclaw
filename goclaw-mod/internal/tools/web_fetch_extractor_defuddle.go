package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	defuddleBaseURL = "https://fetch.goclaw.sh/"
	defuddleTimeout = 10 * time.Second
	defuddleMaxBody = 1 << 20 // 1MB
)

// DefuddleExtractor calls the fetch.goclaw.sh Cloudflare Worker to extract
// clean markdown via Defuddle. The CF Worker handles HTTP fetch + content extraction.
type DefuddleExtractor struct {
	baseURL string
	client  *http.Client
}

// NewDefuddleExtractorFromEntry creates a DefuddleExtractor from chain settings.
func NewDefuddleExtractorFromEntry(entry ExtractorEntry) *DefuddleExtractor {
	baseURL := entry.BaseURL
	if baseURL == "" {
		baseURL = defuddleBaseURL
	}
	// Ensure trailing slash for URL construction.
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}
	timeout := time.Duration(entry.Timeout) * time.Second
	if timeout <= 0 {
		timeout = defuddleTimeout
	}
	return newDefuddleExtractor(baseURL, timeout)
}

// newDefuddleExtractor creates a DefuddleExtractor with custom base URL and timeout (for testing).
func newDefuddleExtractor(baseURL string, timeout time.Duration) *DefuddleExtractor {
	return &DefuddleExtractor{
		baseURL: baseURL,
		client: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				ForceAttemptHTTP2:   true,
				MaxIdleConns:        5,
				IdleConnTimeout:     30 * time.Second,
				TLSHandshakeTimeout: 10 * time.Second,
			},
		},
	}
}

func (d *DefuddleExtractor) Name() string { return "defuddle" }

// Extract sends a GET request to fetch.goclaw.sh/<domain>/<path> (no scheme)
// and returns the plain markdown response.
func (d *DefuddleExtractor) Extract(ctx context.Context, rawURL string) (string, error) {
	// Strip scheme: https://example.com/path → example.com/path
	target := strings.TrimPrefix(strings.TrimPrefix(rawURL, "https://"), "http://")
	fetchURL := d.baseURL + target

	// Context timeout ensures cancellation propagates even if transport ignores client.Timeout.
	ctx, cancel := context.WithTimeout(ctx, d.client.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", fetchURL, nil)
	if err != nil {
		return "", fmt.Errorf("create defuddle request: %w", err)
	}
	req.Header.Set("User-Agent", fetchUserAgent)

	resp, err := d.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("defuddle fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("defuddle returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, defuddleMaxBody))
	if err != nil {
		return "", fmt.Errorf("read defuddle response: %w", err)
	}

	return string(body), nil
}
