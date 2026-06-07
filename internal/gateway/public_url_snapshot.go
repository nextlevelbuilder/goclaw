package gateway

import (
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
)

// PublicURLSnapshot remembers the gateway's externally reachable base URL,
// learned from incoming HTTP requests. Concurrent-safe via atomic.Value.
//
// Purpose: features that need to advertise URLs back to external systems
// (e.g. Bitrix24 portal install links) don't want to require operators to
// configure GOCLAW_PUBLIC_URL by hand. The gateway already sees the public
// URL on every admin request — Cloudflare Tunnel / nginx forward the public
// Host header — so we just snapshot it.
//
// Trust model: callers MUST only invoke Set / Update from authenticated code
// paths (e.g. handleConnect after token validation, or the /bitrix24/install
// handler after OAuth state matches a known portal). Setting from an
// unauthenticated request would let `Host: evil.com /health` poison the URL
// that ends up in OAuth callback links and silently leak tokens to an
// attacker-controlled host.
type PublicURLSnapshot struct {
	v atomic.Pointer[string]
}

// NewPublicURLSnapshot returns an empty snapshot. Get() will return "" until
// the first request flows through Update.
func NewPublicURLSnapshot() *PublicURLSnapshot {
	return &PublicURLSnapshot{}
}

// Get returns the most recently observed public URL, or "" if no request
// has flowed through Update yet.
func (s *PublicURLSnapshot) Get() string {
	if p := s.v.Load(); p != nil {
		return *p
	}
	return ""
}

// Set replaces the stored URL. Intended for tests and for explicit override
// from config; production updates flow through SetIfPublic from
// authenticated WS connections.
func (s *PublicURLSnapshot) Set(url string) {
	url = strings.TrimSpace(url)
	if url == "" {
		return
	}
	s.v.Store(&url)
}

// SetIfPublic stores the URL only if its host is a routable public address.
// Hosts that resolve to loopback, RFC1918 private space, link-local, or the
// literal "localhost" are skipped. This protects the shared snapshot from a
// developer's tunneled/private session corrupting the install URL handed
// back to other admins. Returns true if the URL was accepted.
//
// Callers receiving the URL from any partially-trusted source (e.g. an
// authenticated admin's WS upgrade Host header) should use this instead of
// Set. Reserve Set for fully-trusted sources like explicit operator config.
func (s *PublicURLSnapshot) SetIfPublic(rawURL string) bool {
	host := hostFromURL(rawURL)
	if host == "" || hostIsPrivateOrLoopback(host) {
		return false
	}
	s.Set(rawURL)
	return true
}

// hostFromURL extracts the lowercase host (no port) from a URL like
// "https://goclaw.tamgiac.com:8443". Returns "" when not parseable.
func hostFromURL(rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Host == "" {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(u.Hostname()))
}

// hostIsPrivateOrLoopback mirrors the bitrix24 package's check (duplicated
// because internal/gateway cannot import internal/channels/bitrix24 without
// a package cycle). Treats "localhost" and "*.localhost" as private even
// though they may resolve elsewhere in some setups — RFC 6761 reserves
// .localhost for loopback.
func hostIsPrivateOrLoopback(host string) bool {
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

// Update derives "scheme://host" from a request and stores it. Returns the
// derived value, or "" when the request doesn't carry enough info to build
// a meaningful URL (e.g. missing Host header).
//
// Localhost / loopback hosts are accepted here (the snapshot is general-
// purpose; specific consumers like the Bitrix install URL builder can layer
// their own rejection on top).
func (s *PublicURLSnapshot) Update(r *http.Request) string {
	url := derivePublicURLFromRequest(r)
	if url == "" {
		return ""
	}
	// Avoid a write when the value is unchanged — keeps the atomic pointer
	// stable for readers that compare by pointer identity in hot paths.
	if cur := s.Get(); cur == url {
		return url
	}
	s.v.Store(&url)
	return url
}

// Middleware returns an http.Handler that snapshots the public URL from each
// inbound request before delegating to next. Mount AFTER any auth middleware
// so unauthenticated probes can't pin a value.
func (s *PublicURLSnapshot) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.Update(r)
		next.ServeHTTP(w, r)
	})
}

// derivePublicURLFromRequest is a copy of bitrix24.derivePublicURL minus the
// private/loopback rejection. We can't import the bitrix24 package from
// internal/gateway (would create a cycle with channels → gateway). The logic
// is small enough that duplication is cheaper than introducing a third
// shared package just for one function.
func derivePublicURLFromRequest(r *http.Request) string {
	scheme := "https"
	if proto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); proto != "" {
		scheme = strings.ToLower(proto)
	} else if r.TLS == nil {
		scheme = "http"
	}
	host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = strings.TrimSpace(r.Host)
	}
	if host == "" {
		return ""
	}
	if idx := strings.Index(host, ","); idx >= 0 {
		host = strings.TrimSpace(host[:idx])
	}
	return scheme + "://" + host
}
