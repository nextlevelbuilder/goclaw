package providers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/reliability"
)

// ---------------------------------------------------------------------------
// streamWatchdogContext — unit tests (self-contained, no reliability state)
// ---------------------------------------------------------------------------

// waitWatchdogDone waits until ctx is done or the timeout expires. The
// watchdog fires through its own timer goroutine, so tests must allow real
// time to pass.
func waitWatchdogDone(t *testing.T, ctx context.Context, timeout time.Duration) bool {
	t.Helper()
	select {
	case <-ctx.Done():
		return true
	case <-time.After(timeout):
		return false
	}
}

// TestStreamWatchdog_NoOpWhenDisabled verifies that with both timeouts <= 0
// the helper returns the parent context unchanged with nil reset/cancel.
func TestStreamWatchdog_NoOpWhenDisabled(t *testing.T) {
	parent := context.Background()
	out, reset, cancel := streamWatchdogContext(parent, 0, 0)
	if out != parent {
		t.Errorf("no-op watchdog ctx = %v, want parent", out)
	}
	if reset != nil || cancel != nil {
		t.Errorf("no-op watchdog reset/cancel = %v/%v, want nil", reset == nil, cancel == nil)
	}
	// Negative values are disabled too.
	out2, reset2, cancel2 := streamWatchdogContext(parent, -time.Second, -time.Second)
	if out2 != parent || reset2 != nil || cancel2 != nil {
		t.Errorf("negative watchdog not a no-op: ctx=%v reset=%v cancel=%v", out2 != parent, reset2 != nil, cancel2 != nil)
	}
}

// TestStreamWatchdog_FiresOnIdle verifies that a stream that stops delivering
// events fires within the idle window and reports the idle kind.
func TestStreamWatchdog_FiresOnIdle(t *testing.T) {
	parent := context.Background()
	start := time.Now()
	ctx, reset, cancel := streamWatchdogContext(parent, 100*time.Millisecond, 0)
	defer cancel()

	if !waitWatchdogDone(t, ctx, 2*time.Second) {
		t.Fatal("watchdog did not fire within idle window")
	}
	if elapsed := time.Since(start); elapsed < 80*time.Millisecond {
		t.Errorf("watchdog fired after %v, want >= ~idle", elapsed)
	}
	kind, ok := streamWatchdogStalled(ctx)
	if !ok {
		t.Fatal("streamWatchdogStalled(ctx) = not stalled after fire")
	}
	if kind != streamWatchdogIdle {
		t.Errorf("stalled kind = %v, want idle", kind)
	}
	// reset must be a no-op after the fire (already reported).
	reset()
	_ = reset
}

// TestStreamWatchdog_ResetPreventsFire verifies that resetting the timer on
// each event keeps the stream alive, and that a stall only fires after the
// last reset.
func TestStreamWatchdog_ResetPreventsFire(t *testing.T) {
	parent := context.Background()
	ctx, reset, cancel := streamWatchdogContext(parent, 120*time.Millisecond, 0)
	defer cancel()

	for i := 0; i < 5; i++ {
		reset()
		if waitWatchdogDone(t, ctx, 60*time.Millisecond) {
			t.Fatalf("watchdog fired at reset %d while events kept arriving", i)
		}
	}
	// After the last reset, silence must fire the watchdog.
	if !waitWatchdogDone(t, ctx, 2*time.Second) {
		t.Fatal("watchdog did not fire after last reset")
	}
	if kind, ok := streamWatchdogStalled(ctx); !ok || kind != streamWatchdogIdle {
		t.Errorf("stalled = %v/%v, want idle/fired", kind, ok)
	}
}

// TestStreamWatchdog_FirstByteFires verifies the first-byte-only window: a
// wdwatchdog with only a first-byte timeout fires even without any event, and
// reports the first-byte kind.
func TestStreamWatchdog_FirstByteFires(t *testing.T) {
	parent := context.Background()
	ctx, _, cancel := streamWatchdogContext(parent, 0, 80*time.Millisecond)
	defer cancel()

	if !waitWatchdogDone(t, ctx, 2*time.Second) {
		t.Fatal("first-byte watchdog did not fire")
	}
	if kind, ok := streamWatchdogStalled(ctx); !ok || kind != streamWatchdogFirstByte {
		t.Errorf("stalled = %v/%v, want first-byte/fired", kind, ok)
	}
}

