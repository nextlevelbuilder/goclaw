// Package meow holds the deterministic core of the Meow Telegram autopilot:
// caption building, publish-time input validation, and the exactly-once
// publish orchestration. It depends only on the store interface + stdlib so it
// stays unit-testable without telegram/tools wiring.
package meow

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// DefaultButtonHostAllowlist is the exact set of hosts that may appear in
// inline post buttons — the only domains/bots the channels link to. It is a
// coarse first-layer gate; the authoritative check is exact-URL membership in
// each channel's registered button set (see ValidateButtonURL). Exact-host
// match (incl. specific subdomains), no suffix matching, so `t.me.evil.com` or
// `tonslot.site.evil.com` are rejected.
func DefaultButtonHostAllowlist() map[string]bool {
	hosts := []string{
		"t.me",
		"youtube.com", "www.youtube.com",
		"x.com",
		"facebook.com", "www.facebook.com",
		"tiktok.com", "www.tiktok.com",
		"onewallet.store",
		"play.tonslot.site",
		"jackpot.1dollar.day",
	}
	m := make(map[string]bool, len(hosts))
	for _, h := range hosts {
		m[h] = true
	}
	return m
}

// NewURLSet builds an exact-match set of registered button URLs (trimmed). The
// publish path passes a channel's own button-set URLs so only pre-vetted links
// can be sent — a correct host alone (e.g. any t.me/<bot>) is not enough.
func NewURLSet(urls []string) map[string]bool {
	m := make(map[string]bool, len(urls))
	for _, u := range urls {
		m[strings.TrimSpace(u)] = true
	}
	return m
}

// ValidateButtonURL enforces two layers on one inline-button URL:
//  1. shape: https only (rejects tg://, javascript:, http), non-empty non-IP
//     host, and host ∈ allowedHosts;
//  2. provenance: the exact URL must be one of the channel's registered button
//     URLs. This stops a phishing link that merely lives on an allowed host
//     (e.g. https://t.me/<attacker-bot>) from being published.
func ValidateButtonURL(raw string, registered, allowedHosts map[string]bool) error {
	trimmed := strings.TrimSpace(raw)
	u, err := url.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("button url %q: parse: %w", raw, err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("button url %q: scheme %q rejected (https only)", raw, u.Scheme)
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return fmt.Errorf("button url %q: empty host", raw)
	}
	if net.ParseIP(host) != nil {
		return fmt.Errorf("button url %q: IP host not allowed", raw)
	}
	if !allowedHosts[host] {
		return fmt.Errorf("button url %q: host %q not in allowlist", raw, host)
	}
	if !registered[trimmed] {
		return fmt.Errorf("button url %q not in this channel's registered button set", raw)
	}
	return nil
}

// ValidateImagePath resolves raw (following symlinks) and asserts it lives
// inside one of allowedRoots, guarding against publishing a file outside the
// draft-asset area. Returns the resolved absolute path. A symlink inside a root
// that points outside resolves to its real target and is therefore rejected.
// The file must exist and be a regular file.
func ValidateImagePath(raw string, allowedRoots []string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", fmt.Errorf("image path empty")
	}
	resolved, err := filepath.EvalSymlinks(raw)
	if err != nil {
		return "", fmt.Errorf("image path %q: resolve: %w", raw, err)
	}
	resolved = filepath.Clean(resolved)
	if info, err := os.Stat(resolved); err != nil || info.IsDir() {
		return "", fmt.Errorf("image path %q: not a readable file", raw)
	}
	for _, root := range allowedRoots {
		rr, err := filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		rr = filepath.Clean(rr)
		if resolved == rr || strings.HasPrefix(resolved, rr+string(os.PathSeparator)) {
			return resolved, nil
		}
	}
	return "", fmt.Errorf("image path %q resolves outside allowed roots", raw)
}
