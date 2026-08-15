package agent

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

// mockTraceCollector counts FinishTrace calls for test assertions.
type mockTraceCollector struct {
	mu    sync.Mutex
	calls []finishTraceCall
}

type finishTraceCall struct {
	TraceID       uuid.UUID
	Status        string
	ErrMsg        string
	OutputPreview string
}

func (m *mockTraceCollector) FinishTrace(_ context.Context, traceID uuid.UUID, status, errMsg, outputPreview string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, finishTraceCall{TraceID: traceID, Status: status, ErrMsg: errMsg, OutputPreview: outputPreview})
}

func (m *mockTraceCollector) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

// ---- helpers ----

func newRouterWithCollector(tc TraceCollector) *Router {
	r := NewRouter()
	r.SetTraceCollector(tc)
	return r
}

// registerRun is a test helper that creates a cancel func and registers a run.
// Returns the cancel func and Done channel from the stored ActiveRun.
func registerRun(r *Router, runID, sessionKey string) (context.CancelFunc, chan struct{}) {
	_, cancel := context.WithCancel(context.Background())
	r.RegisterRun(context.Background(), runID, sessionKey, "agent-1", cancel)
	val, _ := r.activeRuns.Load(runID)
	return cancel, val.(*ActiveRun).Done
}

// ---- tests ----

// TestAbortRun_Stopped verifies graceful stop: goroutine calls UnregisterRun
// before the 3s grace period expires → result.Stopped == true.
func TestAbortRun_Stopped(t *testing.T) {
	r := NewRouter()
	runID := "run-stop"
	sessionKey := "session-stop"

	_, _ = registerRun(r, runID, sessionKey)

	// Simulate goroutine exiting after 50ms.
	go func() {
		time.Sleep(50 * time.Millisecond)
		r.UnregisterRun(runID)
	}()

	res := r.AbortRun(runID, sessionKey)
	if !res.Stopped {
		t.Fatalf("expected Stopped=true, got %+v", res)
	}
	if res.Forced || res.AlreadyAborting || res.NotFound || res.Unauthorized {
		t.Fatalf("unexpected flags set: %+v", res)
	}
}

// TestAbortRun_Forced verifies forced abort: goroutine never exits within 3s →
// result.Forced == true and TraceCollector.FinishTrace is called once.
func TestAbortRun_Forced(t *testing.T) {
	tc := &mockTraceCollector{}
	r := newRouterWithCollector(tc)

	runID := "run-forced"
	sessionKey := "session-forced"
	traceID := uuid.New()

	_, _ = registerRun(r, runID, sessionKey)
	r.SetRunTraceID(runID, traceID)

	// Goroutine deliberately never calls UnregisterRun.

	start := time.Now()
	res := r.AbortRun(runID, sessionKey)
	elapsed := time.Since(start)

	if !res.Forced {
		t.Fatalf("expected Forced=true, got %+v", res)
	}
	if res.Stopped || res.AlreadyAborting || res.NotFound || res.Unauthorized {
		t.Fatalf("unexpected flags set: %+v", res)
	}
	if elapsed < abortGraceTimeout {
		t.Fatalf("expected at least %s elapsed, got %s", abortGraceTimeout, elapsed)
	}
	if tc.callCount() != 1 {
		t.Fatalf("expected 1 FinishTrace call, got %d", tc.callCount())
	}
	if tc.calls[0].TraceID != traceID {
		t.Fatalf("FinishTrace called with wrong traceID: %s", tc.calls[0].TraceID)
	}
	if tc.calls[0].Status != "cancelled" {
		t.Fatalf("FinishTrace called with wrong status: %s", tc.calls[0].Status)
	}
}

// TestAbortRun_AlreadyAborting verifies that 100 concurrent abort calls on the
// same run produce exactly one Stopped or Forced result and the rest return
// AlreadyAborting. No panics allowed.
func TestAbortRun_AlreadyAborting(t *testing.T) {
	r := NewRouter()
	runID := "run-concurrent"
	sessionKey := "session-concurrent"

	_, _ = registerRun(r, runID, sessionKey)

	// Goroutine exits quickly so most callers see AlreadyAborting, not Forced.
	go func() {
		time.Sleep(20 * time.Millisecond)
		r.UnregisterRun(runID)
	}()

	const n = 100
	results := make([]AbortResult, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func() {
			defer wg.Done()
			results[i] = r.AbortRun(runID, sessionKey)
		}()
	}
	wg.Wait()

	var stopped, forced, alreadyAborting, other int
	for _, res := range results {
		switch {
		case res.Stopped:
			stopped++
		case res.Forced:
			forced++
		case res.AlreadyAborting:
			alreadyAborting++
		default:
			other++
		}
	}

	definitiveCount := stopped + forced
	if definitiveCount != 1 {
		t.Fatalf("expected exactly 1 Stopped/Forced, got stopped=%d forced=%d", stopped, forced)
	}
	if alreadyAborting+definitiveCount != n {
		t.Fatalf("counts don't sum to %d: stopped=%d forced=%d alreadyAborting=%d other=%d",
			n, stopped, forced, alreadyAborting, other)
	}
}