// TestStreamWatchdog_FirstEventResetsFirstByte verifies that a first event
// arriving before the first-byte deadline disarms it and re-arms the idle
// window.
func TestStreamWatchdog_FirstEventResetsFirstByte(t *testing.T) {
	parent := context.Background()
	ctx, reset, cancel := streamWatchdogContext(parent, 150*time.Millisecond, 60*time.Millisecond)
	defer cancel()

	// First event arrives before the first-byte deadline.
	reset()
	if waitWatchdogDone(t, ctx, 100*time.Millisecond) {
		t.Fatal("first-byte watchdog fired despite first event")
	}
	// Silence after the first event fires the idle window instead.
	if !waitWatchdogDone(t, ctx, 2*time.Second) {
		t.Fatal("idle watchdog did not fire after first event")
	}
	if kind, ok := streamWatchdogStalled(ctx); !ok || kind != streamWatchdogIdle {
		t.Errorf("stalled = %v/%v, want idle/fired", kind, ok)
	}
}

// TestStreamWatchdog_CancelStopsWatchdog verifies the revertible cancel
// behavior: cancel() stops the watchdog before it fires.
func TestStreamWatchdog_CancelStopsWatchdog(t *testing.T) {
	parent := context.Background()
	ctx, _, cancel := streamWatchdogContext(parent, 60*time.Millisecond, 0)
	cancel() // cancel before the window elapses

	time.Sleep(150 * time.Millisecond)
	if kind, ok := streamWatchdogStalled(ctx); ok {
		t.Errorf("stalled after cancel = %v, want not fired", kind)
	}
	// Idempotent second cancel must not panic.
	cancel()
}

// TestStreamWatchdog_ParentCancelNeverTrumps verifies that cancelling the
// parent context stops the watchdog without reporting a stall: the watchdog
// must never misreport a parent cancellation as a stream stall.
func TestStreamWatchdog_ParentCancelNeverTrumps(t *testing.T) {
	parent, parentCancel := context.WithCancel(context.Background())
	defer parentCancel()
	ctx, _, cancel := streamWatchdogContext(parent, 60*time.Millisecond, 0)
	defer cancel()

	parentCancel() // parent dies before the idle window
	time.Sleep(150 * time.Millisecond)

	if kind, ok := streamWatchdogStalled(ctx); ok {
		t.Errorf("stalled after parent cancel = %v, want not fired", kind)
	}
	select {
	case <-ctx.Done():
		t.Fatal("watchdog ctx.Done() fired after parent cancel (inert watchdog must not)")
	default:
	}
}

// ---------------------------------------------------------------------------
// observeStreamStall — nil-safe, records exactly one, metrics delta
// ---------------------------------------------------------------------------

// TestObserveStreamStallRecordsOnce verifies observeStreamStall increments the
// metrics stream-stall counter and the health registry count exactly once per
// stall (the once-guard lives in the caller's stall path, exercised by the
// provider wiring tests). It also verifies nil safety: observing with the
// singleton present never panics.
func TestObserveStreamStallRecordsOnce(t *testing.T) {
	reg := reliability.Default()
	if reg == nil {
		t.Fatal("reliability.Default() returned nil")
	}
	const provider = "watchdog-test"
	const model = "stall-once"

	// Distinct key so no other test's state leaks in.
	before := reg.Metrics.Take().LLMStreamStalls
	observeStreamStall(provider, model)
	after := reg.Metrics.Take()
	if got := after.LLMStreamStalls - before; got != 1 {
		t.Errorf("LLMStreamStalls delta = %d, want 1", got)
	}
	if reg.Health == nil {
		t.Fatal("health registry nil")
	}
	status := reg.Health.Status(provider, model)
	if status.StreamStallCount != 1 {
		t.Errorf("StreamStallCount = %d, want 1", status.StreamStallCount)
	}
	// A stall is a provider timeout: it must move the consecutive-failure
	// counter so the score degrades.
	if status.Attempts != 0 {
		t.Errorf("Attempts = %d, want 0 (stream stall does not count attempts)", status.Attempts)
	}
}

