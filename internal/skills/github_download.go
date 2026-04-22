package skills

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// Sentinel errors for the downloader.
var (
	ErrNotHTTPS        = errors.New("github.download: non-HTTPS URL rejected")
	ErrHostNotAllowed  = errors.New("github.download: host not in allowlist")
	ErrAssetTooLarge   = errors.New("github.download: asset exceeds max size")
	ErrTooManyRedirect = errors.New("github.download: too many redirects")
)

// allowedDownloadHosts is the SSRF allowlist for asset downloads.
var allowedDownloadHosts = map[string]bool{
	"github.com":                        true,
	"api.github.com":                    true,
	"objects.githubusercontent.com":     true,
	"release-assets.githubusercontent.com": true,
	"codeload.github.com":               true,
}

// validateDownloadURL ensures the URL is HTTPS and the host is allowlisted.
// Also blocks private/loopback IPs when the host is an IP literal.
func validateDownloadURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("github.download: parse url: %w", err)
	}
	if u.Scheme != "https" {
		return ErrNotHTTPS
	}
	host := strings.ToLower(u.Hostname())
	if !allowedDownloadHosts[host] {
		return fmt.Errorf("%w: %s", ErrHostNotAllowed, host)
	}
	// Block literal IPs as hostname (prevents raw-IP SSRF via rebinding tricks).
	if ip := net.ParseIP(host); ip != nil {
		return fmt.Errorf("%w: literal IP %s", ErrHostNotAllowed, host)
	}
	return nil
}

// DownloadAsset streams an asset over HTTPS to a temp file, validating the URL,
// enforcing a max byte cap, and computing SHA256 as it writes.
// Caller must remove the temp file.
func (c *GitHubClient) DownloadAsset(ctx context.Context, assetURL string, maxBytes int64) (string, string, error) {
	if err := validateDownloadURL(assetURL); err != nil {
		return "", "", err
	}
	if maxBytes <= 0 {
		maxBytes = 200 * 1024 * 1024
	}

	// Build a client that validates every redirect hop.
	// No Timeout here — it caps the whole request including body read, which
	// would abort large (hundreds of MB) downloads on modest connections.
	// The caller's context carries the correct deadline (install timeout).
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return ErrTooManyRedirect
			}
			return validateDownloadURL(req.URL.String())
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, assetURL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Accept", "application/octet-stream")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("github.download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", "", fmt.Errorf("github.download: status %d: %s",
			resp.StatusCode, strings.TrimSpace(string(body)))
	}

	tmp, err := os.CreateTemp("", "goclaw-gh-asset-*.bin")
	if err != nil {
		return "", "", err
	}
	tmpName := tmp.Name()
	_ = tmp.Chmod(0o600)

	h := sha256.New()
	// Read up to maxBytes+1 so we can detect overflow.
	limited := io.LimitReader(resp.Body, maxBytes+1)
	n, err := io.Copy(io.MultiWriter(tmp, h), limited)
	cerr := tmp.Close()
	if err != nil {
		os.Remove(tmpName)
		return "", "", fmt.Errorf("github.download: copy: %w", err)
	}
	if cerr != nil {
		os.Remove(tmpName)
		return "", "", cerr
	}
	if n > maxBytes {
		os.Remove(tmpName)
		return "", "", ErrAssetTooLarge
	}
	return tmpName, hex.EncodeToString(h.Sum(nil)), nil
}
