package providers

import (
	"net/http"
	"os"
	"strconv"
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

// Idle connection pooling, overridable for deployments that raise agent-run
// concurrency:
//
//	GOCLAW_HTTP_MAX_IDLE_CONNS=100
//	GOCLAW_HTTP_MAX_IDLE_CONNS_PER_HOST=10
//
// When nearly all traffic goes to one provider host, the per-host limit is the
// one that binds: every concurrent request past it gets a fresh TCP+TLS
// handshake, and its connection is closed rather than pooled on completion.
// A self-hosted or in-network provider has no reason to be throttled this way,
// so those deployments should raise it to match LaneMain.
const (
	defaultMaxIdleConns        = 100
	defaultMaxIdleConnsPerHost = 10
)

// transportEnv reads a positive int from an env var, falling back to defaultVal.
func transportEnv(key string, defaultVal int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return defaultVal
}

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
		MaxIdleConns:          transportEnv("GOCLAW_HTTP_MAX_IDLE_CONNS", defaultMaxIdleConns),
		MaxIdleConnsPerHost:   transportEnv("GOCLAW_HTTP_MAX_IDLE_CONNS_PER_HOST", defaultMaxIdleConnsPerHost),
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