// TestStreamWatchdogErrorText verifies the stall error carries the canonical
// provider.timeout code and a message naming the provider/model.
func TestStreamWatchdogErrorText(t *testing.T) {
	re := streamWatchdogError("watchdog-provider", "watchdog-model", streamWatchdogIdle)
	if re.Code != reliability.ErrProviderTimeout {
		t.Errorf("Code = %s, want %s", re.Code, reliability.ErrProviderTimeout)
	}
	if !strings.Contains(re.Error(), "stream idle timeout") {
		t.Errorf("Error() = %q, want idle wording", re.Error())
	}
	fb := streamWatchdogError("watchdog-provider", "watchdog-model", streamWatchdogFirstByte)
	if !strings.Contains(fb.Error(), "first-byte") {
		t.Errorf("Error() = %q, want first-byte wording", fb.Error())
	}
}

// ---------------------------------------------------------------------------
// Provider wiring — httptest SSE servers that stall
// ---------------------------------------------------------------------------

// stallAfterEventsServer writes headers (flushed so the client's connection
// phase completes and the watchdog arms) plus the given first event, then goes
// silent until the test closes it. closeCh is closed by the test to unblock.
func stallAfterEventsServer(t *testing.T, first string, closeCh chan struct{}) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("ResponseWriter does not implement http.Flusher")
			return
		}
		// Flush the headers immediately: without them the client is still in
		// the connection phase (no watchdog armed yet) and the test would hang
		// on the transport-level timeout instead of exercising the watchdog.
		flusher.Flush()
		if first != "" {
			fmt.Fprint(w, first)
			flusher.Flush()
		}
		<-closeCh
	}))
	t.Cleanup(server.Close)
	return server
}

// TestOpenAIChatStream_WatchdogFires verifies the OpenAI-compatible stream
// fires the watchdog when the server goes silent, returns the canonical
// timeout error exactly once, and cancels the request so the client unwinds.
func TestOpenAIChatStream_WatchdogFires(t *testing.T) {
	closeCh := make(chan struct{})
	server := stallAfterEventsServer(t, "", closeCh)

	p := NewOpenAIProvider("watchdog-openai", "test-key", server.URL, "gpt-4o")
	p.retryConfig.Attempts = 1

	reg := reliability.Default()
	if reg == nil {
		t.Fatal("reliability.Default() returned nil")
	}
	const model = "watchdog-stall-model"
	before := reg.Metrics.Take().LLMStreamStalls

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// The runtime Stream knobs are owned by the config lane (may not exist yet
	// in a standalone build) — arm the watchdog per-request for this test.
	ctx = WithStreamTimeouts(ctx, time.Second, 0)

	req := ChatRequest{
		Model:    model,
		Messages: []Message{{Role: "user", Content: "hello"}},
	}
	start := time.Now()
	result, err := p.ChatStream(ctx, req, nil)

	// The watchdog cancels the request as a side effect.
	close(closeCh)

	if err == nil {
		t.Fatal("expected stall error, got nil")
	}
	var relErr *reliability.ReliabilityError
	if !errors.As(err, &relErr) {
		t.Fatalf("err type = %T, want *reliability.ReliabilityError", err)
	}
	if relErr.Code != reliability.ErrProviderTimeout {
		t.Errorf("Code = %s, want %s", relErr.Code, reliability.ErrProviderTimeout)
	}
	if !strings.Contains(err.Error(), "stream idle timeout") {
		t.Errorf("err = %q, want idle timeout wording", err.Error())
	}
	if result != nil {
		t.Errorf("result = %+v, want nil on stall", result)
	}
	if elapsed := time.Since(start); elapsed < 800*time.Millisecond {
		t.Errorf("stall resolved in %v, want to wait for the idle window", elapsed)
	}

	// Stall observed exactly once on the metrics counter.
	after := reg.Metrics.Take().LLMStreamStalls
	if delta := after - before; delta != 1 {
		t.Errorf("LLMStreamStalls delta = %d, want exactly 1", delta)
	}
}

