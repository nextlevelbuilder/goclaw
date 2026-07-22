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
	"os"
	"strings"
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
		// Benchmarking (RFC 2544)
		"198.18.0.0/15",
		// Reserved for future use
		"240.0.0.0/4",
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

// exemptableCIDRs are the private/loopback ranges that an operator-allowlisted
// MCP host may resolve into (see ValidateAllowingHosts). Cloud-metadata and
// link-local (169.254.0.0/16, fe80::/10), multicast, and unspecified ranges are
// deliberately NOT included: an allowlist entry must never open a path to the
// cloud metadata endpoint.
var exemptableCIDRs []*net.IPNet

func init() {
	for _, cidr := range []string{
		"127.0.0.0/8", "::1/128", // loopback
		"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "fc00::/7", // RFC 1918 + ULA
	} {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			panic(fmt.Sprintf("security: bad exemptable CIDR %q: %v", cidr, err))
		}
		exemptableCIDRs = append(exemptableCIDRs, ipNet)
	}
}

// isExemptable reports whether ip is in a private/loopback range that an
// operator-allowlisted MCP host is permitted to resolve into.
func isExemptable(ip net.IP) bool {
	for _, cidr := range exemptableCIDRs {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// SSRFAllowedCIDRsEnv names the operator escape hatch for deployments where a
// transparent proxy makes DNS resolution stop describing the real destination.
const SSRFAllowedCIDRsEnv = "GOCLAW_SSRF_ALLOWED_CIDRS"

// operatorAllowedCIDRs are ranges an operator has explicitly un-blocked via
// GOCLAW_SSRF_ALLOWED_CIDRS (comma-separated). Empty by default, which leaves
// the block list exactly as it ships.
//
// The case this exists for: a TUN/fake-IP proxy answers every DNS query with a
// synthetic address out of a reserved range and then routes that address to the
// real public host. The resolved IP is a handle, not a destination, so the
// "resolve, then judge the IP" model this package is built on reports a false
// positive on traffic that never touches an internal network. Without a way to
// say so, web_fetch is unusable in that environment and no configuration can
// fix it.
//
// This widens what LLM- and admin-supplied URLs can reach, so it is opt-in,
// logged at startup, and refuses the ranges an SSRF actually targets.
var operatorAllowedCIDRs []*net.IPNet

// neverAllowlistableCIDRs can never be un-blocked, whatever the operator sets.
// Cloud metadata (169.254.169.254) is the canonical SSRF target and must stay
// unreachable; multicast and unspecified are meaningless as proxy handles.
// This mirrors what exemptableCIDRs already refuses to cover.
var neverAllowlistableCIDRs []*net.IPNet

func init() {
	for _, cidr := range []string{
		"169.254.0.0/16", "fe80::/10", // link-local incl. cloud metadata
		"224.0.0.0/4", "ff00::/8", // multicast
		"0.0.0.0/32", "::/128", // unspecified
	} {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			panic(fmt.Sprintf("security: bad never-allowlistable CIDR %q: %v", cidr, err))
		}
		neverAllowlistableCIDRs = append(neverAllowlistableCIDRs, ipNet)
	}

	nets, rejected := parseOperatorAllowedCIDRs(os.Getenv(SSRFAllowedCIDRsEnv))
	operatorAllowedCIDRs = nets
	operatorAllowlistRejected = rejected
}

// operatorAllowlistRejected records entries refused at parse time so startup
// can report them. A silently dropped entry would read as "configured".
var operatorAllowlistRejected []string

// parseOperatorAllowedCIDRs parses a comma-separated CIDR list, dropping
// entries that are malformed or that overlap a never-allowlistable range.
// Returns the accepted nets and a human-readable reason per rejected entry.
func parseOperatorAllowedCIDRs(spec string) (nets []*net.IPNet, rejected []string) {
	for _, raw := range strings.Split(spec, ",") {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}
		_, ipNet, err := net.ParseCIDR(entry)
		if err != nil {
			rejected = append(rejected, fmt.Sprintf("%s (not a CIDR)", entry))
			continue
		}
		if blocked := overlappingNeverAllowlistable(ipNet); blocked != "" {
			rejected = append(rejected, fmt.Sprintf("%s (overlaps %s)", entry, blocked))
			continue
		}
		nets = append(nets, ipNet)
	}
	return nets, rejected
}

