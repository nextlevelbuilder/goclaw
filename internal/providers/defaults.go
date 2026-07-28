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

	// SSE stream reader initial buffer size (OpenAI-compat, Anthropic, Codex).
	// No max: bufio.Reader.ReadString grows to fit a line of any length — a full
	// base64 image in a single image-generation SSE data line can exceed several MB.
	SSEScanBufInit = 64 * 1024 // 64KB initial buffer

	// Stdio/JSONRPC scanner buffer sizes (Claude CLI, ACP).
	StdioScanBufInit = 256 * 1024       // 256KB initial buffer
	StdioScanBufMax  = 10 * 1024 * 1024 // 10MB max for large protocol messages
)

// NewDefaultTransport returns an http.Transport with per-stage timeouts but no
// overall deadline. The absence of Client.Timeout allows LLM streaming responses
// (extended thinking, long completions) to run indefinitely once the response has
// STARTED, while ctx cancellation still terminates the request promptly via
// CtxBody.
//
// Note that ctx cancellation is a user/caller signal here, not a time bound:
// agent runs get their context from scheduler queue.go via context.WithCancel,
// so no deadline is attached. The stage timeouts below are therefore the only
// thing bounding a request that never answers.
func NewDefaultTransport() *http.Transport {
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		// Wait for the first byte of the response. This bounds a SINGLE attempt;
		// RetryDo wraps the call with Attempts: 3, so the worst case a caller
		// observes is ~3x this value, and a workflow step that requeues 3 times
		// multiplies it again. Keep it modest.
		//
		// Raised from 180s after live evidence 2026-07-28: a reasoning model
		// (claude-opus-5-thinking) emits nothing at all while thinking, so with a
		// ~110k-token prompt it routinely passed 3 minutes before its first byte.
		// GoClaw killed the connection and reported "http2: timeout awaiting
		// response headers" while 9router's own usage log showed the upstream
		// answering fine moments later (promptTokens=77194 → completionTokens=2789).
		// The request was healthy; only this deadline was too short.
		//
		// Deliberately NOT removed: nothing else bounds an LLM call. The scheduler
		// builds run contexts with context.WithCancel, not WithTimeout, so there is
		// no run-level deadline to fall back on — despite what this file's older
		// comment claimed. Without this timeout a silently dead connection would
		// hold its scheduler lane until the OS TCP keepalive gave up (~2h), and
		// requeue could not rescue it because requeue only fires when a run ends.
		ResponseHeaderTimeout: 300 * time.Second,
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
// SSRF protection for user-configured provider URLs is enforced at provider
// create/update time by validateProviderURL (resolves the host and rejects
// private/reserved IPs via security.IsBlocked). Dial-time DNS-rebinding
// hardening is tracked as a follow-up.
func NewDefaultHTTPClient() *http.Client {
	return &http.Client{Transport: NewDefaultTransport()}
}