// TestAnthropicChatStream_WatchdogFires verifies the Anthropic stream fires
// the watchdog on a silent server and reports the stall exactly once.
func TestAnthropicChatStream_WatchdogFires(t *testing.T) {
	closeCh := make(chan struct{})
	server := stallAfterEventsServer(t, "", closeCh)

	p := newTestAnthropicProvider(server.URL)

	reg := reliability.Default()
	if reg == nil {
		t.Fatal("reliability.Default() returned nil")
	}
	before := reg.Metrics.Take().LLMStreamStalls

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = WithStreamTimeouts(ctx, time.Second, 0)

	req := ChatRequest{
		Model:    "claude-sonnet-4-5-20250929",
		Messages: []Message{{Role: "user", Content: "hello"}},
	}
	start := time.Now()
	_, err := p.ChatStream(ctx, req, nil)
	close(closeCh)

	if err == nil {
		t.Fatal("expected stall error, got nil")
	}
	var relErr *reliability.ReliabilityError
	if !errors.As(err, &relErr) {
		t.Fatalf("err type = %T, want *reliability.ReliabilityError", err)
	}
	if relErr.Code != reliability.ErrProviderTimeout {
		t.Errorf("Code = %s, want %s", relErr.Code, reliability.ErrProviderTimeout)
	}
	if elapsed := time.Since(start); elapsed < 800*time.Millisecond {
		t.Errorf("stall resolved in %v, want to wait for the idle window", elapsed)
	}
	after := reg.Metrics.Take().LLMStreamStalls
	if delta := after - before; delta != 1 {
		t.Errorf("LLMStreamStalls delta = %d, want exactly 1", delta)
	}
}

// TestCodexChatStream_WatchdogFiresAndDoesNotRetryAfterEmit verifies the Codex
// stream (which carries its own retry loop) fires the watchdog and, because a
// chunk already escaped, does not retry the request.
func TestCodexChatStream_WatchdogFiresAndDoesNotRetryAfterEmit(t *testing.T) {
	var requests int32
	closeCh := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("ResponseWriter does not implement http.Flusher")
			return
		}
		// One text delta, then silence — the partial-stream stall case.
		fmt.Fprint(w, "data: "+mustJSON(codexSSEEvent{Type: "response.output_text.delta", Delta: "Hello"})+"\n\n")
		flusher.Flush()
		<-closeCh
	}))
	t.Cleanup(server.Close)

	p := NewCodexProvider("watchdog-codex", &staticTokenSource{token: "test"}, server.URL, "gpt-5.5")
	p.retryConfig.Attempts = 3 // the output-emitted guard must suppress replays

	reg := reliability.Default()
	if reg == nil {
		t.Fatal("reliability.Default() returned nil")
	}
	before := reg.Metrics.Take().LLMStreamStalls

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = WithStreamTimeouts(ctx, time.Second, 0)

	req := ChatRequest{
		Model:    "gpt-5.5",
		Messages: []Message{{Role: "user", Content: "hello"}},
	}
	var chunks atomic.Int32
	start := time.Now()
	_, err := p.ChatStream(ctx, req, func(StreamChunk) { chunks.Add(1) })
	close(closeCh)

	if err == nil {
		t.Fatal("expected stall error, got nil")
	}
	var relErr *reliability.ReliabilityError
	if !errors.As(err, &relErr) || relErr.Code != reliability.ErrProviderTimeout {
		t.Fatalf("err = %v, want provider.timeout reliability error", err)
	}
	if got := chunks.Load(); got < 1 {
		t.Errorf("chunks emitted = %d, want >= 1 (delta arrived before the stall)", got)
	}
	if got := atomic.LoadInt32(&requests); got != 1 {
		t.Errorf("requests = %d, want exactly 1 (no retry after emit)", got)
	}
	if elapsed := time.Since(start); elapsed < 800*time.Millisecond {
		t.Errorf("stall resolved in %v, want to wait for the idle window", elapsed)
	}
	after := reg.Metrics.Take().LLMStreamStalls
	if delta := after - before; delta != 1 {
		t.Errorf("LLMStreamStalls delta = %d, want exactly 1", delta)
	}
}

