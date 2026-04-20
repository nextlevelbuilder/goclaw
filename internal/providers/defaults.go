package providers

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// Provider-level defaults for HTTP clients and stream parsing.
const (
	// Deprecated: DefaultHTTPTimeout set a wall-clock socket timeout that prevented
	// ctx cancellation from unblocking bufio.Scanner. Use NewDefaultHTTPClient() instead.
	DefaultHTTPTimeout = 300 * time.Second

	// SSE stream scanner buffer sizes (OpenAI-compat, Anthropic, Codex).
	SSEScanBufInit = 64 * 1024   // 64KB initial buffer
	SSEScanBufMax  = 1024 * 1024 // 1MB max line for large tool call / thinking chunks

	// Stdio/JSONRPC scanner buffer sizes (Claude CLI, ACP).
	StdioScanBufInit = 256 * 1024       // 256KB initial buffer
	StdioScanBufMax  = 10 * 1024 * 1024 // 10MB max for large protocol messages
)

// NewDefaultTransport returns an http.Transport with per-stage timeouts but no
// overall deadline. The absence of Client.Timeout allows LLM streaming responses
// (extended thinking, long completions) to run indefinitely while ctx cancellation
// still terminates the request promptly via CtxBody.
func NewDefaultTransport() *http.Transport {
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ResponseHeaderTimeout: 180 * time.Second, // wait for first byte of response (3min for slow providers)
		IdleConnTimeout:       90 * time.Second, // close idle keep-alive connections
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
	}
}

// NewDefaultHTTPClient returns an *http.Client backed by NewDefaultTransport.
// No Client.Timeout is set — rely on ctx deadlines and Transport stage timeouts.
//
// When GOCLAW_ALLOW_PRIVATE_PROVIDER_URLS is not set, the transport uses an
// SSRF-safe dialer that rejects connections to private/loopback/link-local IPs
// at dial time — defense-in-depth against DNS rebinding after provider creation.
func NewDefaultHTTPClient() *http.Client {
	t := NewDefaultTransport()
	if !allowPrivateURLs() {
		t.DialContext = ssrfSafeDialContext
	}
	return &http.Client{Transport: t}
}

// allowPrivateURLs reports whether the operator opted in via env var.
func allowPrivateURLs() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("GOCLAW_ALLOW_PRIVATE_PROVIDER_URLS")))
	return v == "1" || v == "true" || v == "yes"
}

// ssrfSafeDialContext resolves the target hostname and rejects connections to
// private, loopback, or link-local IPs. This prevents DNS rebinding attacks
// where a hostname passed validation at provider-creation time but resolves
// to an internal IP at request time.
func ssrfSafeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	addrs, err := net.DefaultResolver.LookupHost(ctx, host)
	if err != nil {
		return nil, err
	}
	for _, a := range addrs {
		ip := net.ParseIP(a)
		if ip == nil {
			continue
		}
		if v4 := ip.To4(); v4 != nil {
			ip = v4
		}
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return nil, fmt.Errorf("ssrf blocked: %s resolves to private address %s", host, ip)
		}
	}
	return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, net.JoinHostPort(addrs[0], port))
}