// TestAbortRun_Unauthorized verifies sessionKey mismatch returns Unauthorized.
func TestAbortRun_Unauthorized(t *testing.T) {
	r := NewRouter()
	_, _ = registerRun(r, "run-1", "session-1")

	res := r.AbortRun("run-1", "wrong-session")
	if !res.Unauthorized {
		t.Fatalf("expected Unauthorized=true, got %+v", res)
	}
	if res.Stopped || res.Forced || res.AlreadyAborting || res.NotFound {
		t.Fatalf("unexpected flags set: %+v", res)
	}
	// Session must still be busy (run not cancelled).
	if !r.IsSessionBusy("session-1") {
		t.Fatal("session should still be busy after Unauthorized abort")
	}
}

// TestAbortRun_NotFound verifies abort on never-registered runID returns NotFound.
func TestAbortRun_NotFound(t *testing.T) {
	r := NewRouter()
	res := r.AbortRun("nonexistent", "")
	if !res.NotFound {
		t.Fatalf("expected NotFound=true, got %+v", res)
	}
}

// TestAbortRun_AfterUnregister verifies that aborting after run completes returns NotFound
// (state==2 branch of CAS failure).
func TestAbortRun_AfterUnregister(t *testing.T) {
	r := NewRouter()
	_, _ = registerRun(r, "run-1", "session-1")
	r.UnregisterRun("run-1")

	res := r.AbortRun("run-1", "session-1")
	if !res.NotFound {
		t.Fatalf("expected NotFound=true after unregister, got %+v", res)
	}
}

// TestAbortRun_Race_UnregisterConcurrent verifies no panic and no goroutine leak
// when UnregisterRun and AbortRun interleave across 100 iterations.
func TestAbortRun_Race_UnregisterConcurrent(t *testing.T) {
	for range 100 {
		r := NewRouter()
		runID := "run-race"
		sessionKey := "session-race"

		_, _ = registerRun(r, runID, sessionKey)

		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			r.UnregisterRun(runID)
		}()
		go func() {
			defer wg.Done()
			r.AbortRun(runID, sessionKey)
		}()

		wg.Wait()
		// No assertion on result — we only care that no panic and no goroutine leak.
	}
}

// TestAbortRunsForSession_ReturnsResults verifies AbortRunsForSession returns
// rich AbortResult slices for two concurrent runs.
// AbortRunsForSession is sequential; goroutines unregister independently so
// each AbortRun call sees its Done close quickly (Stopped result).
func TestAbortRunsForSession_ReturnsResults(t *testing.T) {
	r := NewRouter()
	sessionKey := "session-multi"

	_, doneA := registerRun(r, "run-a", sessionKey)
	_, doneB := registerRun(r, "run-b", sessionKey)

	_ = doneA
	_ = doneB

	// Goroutines close Done channels 50ms after they're signalled (Cancel called).
	// We use a separate goroutine per run that watches for Cancel via context.
	ctxA, cancelA := context.WithCancel(context.Background())
	ctxB, cancelB := context.WithCancel(context.Background())
	defer cancelA()
	defer cancelB()

	// Replace stored cancel funcs with our instrumented ones.
	if valA, ok := r.activeRuns.Load("run-a"); ok {
		valA.(*ActiveRun).Cancel = cancelA
	}
	if valB, ok := r.activeRuns.Load("run-b"); ok {
		valB.(*ActiveRun).Cancel = cancelB
	}

	go func() {
		<-ctxA.Done()
		time.Sleep(10 * time.Millisecond)
		r.UnregisterRun("run-a")
	}()
	go func() {
		<-ctxB.Done()
		time.Sleep(10 * time.Millisecond)
		r.UnregisterRun("run-b")
	}()

	results := r.AbortRunsForSession(sessionKey)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for _, res := range results {
		if res.Unauthorized {
			t.Fatalf("unexpected Unauthorized result: %+v", res)
		}
	}
	// Both should be stopped (not forced), since goroutines exit promptly.
	for _, res := range results {
		if res.Forced {
			t.Fatalf("expected Stopped not Forced: %+v", res)
		}
	}
}

