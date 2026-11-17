package mattermost

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/security"
)

// allowPrivateEnv lets an operator connect to a Mattermost server on a private
// network, mirroring GOCLAW_ALLOW_PRIVATE_PROVIDER_URLS for LLM providers.
const allowPrivateEnv = "GOCLAW_ALLOW_PRIVATE_MATTERMOST_URLS"

const maxRedirects = 5

// errBlockedAddress marks a connection refused by the egress policy.
var errBlockedAddress = errors.New("mattermost server address is not allowed")

// allowPrivateFn reports whether private and loopback server addresses are
// allowed. Evaluated once; replaced in tests.
var allowPrivateFn = sync.OnceValue(func() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(allowPrivateEnv)))
	allowed := v == "1" || v == "true" || v == "yes"
	if allowed {
		slog.Warn("security.mattermost.private_allowed", "env", allowPrivateEnv)
	}
	return allowed
})

// checkDialIP applies the egress policy to the IP actually being dialed.
// Everything security.IsBlocked allows passes (including ranges an operator
// opened with GOCLAW_SSRF_ALLOWED_CIDRS). Private and loopback addresses pass
// only with allowPrivateEnv; link-local (cloud metadata), multicast,
// unspecified and reserved ranges never do.
func checkDialIP(ip net.IP) error {
	if !security.IsBlocked(ip) {
		return nil
	}
	if allowPrivateFn() && (ip.IsPrivate() || ip.IsLoopback()) {
		return nil
	}
	slog.Warn("security.mattermost.ssrf_block", "ip", ip.String())
	if ip.IsPrivate() || ip.IsLoopback() {
		return fmt.Errorf("%w: %s is a private address (set %s=true for an internal server)", errBlockedAddress, ip, allowPrivateEnv)
	}
	return fmt.Errorf("%w: %s is in a blocked range", errBlockedAddress, ip)
}

// dialControl runs after DNS resolution with the concrete ip:port, so the
// check covers redirects and DNS rebinding as well as the configured URL.
func dialControl(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("split dial address %q: %w", address, err)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("dial address %q is not an IP", host)
	}
	return checkDialIP(ip)
}

// newDialer returns the dialer shared by the REST client and the websocket.
// No proxy is used: with a proxy the policy would judge the proxy address
// instead of the Mattermost server.
func newDialer(timeout time.Duration) *net.Dialer {
	return &net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second, Control: dialControl}
}

// newHTTPClient returns the REST client: egress policy at dial time and
// redirects limited to http(s), as file downloads may redirect to storage.
func newHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext:         newDialer(timeout).DialContext,
			TLSHandshakeTimeout: 10 * time.Second,
			IdleConnTimeout:     90 * time.Second,
			MaxIdleConnsPerHost: 4,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("too many redirects (%d)", len(via))
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return fmt.Errorf("redirect to non-http(s) scheme %q", req.URL.Scheme)
			}
			return nil
		},
	}
}