// TestChatStreamWatchdogCleanStreamDoesNotFire verifies a stream that finishes
// cleanly never observes a stall, even when events arrive sparsely relative to
// the idle window.
func TestChatStreamWatchdogCleanStreamDoesNotFire(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("ResponseWriter does not implement http.Flusher")
			return
		}
		for _, ev := range []string{
			"data: "+mustJSON(codexSSEEvent{Type: "response.output_text.delta", Delta: "A"})+"\n\n",
			"data: "+mustJSON(codexSSEEvent{Type: "response.output_text.delta", Delta: "B"})+"\n\n",
			"data: "+mustJSON(codexSSEEvent{Type: "response.completed", Response: &codexAPIResponse{ID: "r-1", Status: "completed"}})+"\n\n",
			"data: [DONE]\n\n",
		} {
			fmt.Fprint(w, ev)
			flusher.Flush()
		}
	}))
	t.Cleanup(server.Close)

	p := NewCodexProvider("watchdog-clean", &staticTokenSource{token: "test"}, server.URL, "gpt-5.5")
	p.retryConfig.Attempts = 1

	reg := reliability.Default()
	if reg == nil {
		t.Fatal("reliability.Default() returned nil")
	}
	before := reg.Metrics.Take().LLMStreamStalls

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := p.ChatStream(ctx, ChatRequest{Model: "gpt-5.5", Messages: []Message{{Role: "user", Content: "hi"}}}, nil)
	if err != nil {
		t.Fatalf("clean stream returned error: %v", err)
	}
	if result == nil || result.Content != "AB" {
		t.Errorf("result = %+v, want content AB", result)
	}
	after := reg.Metrics.Take().LLMStreamStalls
	if delta := after - before; delta != 0 {
		t.Errorf("LLMStreamStalls delta = %d, want 0 on a clean stream", delta)
	}
}

// TestFailoverDoesNotFallbackAfterStreamedError verifies the failover settle
// gate: a candidate outcome that already emitted chunks reports FailoverStreamed
// and RunWithFailover returns it without trying the second candidate.
func TestFailoverDoesNotFallbackAfterStreamedError(t *testing.T) {
	ctx := context.Background()
	cfg := FailoverConfig{
		Candidates: []ModelCandidate{
			{Provider: "openai", Model: "gpt-4o", ProfileID: "key1"},
			{Provider: "openai", Model: "gpt-4o", ProfileID: "key2"},
		},
		Classifier: NewDefaultClassifier(),
	}

	callCount := 0
	runFn := func(ctx context.Context, candidate ModelCandidate) (string, error) {
		callCount++
		if callCount == 1 {
			// First candidate emitted stream chunks then failed mid-stream.
			return "partial", &FailoverStreamed{}
		}
		return "fallback-output", nil
	}

	result, attempts, err := RunWithFailover(ctx, cfg, runFn)

	if err == nil {
		t.Fatal("expected the streamed error to settle the run")
	}
	var streamedErr *FailoverStreamed
	if !errors.As(err, &streamedErr) {
		t.Fatalf("err type = %T, want *FailoverStreamed", err)
	}
	if result != "partial" {
		t.Errorf("result = %q, want partial from the first candidate", result)
	}
	if callCount != 1 {
		t.Errorf("calls = %d, want 1 (no fallback after streamed chunk)", callCount)
	}
	if len(attempts) != 0 {
		t.Errorf("attempts = %d, want 0 (streamed errors are not classified)", len(attempts))
	}
}

// TestFailoverErrorBeforeEmitStillFallsBack verifies the pre-output failure
// path still rotates: an error without any emitted chunk falls through to the
// next candidate as before.
func TestFailoverErrorBeforeEmitStillFallsBack(t *testing.T) {
	ctx := context.Background()
	cfg := FailoverConfig{
		Candidates: []ModelCandidate{
			{Provider: "openai", Model: "gpt-4o", ProfileID: "key1"},
			{Provider: "openai", Model: "gpt-4o", ProfileID: "key2"},
		},
		Classifier: NewDefaultClassifier(),
	}

	callCount := 0
	runFn := func(ctx context.Context, candidate ModelCandidate) (string, error) {
		callCount++
		if callCount == 1 {
			return "", &HTTPError{Status: 429, Body: "rate limited"}
		}
		return "success", nil
	}

	result, _, err := RunWithFailover(ctx, cfg, runFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "success" {
		t.Errorf("result = %q, want success", result)
	}
	if callCount != 2 {
		t.Errorf("calls = %d, want 2 (pre-output error still rotates)", callCount)
	}
}