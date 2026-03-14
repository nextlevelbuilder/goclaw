package proxyserver

import (
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HopByHopHeaders are headers that should not be forwarded by proxies.
var HopByHopHeaders = []string{
	"Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Te",
	"Trailers",
	"Transfer-Encoding",
	"Upgrade",
}

// CreateDirector creates a Director function for httputil.ReverseProxy.
func CreateDirector(targetURL *url.URL, originalReq *http.Request) func(*http.Request) {
	return func(req *http.Request) {
		req.URL.Scheme = targetURL.Scheme
		req.URL.Host = targetURL.Host

		if originalReq != nil {
			req.URL.Path = originalReq.URL.Path
			req.URL.RawQuery = originalReq.URL.RawQuery
		}

		for _, h := range HopByHopHeaders {
			req.Header.Del(h)
		}
		req.Header.Del("Accept-Encoding")
		req.Host = targetURL.Host
	}
}

// CreateSimpleDirector creates a simple director that only sets the target URL.
func CreateSimpleDirector(targetURL *url.URL) func(*http.Request) {
	return func(req *http.Request) {
		req.URL.Scheme = targetURL.Scheme
		req.URL.Host = targetURL.Host
		req.Host = targetURL.Host

		for _, h := range HopByHopHeaders {
			req.Header.Del(h)
		}
	}
}

// FilterRequestHeaders returns only essential headers for the proxy request.
func FilterRequestHeaders(original http.Header) http.Header {
	filtered := make(http.Header)

	essentialHeaders := []string{
		"Content-Type",
		"Authorization",
		"Accept",
		"User-Agent",
		"api-key",
		"x-api-key",
	}

	for _, h := range essentialHeaders {
		if v := original.Get(h); v != "" {
			filtered.Set(h, v)
		}
	}

	for k, v := range original {
		if strings.HasPrefix(k, "X-") {
			filtered[k] = v
		}
	}

	return filtered
}

// NewPooledTransport creates an HTTP transport with connection pooling.
func NewPooledTransport(cfg *ProxyConfig) *http.Transport {
	return &http.Transport{
		MaxIdleConns:          cfg.ConnectionPoolLimit,
		MaxIdleConnsPerHost:   cfg.ConnectionPoolPerHost,
		MaxConnsPerHost:       0,
		IdleConnTimeout:       cfg.KeepaliveTimeout,
		ResponseHeaderTimeout: cfg.ReadTimeout,
		ExpectContinueTimeout: 1 * time.Second,
		DisableCompression:    true,
		ForceAttemptHTTP2:     true,
	}
}
