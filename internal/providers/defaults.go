package providers

import (
	"net/http"
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
func NewDefaultHTTPClient() *http.Client {
	return &http.Client{Transport: NewDefaultTransport()}
}