// TestSetRunTraceID verifies SetRunTraceID stores the traceID in the active run.
func TestSetRunTraceID(t *testing.T) {
	r := NewRouter()
	_, _ = registerRun(r, "run-trace", "session-trace")

	tid := uuid.New()
	r.SetRunTraceID("run-trace", tid)

	val, ok := r.activeRuns.Load("run-trace")
	if !ok {
		t.Fatal("run should still be in activeRuns")
	}
	if val.(*ActiveRun).TraceID != tid {
		t.Fatalf("expected traceID %s, got %s", tid, val.(*ActiveRun).TraceID)
	}
}

// TestSafeClose verifies safeClose does not panic when called twice.
func TestSafeClose(t *testing.T) {
	ch := make(chan struct{})
	safeClose(ch)
	// Second call must not panic.
	safeClose(ch)
}

// TestAbortRun_StateAfterStop verifies state transitions correctly to 2 (done)
// after a successful stop (goroutine calls UnregisterRun).
func TestAbortRun_StateAfterStop(t *testing.T) {
	r := NewRouter()
	runID := "run-state"
	sessionKey := "session-state"

	_, _ = registerRun(r, runID, sessionKey)

	unregistered := make(chan struct{})
	go func() {
		time.Sleep(20 * time.Millisecond)
		r.UnregisterRun(runID)
		close(unregistered)
	}()

	res := r.AbortRun(runID, sessionKey)
	if !res.Stopped {
		t.Fatalf("expected Stopped=true, got %+v", res)
	}

	<-unregistered

	// After stop the run entry should be gone from the map.
	if _, ok := r.activeRuns.Load(runID); ok {
		t.Fatal("activeRuns should not contain run after UnregisterRun")
	}
}

// TestAbortRun_NilTraceCollector_Forced verifies that force-abort path does not
// panic when no TraceCollector is wired (traceCollector == nil).
func TestAbortRun_NilTraceCollector_Forced(t *testing.T) {
	r := NewRouter() // no SetTraceCollector
	runID := "run-nil-tc"
	sessionKey := "session-nil-tc"

	_, _ = registerRun(r, runID, sessionKey)

	// Goroutine never exits → Forced path will call forceMarkTraceAborted.
	res := r.AbortRun(runID, sessionKey)
	if !res.Forced {
		t.Fatalf("expected Forced=true, got %+v", res)
	}
	// No panic = pass.
}

// TestAbortRunsForSession_EmptySession returns empty slice for unknown session.
func TestAbortRunsForSession_EmptySession(t *testing.T) {
	r := NewRouter()
	results := r.AbortRunsForSession("no-such-session")
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

// TestAbortRun_DoesNotLeakGoroutine verifies that after the forced-abort timeout,
// when the goroutine eventually calls UnregisterRun, the Done channel is safely
// closed without a double-close panic.
func TestAbortRun_DoesNotLeakGoroutine(t *testing.T) {
	r := NewRouter()
	runID := "run-late-unregister"
	sessionKey := "session-late"

	_, done := registerRun(r, runID, sessionKey)

	// Goroutine exits well after the 3s timeout.
	var lateUnregisterDone atomic.Bool
	go func() {
		// Force path fires after abortGraceTimeout; goroutine "finishes" 100ms later.
		time.Sleep(abortGraceTimeout + 100*time.Millisecond)
		r.UnregisterRun(runID) // must not panic even though AbortRun already returned
		lateUnregisterDone.Store(true)
	}()

	res := r.AbortRun(runID, sessionKey)
	if !res.Forced {
		t.Fatalf("expected Forced=true, got %+v", res)
	}

	// Wait for the goroutine to call UnregisterRun.
	deadline := time.Now().Add(2 * time.Second)
	for !lateUnregisterDone.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !lateUnregisterDone.Load() {
		t.Fatal("goroutine did not complete UnregisterRun in time")
	}

	// Done channel should be closed (safeClose handled double-close internally).
	select {
	case <-done:
		// expected
	default:
		t.Fatal("Done channel should be closed after UnregisterRun")
	}
}
