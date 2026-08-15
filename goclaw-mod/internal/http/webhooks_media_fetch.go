package http

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/security"
)

const (
	// webhookMediaMaxBytes is the maximum allowed media file size (25 MB).
	webhookMediaMaxBytes = 25 * 1024 * 1024

	// webhookMediaProbeTimeout is the deadline for the HEAD probe request.
	webhookMediaProbeTimeout = 15 * time.Second
)

// allowedMediaMIMETypes is the set of Content-Type values accepted for media attachments.
// Must be lowercase prefix-matched against the probed value.
var allowedMediaMIMETypes = map[string]bool{
	"image/jpeg":       true,
	"image/png":        true,
	"image/gif":        true,
	"image/webp":       true,
	"video/mp4":        true,
	"audio/mpeg":       true,
	"audio/ogg":        true,
	"application/pdf":  true,
}

// mediaProbeResult is returned by probeMediaURL on success.
type mediaProbeResult struct {
	// ContentType is the canonical MIME type from the HEAD response (trimmed of params).
	ContentType string
	// PinnedIP is the resolved IP from SSRF validation — callers may store for logging.
	PinnedIP net.IP
}

// mediaValidateError categories (callers map these to HTTP status codes).
type mediaValidateError struct {
	code    string // "ssrf" | "too_large" | "mime_denied"
	message string
}

func (e *mediaValidateError) Error() string { return e.message }

// probeMediaURL performs SSRF validation, DNS pinning, and a HEAD request to
// verify the media URL is reachable and within size + MIME constraints.
//
// Workflow:
//  1. security.Validate(rawURL) — rejects private/loopback ranges.
//  2. Build SafeClient with pinned IP via WithPinnedIP context.
//  3. HEAD request — parse Content-Length (≤25 MB) and Content-Type (allowlist).
//
// Returns (result, nil) on success, or (*mediaValidateError, error) on failure.
// On error, the returned error is always *mediaValidateError so callers can
// switch on .code for status-code selection.
func probeMediaURL(rawURL string) (*mediaProbeResult, error) {
	// Step 1: SSRF validation — resolve DNS and reject blocked CIDRs.
	_, pinnedIP, err := security.Validate(rawURL)
	if err != nil {
		return nil, &mediaValidateError{
			code:    "ssrf",
			message: fmt.Sprintf("media URL blocked by SSRF policy: %v", err),
		}
	}

	// Step 2: Build SSRF-safe client with pinned IP.
	client := security.NewSafeClient(webhookMediaProbeTimeout)

	// Create HEAD request. Context carries the pinned IP for the safe dialer.
	// We use context.Background here; the caller's request context is not passed
	// to avoid cancellation from the response write path racing with the probe.
	// This is acceptable — the probe has its own 15s timeout via NewSafeClient.
	req, err := http.NewRequest(http.MethodHead, rawURL, nil)
	if err != nil {
		return nil, &mediaValidateError{
			code:    "ssrf",
			message: fmt.Sprintf("media URL parse error: %v", err),
		}
	}
	// Inject pinned IP into request context so SafeClient can use it.
	req = req.WithContext(security.WithPinnedIP(req.Context(), pinnedIP))

	// Step 3: Execute HEAD request.
	resp, err := client.Do(req)
	if err != nil {
		return nil, &mediaValidateError{
			code:    "ssrf",
			message: fmt.Sprintf("media HEAD probe failed: %v", err),
		}
	}
	defer resp.Body.Close()

	// Step 4: Validate Content-Length if present.
	if clStr := resp.Header.Get("Content-Length"); clStr != "" {
		cl, parseErr := strconv.ParseInt(clStr, 10, 64)
		if parseErr == nil && cl > webhookMediaMaxBytes {
			return nil, &mediaValidateError{
				code:    "too_large",
				message: fmt.Sprintf("media file exceeds size limit (%d bytes > %d)", cl, webhookMediaMaxBytes),
			}
		}
	}

	// Step 5: Validate Content-Type against allowlist.
	rawCT := resp.Header.Get("Content-Type")
	mimeType := parseMIMEType(rawCT)
	if !allowedMediaMIMETypes[mimeType] {
		return nil, &mediaValidateError{
			code:    "mime_denied",
			message: fmt.Sprintf("media MIME type %q is not allowed", mimeType),
		}
	}

	return &mediaProbeResult{
		ContentType: mimeType,
		PinnedIP:    pinnedIP,
	}, nil
}

// parseMIMEType strips parameters from a Content-Type header value and returns
// the lowercase base type (e.g. "image/jpeg; charset=utf-8" → "image/jpeg").
func parseMIMEType(ct string) string {
	if ct == "" {
		return ""
	}
	// Split on ";" and take the first part.
	parts := strings.SplitN(ct, ";", 2)
	return strings.ToLower(strings.TrimSpace(parts[0]))
}