// overlappingNeverAllowlistable returns the never-allowlistable range that
// candidate overlaps, or "" when it overlaps none. Both directions are checked
// so neither a wider nor a narrower entry can slip a protected range through.
func overlappingNeverAllowlistable(candidate *net.IPNet) string {
	for _, protected := range neverAllowlistableCIDRs {
		if candidate.Contains(protected.IP) || protected.Contains(candidate.IP) {
			return protected.String()
		}
	}
	return ""
}

// SetOperatorAllowlistForTest replaces the operator allowlist with the parsed
// form of spec and returns a function restoring the previous value.
//
// This function MUST only be called from test code, and not from tests marked
// t.Parallel(): unlike allowLoopbackForTest it swaps slices rather than an
// atomic. It exists so packages that carry their own SSRF gate — internal/tools
// — can cover the allowlist without duplicating the parser.
func SetOperatorAllowlistForTest(spec string) func() {
	prevNets, prevRejected := operatorAllowedCIDRs, operatorAllowlistRejected
	operatorAllowedCIDRs, operatorAllowlistRejected = parseOperatorAllowedCIDRs(spec)
	return func() {
		operatorAllowedCIDRs, operatorAllowlistRejected = prevNets, prevRejected
	}
}

// OperatorAllowlistStatus reports the configured allowlist and any rejected
// entries, for the startup warning. Callers must not mutate the result.
func OperatorAllowlistStatus() (allowed []string, rejected []string) {
	for _, n := range operatorAllowedCIDRs {
		allowed = append(allowed, n.String())
	}
	return allowed, operatorAllowlistRejected
}

// isOperatorAllowed reports whether an operator has un-blocked ip's range.
func isOperatorAllowed(ip net.IP) bool {
	for _, cidr := range operatorAllowedCIDRs {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// IsOperatorAllowed reports whether an operator has explicitly un-blocked ip's
// range via GOCLAW_SSRF_ALLOWED_CIDRS.
//
// Exported for the separate SSRF check in internal/tools, which carries its own
// private-range list. Both gates have to honour the same operator setting, or
// relaxing one leaves the other rejecting the very traffic it was configured
// to permit.
func IsOperatorAllowed(ip net.IP) bool {
	return isOperatorAllowed(ip)
}

// isBlocked returns true if ip falls within any blocked CIDR.
//
// The operator allowlist is consulted first and deliberately applies here
// rather than only in validate(): NewSafeClient re-checks the pinned IP at dial
// time through this same function, so relaxing only the pre-flight check would
// pass validation and then fail to connect.
func isBlocked(ip net.IP) bool {
	if isOperatorAllowed(ip) {
		return false
	}
	for _, cidr := range blockedCIDRs {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// IsBlocked reports whether ip falls within any blocked CIDR (loopback,
// link-local including cloud-metadata 169.254.169.254, RFC 1918 private,
// multicast, and unspecified 0.0.0.0/:: ranges).
//
// Use this in provider-URL validation and dial-time guards to avoid
// duplicating the CIDR list across packages.
func IsBlocked(ip net.IP) bool {
	return isBlocked(ip)
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
	return validate(rawURL, allowLoopbackForTest.Load(), nil)
}

// ValidateAllowingHosts behaves like Validate but exempts an operator-configured
// set of trusted hostnames from the private/loopback IP block, so self-hosted MCP
// servers on a private network can be registered. Matching is case-insensitive on
// the pre-resolution hostname; cloud-metadata/link-local, multicast and
// unspecified ranges are still blocked even for allowlisted hosts. allowedHosts
// may be nil (identical to Validate). Intended ONLY for owner/admin MCP server
// config validation, never for agent-influenced fetch paths.
func ValidateAllowingHosts(rawURL string, allowedHosts map[string]bool) (*url.URL, net.IP, error) {
	return validate(rawURL, allowLoopbackForTest.Load(), allowedHosts)
}

// validate is the internal implementation. allowLoopback is set only in tests;
// allowedHosts is the operator MCP allowlist (nil for all other callers).
func validate(rawURL string, allowLoopback bool, allowedHosts map[string]bool) (*url.URL, net.IP, error) {
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

	hostAllowlisted := len(allowedHosts) > 0 && allowedHosts[strings.ToLower(host)]

	// If the host is already a literal IP, validate it directly.
	if ip := net.ParseIP(host); ip != nil {
		if !allowLoopback && isBlocked(ip) && !(hostAllowlisted && isExemptable(ip)) {
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

	if !allowLoopback && isBlocked(ip) && !(hostAllowlisted && isExemptable(ip)) {
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
