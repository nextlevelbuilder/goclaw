package mcp

import (
	"context"
	"log/slog"
	"strings"
	"time"
)

// forceReconnectTimeout bounds the synchronous Initialize during a
// session-reset recovery so a slow server cannot wedge the goroutine.
const forceReconnectTimeout = 30 * time.Second

// isSessionUninitializedErr detects server-side responses that indicate the
// MCP session lifecycle was reset and the cached client must re-Initialize
// before further tool calls succeed.
//
// Background: FastMCP / mcp-spec stateful HTTP servers track an "initializing
// → initialized" state per Mcp-Session-Id. When the server restarts, GCs idle
// sessions, or scales down, the in-memory state vanishes; the client keeps
// reusing the same SID and the server treats it as a fresh "initializing"
// session that rejects `tools/call` until `notifications/initialized` arrives.
//
// The mcp-go transport only maps HTTP 404 → ErrSessionTerminated. FastMCP-
// style servers return HTTP 200 with a JSON-RPC error body instead, which
// surfaces here as a plain Go error. We string-match because there is no
// dedicated error code in the JSON-RPC spec for this lifecycle violation.
//
// Known phrasings (extend as new servers surface):
//   - "method <X> is invalid during session initialization" (FastMCP / Python mcp)
//   - "session not initialized" (mcp-go server, some Node implementations)
//   - "session terminated" (catch-all for mcp-go ErrSessionTerminated text)
func isSessionUninitializedErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "invalid during session initialization") ||
		strings.Contains(msg, "session not initialized") ||
		strings.Contains(msg, "session terminated")
}

// requestForceReconnect kicks off a fresh Initialize handshake out-of-band
// because a BridgeTool detected the server lost session state. Concurrent
// calls are deduped via reconnPending CAS so N failing tool calls in flight
// trigger exactly one reconnect.
//
// Runs asynchronously: the caller (BridgeTool.Execute) returns the original
// error to the agent loop without waiting. By the next tool call attempt the
// fresh client will be in place via the atomic clientPtr swap inside
// fullReconnect.
func (ss *serverState) requestForceReconnect(reason string) {
	if !ss.reconnPending.CompareAndSwap(false, true) {
		slog.Debug("mcp.session_reset.dedup",
			"server", ss.name, "reason", reason)
		return
	}

	slog.Warn("mcp.session_reset.detected",
		"server", ss.name,
		"transport", ss.transport,
		"reason", reason,
		"action", "force_reconnect")

	go func() {
		defer ss.reconnPending.Store(false)

		ctx, cancel := context.WithTimeout(context.Background(), forceReconnectTimeout)
		defer cancel()

		ss.connected.Store(false)
		start := time.Now()
		if fullReconnect(ctx, ss) {
			slog.Info("mcp.session_reset.recovered",
				"server", ss.name,
				"latency_ms", time.Since(start).Milliseconds())
			return
		}
		slog.Warn("mcp.session_reset.recovery_failed",
			"server", ss.name,
			"latency_ms", time.Since(start).Milliseconds(),
			"hint", "next health-loop tick will retry via standard reconnectWithBackoff")
	}()
}
