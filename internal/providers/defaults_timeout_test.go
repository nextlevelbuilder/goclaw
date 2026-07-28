package providers

import (
	"testing"
	"time"
)

// A reasoning model emits nothing while thinking, so ResponseHeaderTimeout —
// not any streaming timeout — is what decides whether its answer is reachable.
// At 180s, claude-opus-5-thinking with a ~110k-token prompt routinely lost the
// race: GoClaw reported "http2: timeout awaiting response headers" while
// 9router's usage log showed the same request answering fine
// (promptTokens=77194 → completionTokens=2789). Live 2026-07-28.
func TestResponseHeaderTimeoutAccommodatesReasoningModels(t *testing.T) {
	t.Parallel()
	tr := NewDefaultTransport()

	// The observed first-byte latency that failed at 180s. The timeout must sit
	// above it with room to spare.
	const observedSlowFirstByte = 180 * time.Second
	if tr.ResponseHeaderTimeout <= observedSlowFirstByte {
		t.Errorf("ResponseHeaderTimeout = %v, must exceed the %v first-byte latency that failed live",
			tr.ResponseHeaderTimeout, observedSlowFirstByte)
	}

	// It must stay bounded. RetryDo multiplies it by Attempts, and a workflow
	// step requeues up to MaxTaskDispatches times on top of that, so an
	// over-generous value turns one slow provider into an hours-long workflow.
	// 3 (retry) x 3 (requeue) x 300s = 45min worst case, which is the ceiling
	// this value was chosen against.
	const maxAcceptable = 300 * time.Second
	if tr.ResponseHeaderTimeout > maxAcceptable {
		t.Errorf("ResponseHeaderTimeout = %v, exceeds %v; retry x requeue multiplies this ~9x",
			tr.ResponseHeaderTimeout, maxAcceptable)
	}
}

// Removing ResponseHeaderTimeout entirely was considered and rejected: agent run
// contexts come from scheduler queue.go via context.WithCancel, so they carry no
// deadline. With no header timeout, a silently dead connection would hold its
// scheduler lane until the OS TCP keepalive expired (~2h), and workflow requeue
// could not rescue it because requeue only fires when a run ENDS. This timeout is
// the only bound on a request that never answers — it must stay non-zero.
func TestResponseHeaderTimeoutIsSet(t *testing.T) {
	t.Parallel()
	if got := NewDefaultTransport().ResponseHeaderTimeout; got == 0 {
		t.Fatal("ResponseHeaderTimeout is 0 (unbounded); nothing else bounds an LLM call, " +
			"so a dead connection would wedge its scheduler lane indefinitely")
	}
}

// The retry multiplier this timeout is sized against must not drift silently.
func TestDefaultRetryAttemptsMatchTimeoutSizing(t *testing.T) {
	t.Parallel()
	if got := DefaultRetryConfig().Attempts; got != 3 {
		t.Errorf("DefaultRetryConfig().Attempts = %d, want 3 — ResponseHeaderTimeout was sized "+
			"assuming a 3x retry multiplier; re-check that sizing if this changed", got)
	}
}
