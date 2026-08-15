package bitrix24

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
)

// Sentinel errors from derivePublicURL. Callers log-and-continue: capture
// failure must NOT block install completion (tokens are already minted by the
// time we get here).
var (
	errPublicURLEmpty   = errors.New("derivePublicURL: empty host")
	errPublicURLPrivate = errors.New("derivePublicURL: host is private/loopback")
)

// derivePublicURL extracts the gateway's externally reachable URL from a
// request that hit the install handler. Bitrix24 only invokes our install
// endpoint via the URL the portal admin pasted into the application config —
// so r.Host (or X-Forwarded-Host behind a reverse proxy) is by construction
// the URL Bitrix24 will use for all subsequent event callbacks.
//
// Scheme resolution priority:
//  1. X-Forwarded-Proto header (set by Cloudflare Tunnel / nginx), normalized:
//     "http"/"https" pass through as-is; "ws"/"wss" map to "http"/"https"
//     (same transport, describes a WebSocket upgrade rather than a scheme
//     change); any other value is ignored.
//  2. r.TLS != nil → https
//  3. Otherwise → http (honest to what we observed). If a reverse proxy is
//     terminating TLS without forwarding the proto header, fix the proxy
//     config — guessing https here would silently mask the misconfiguration.
//     Downstream imbot.register will reject http URLs explicitly.
//
// Host resolution priority:
//  1. X-Forwarded-Host (reverse proxy)
//  2. r.Host (direct connect)
//
// Private/loopback hosts are rejected to prevent the operator from
// accidentally pinning the portal to a URL Bitrix24 cannot reach (e.g.
// authorizing via a Tailscale URL when the public ingress is elsewhere).
func derivePublicURL(r *http.Request) (string, error) {
	scheme := "https"
	if r.TLS == nil {
		scheme = "http"
	}
	if proto := strings.ToLower(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))); proto != "" {
		switch proto {
		case "http", "https":
			scheme = proto
		case "ws":
			scheme = "http"
		case "wss":
			scheme = "https"
		}
		// any other value is left ignored — keeps the TLS-derived default
	}

	host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = strings.TrimSpace(r.Host)
	}
	if host == "" {
		return "", errPublicURLEmpty
	}

	// X-Forwarded-Host may contain a comma-separated list (RFC 7239 style); take
	// the first hop, which is the original client-facing host.
	if idx := strings.Index(host, ","); idx >= 0 {
		host = strings.TrimSpace(host[:idx])
	}

	// Strip port for the privacy check but keep the original host (with port)
	// for the URL string — a non-standard port is legitimate (e.g. dev tunnel).
	hostOnly := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		hostOnly = h
	}
	if isPrivateOrLoopback(hostOnly) {
		return "", errPublicURLPrivate
	}

	return scheme + "://" + host, nil
}

// isPrivateOrLoopback reports whether host (no port) refers to a network
// location that Bitrix24-side cannot reach. Hostnames that aren't literal IPs
// are assumed public — we don't DNS-resolve to inspect the underlying address
// because the lookup adds latency and a dev "localtunnel.example.com" pointing
// to 127.0.0.1 is still a valid public URL from Bitrix24's perspective.
func isPrivateOrLoopback(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()
}

// capturePublicURL is the install-handler glue: derive the gateway URL from
// the incoming request and persist it on the portal. Both errors and successes
// are best-effort logged; the caller continues regardless because the install
// (tokens) has already succeeded by the time we get here.
//
// Optionally promotes the URL into the gateway-wide snapshot when promote is
// non-nil. This path is trustworthy because /bitrix24/install only succeeds
// after Bitrix24's own OAuth-state validation — a forged Host header alone
// can't reach this code path.
func capturePublicURL(ctx context.Context, portal *Portal, req *http.Request, promote func(string)) {
	url, err := derivePublicURL(req)
	if err != nil {
		slog.Warn("bitrix24 install: derive public_url failed",
			"tenant", portal.TenantID(), "portal", portal.Name(),
			"host", req.Host, "x_forwarded_host", req.Header.Get("X-Forwarded-Host"),
			"err", err)
		return
	}
	if err := portal.UpdatePublicURL(ctx, url); err != nil {
		slog.Warn("bitrix24 install: persist public_url failed",
			"tenant", portal.TenantID(), "portal", portal.Name(),
			"url", url, "err", err)
	}
	if promote != nil {
		promote(url)
	}
}
