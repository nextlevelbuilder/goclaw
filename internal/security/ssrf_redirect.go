package security

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"syscall"
	"time"
)

// safeDialControl is a net.Dialer.Control hook that rejects a connection whose
// resolved destination IP falls in a blocked range. Control runs AFTER DNS
// resolution with the concrete ip:port that is about to be dialed, so it
// validates the *actual* connection target — closing both redirect-to-internal
// and DNS-rebinding gaps (the IP it checks is the IP that gets connected, with no
// TOCTOU window). allowLoopbackForTest bypasses the check in test code only.
func safeDialControl(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("ssrf: split dial address %q: %w", address, err)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("ssrf: dial address %q is not an IP", host)
	}
	if !allowLoopbackForTest.Load() && isBlocked(ip) {
		slog.Warn("security.ssrf_block", "reason", "blocked_dial_ip", "ip", ip.String())
		return fmt.Errorf("ssrf: dial IP %s is in a blocked range", ip)
	}
	return nil
}

// NewRedirectFollowingSafeClient returns an *http.Client that FOLLOWS up to
// maxRedirects redirects while validating the resolved destination IP of EVERY
// hop at dial time (via net.Dialer.Control), and refusing any non-http(s)
// redirect target.
//
// Unlike NewSafeClient — which refuses redirects outright and pins a single
// pre-validated IP — this client suits fetching an authenticated media URL that
// legitimately 3xx-redirects to a public CDN (e.g. Bitrix imbot.v2.File.download).
// Checking the resolved IP at the dial boundary (not the hostname string) means a
// redirect whose host resolves into a loopback, link-local (cloud-metadata),
// private, multicast, or unspecified range is refused even when the hostname
// looks public — and the check happens on the IP actually connected, so DNS
// rebinding cannot swap a public IP for a private one after a string check.
//
// The returned client is safe to share across goroutines.
func NewRedirectFollowingSafeClient(timeout time.Duration, maxRedirects int) *http.Client {
	dialer := &net.Dialer{Timeout: timeout, Control: safeDialControl}
	return &http.Client{
		Timeout:   timeout,
		Transport: &http.Transport{DialContext: dialer.DialContext},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("ssrf: too many redirects (%d)", len(via))
			}
			switch req.URL.Scheme {
			case "http", "https":
				return nil
			default:
				return fmt.Errorf("ssrf: redirect to non-http(s) scheme %q", req.URL.Scheme)
			}
		},
	}
}
