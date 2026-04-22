// Package security provides SSRF-safe HTTP utilities for outbound webhook calls.
// All production webhook HTTP clients MUST use NewSafeClient to prevent
// admin-configured hooks from probing internal infrastructure.
package security

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sync/atomic"
	"time"
)

// pinnedIPKey is a context key used to pass the pre-resolved IP from
// Validate into the custom DialContext, preventing DNS rebinding attacks.
type pinnedIPKey struct{}

// allowLoopbackForTest is a test-only bypass. Production code MUST never set
// this flag. Exposed via SetAllowLoopbackForTest exclusively for *_test.go.
// atomic.Bool keeps read/write safe when tests spawn goroutines that trigger
// outbound calls concurrently with the flag flip.
var allowLoopbackForTest atomic.Bool

// SetAllowLoopbackForTest enables or disables the loopback/private-CIDR bypass
// for tests. Call with true before tests that use httptest.NewServer, and
// always defer a call with false to restore the default.
//
// This function MUST only be called from test code. Production paths never
// set this flag — the zero value (false) is the safe default.
func SetAllowLoopbackForTest(allow bool) {
	allowLoopbackForTest.Store(allow)
}

// blockedCIDRs lists all CIDRs that must never be dialed.
var blockedCIDRs []*net.IPNet

func init() {
	cidrs := []string{
		// Loopback
		"127.0.0.0/8",
		"::1/128",
		// Link-local (includes cloud-metadata 169.254.169.254)
		"169.254.0.0/16",
		"fe80::/10",
		// Private (RFC 1918 + RFC 4193)
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"fc00::/7",
		// Multicast
		"224.0.0.0/4",
		"ff00::/8",
		// Unspecified
		"0.0.0.0/32",
		"::/128",
	}
	for _, cidr := range cidrs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			panic(fmt.Sprintf("security: bad CIDR %q: %v", cidr, err))
		}
		blockedCIDRs = append(blockedCIDRs, ipNet)
	}
}

// isBlocked returns true if ip falls within any blocked CIDR.
func isBlocked(ip net.IP) bool {
	for _, cidr := range blockedCIDRs {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// redactURL strips query string and userinfo for safe logging.
func redactURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "[unparseable]"
	}
	u.RawQuery = ""
	u.Fragment = ""
	u.User = nil
	return u.String()
}

// Validate parses rawURL, resolves the host once, and rejects loopback,
// link-local, private, multicast, unspecified, and cloud-metadata destinations.
// Returns the parsed URL and the pinned resolved IP that the caller should dial
// directly. Only http and https schemes are accepted.
//
// In test code, call SetAllowLoopbackForTest(true) before invoking this
// function to permit httptest.NewServer addresses (127.0.0.1).
func Validate(rawURL string) (*url.URL, net.IP, error) {
	return validate(rawURL, allowLoopbackForTest.Load())
}

// validate is the internal implementation; allowLoopback is set only in tests.
func validate(rawURL string, allowLoopback bool) (*url.URL, net.IP, error) {
	redacted := redactURL(rawURL)

	u, err := url.Parse(rawURL)
	if err != nil {
		slog.Warn("security.hook.ssrf_block", "url", redacted, "reason", "url_parse_error")
		return nil, nil, fmt.Errorf("ssrf: parse url: %w", err)
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		slog.Warn("security.hook.ssrf_block", "url", redacted, "reason", "non_http_scheme", "scheme", u.Scheme)
		return nil, nil, fmt.Errorf("ssrf: scheme %q not allowed (only http/https)", u.Scheme)
	}

	host := u.Hostname()
	if host == "" {
		slog.Warn("security.hook.ssrf_block", "url", redacted, "reason", "empty_host")
		return nil, nil, errors.New("ssrf: empty host")
	}

	// If the host is already a literal IP, validate it directly.
	if ip := net.ParseIP(host); ip != nil {
		if !allowLoopback && isBlocked(ip) {
			slog.Warn("security.hook.ssrf_block", "url", redacted, "reason", "blocked_ip", "ip", ip.String())
			return nil, nil, fmt.Errorf("ssrf: IP %s is in a blocked range", ip)
		}
		return u, ip, nil
	}

	// DNS resolution — pin the first returned IP.
	addrs, err := net.LookupHost(host)
	if err != nil {
		slog.Warn("security.hook.ssrf_block", "url", redacted, "reason", "dns_resolve_failed", "host", host)
		return nil, nil, fmt.Errorf("ssrf: resolve %q: %w", host, err)
	}
	if len(addrs) == 0 {
		slog.Warn("security.hook.ssrf_block", "url", redacted, "reason", "no_ips_resolved", "host", host)
		return nil, nil, fmt.Errorf("ssrf: %q resolved to no addresses", host)
	}

	ip := net.ParseIP(addrs[0])
	if ip == nil {
		return nil, nil, fmt.Errorf("ssrf: resolved address %q is not a valid IP", addrs[0])
	}

	if !allowLoopback && isBlocked(ip) {
		slog.Warn("security.hook.ssrf_block", "url", redacted, "reason", "blocked_resolved_ip", "host", host, "ip", ip.String())
		return nil, nil, fmt.Errorf("ssrf: %q resolved to blocked IP %s", host, ip)
	}

	return u, ip, nil
}

// WithPinnedIP stores the pinned IP in ctx so the safe DialContext can read it.
func WithPinnedIP(ctx context.Context, ip net.IP) context.Context {
	return context.WithValue(ctx, pinnedIPKey{}, ip)
}

// pinnedIPFrom retrieves the pinned IP from ctx. Returns nil if not set.
func pinnedIPFrom(ctx context.Context) net.IP {
	ip, _ := ctx.Value(pinnedIPKey{}).(net.IP)
	return ip
}

// NewSafeClient returns an *http.Client whose Transport.DialContext pins the
// destination to the IP stored in the request context via WithPinnedIP (so DNS
// rebinding cannot swap a public IP for a private one between Validate and Dial),
// refuses redirects, and applies the supplied per-request timeout.
// The returned client is safe to share across goroutines.
//
// Caller workflow:
//  1. Call Validate(url) → get pinnedIP
//  2. ctx = WithPinnedIP(ctx, pinnedIP)
//  3. http.NewRequestWithContext(ctx, ...)
//  4. client.Do(req)
func NewSafeClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: timeout}

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			pinnedIP := pinnedIPFrom(ctx)
			if pinnedIP == nil {
				// No pinned IP in context — reject for safety.
				return nil, errors.New("ssrf: no pinned IP in context; call security.WithPinnedIP before dialing")
			}

			// Defense-in-depth: re-check pinned IP against block list.
			// allowLoopbackForTest bypasses this check in test code only.
			if !allowLoopbackForTest.Load() && isBlocked(pinnedIP) {
				return nil, fmt.Errorf("ssrf: pinned IP %s is in a blocked range", pinnedIP)
			}

			// Replace the host portion of addr with the pinned IP,
			// preserving the port from addr.
			_, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, fmt.Errorf("ssrf: split host/port from %q: %w", addr, err)
			}
			pinnedAddr := net.JoinHostPort(pinnedIP.String(), port)
			return dialer.DialContext(ctx, network, pinnedAddr)
		},
	}

	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Never follow redirects — return the redirect response directly.
			return http.ErrUseLastResponse
		},
	}
}
