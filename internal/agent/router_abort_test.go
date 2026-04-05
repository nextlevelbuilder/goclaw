package agent

import (
	"context"
	"testing"
)

// TestAbortRun_CancelsContext verifies that AbortRun calls the cancel func
// and removes the run from tracking.
func TestAbortRun_CancelsContext(t *testing.T) {
	r := NewRouter()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runID := "run-1"
	sessionKey := "session-1"
	r.RegisterRun(runID, sessionKey, "agent-1", cancel)

	if !r.IsSessionBusy(sessionKey) {
		t.Fatal("session should be busy after RegisterRun")
	}

	ok := r.AbortRun(runID, sessionKey)
	if !ok {
		t.Fatal("AbortRun should return true for active run")
	}
	if ctx.Err() == nil {
		t.Fatal("context should be cancelled after AbortRun")
	}
	if r.IsSessionBusy(sessionKey) {
		t.Fatal("session should not be busy after AbortRun")
	}
}

// TestAbortRun_WrongSessionKey verifies authorization check.
func TestAbortRun_WrongSessionKey(t *testing.T) {
	r := NewRouter()
	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	r.RegisterRun("run-1", "session-1", "agent-1", cancel)

	ok := r.AbortRun("run-1", "wrong-session")
	if ok {
		t.Fatal("AbortRun should return false for mismatched sessionKey")
	}
	if !r.IsSessionBusy("session-1") {
		t.Fatal("session should still be busy after failed abort")
	}
}

// TestAbortRun_NotFound returns false for unknown run IDs.
func TestAbortRun_NotFound(t *testing.T) {
	r := NewRouter()
	if r.AbortRun("nonexistent", "") {
		t.Fatal("AbortRun should return false for unknown runID")
	}
}

// TestAbortRunsForSession_CancelsAllRuns verifies that all runs in a session
// are cancelled when AbortRunsForSession is called.
func TestAbortRunsForSession_CancelsAllRuns(t *testing.T) {
	r := NewRouter()
	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()

	sessionKey := "session-multi"
	r.RegisterRun("run-1", sessionKey, "agent-1", cancel1)
	r.RegisterRun("run-2", sessionKey, "agent-1", cancel2)

	aborted := r.AbortRunsForSession(sessionKey)
	if len(aborted) != 2 {
		t.Fatalf("expected 2 aborted runs, got %d", len(aborted))
	}
	if ctx1.Err() == nil || ctx2.Err() == nil {
		t.Fatal("both contexts should be cancelled")
	}
	if r.IsSessionBusy(sessionKey) {
		t.Fatal("session should not be busy after AbortRunsForSession")
	}
}

// TestAbortRunsForSession_EmptySession returns empty slice.
func TestAbortRunsForSession_EmptySession(t *testing.T) {
	r := NewRouter()
	aborted := r.AbortRunsForSession("no-such-session")
	if len(aborted) != 0 {
		t.Fatalf("expected 0 aborted runs, got %d", len(aborted))
	}
}

// TestAbortRun_AfterUnregister verifies the race scenario:
// if run is unregistered before abort, abort returns false (idempotent).
func TestAbortRun_AfterUnregister(t *testing.T) {
	r := NewRouter()
	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	r.RegisterRun("run-1", "session-1", "agent-1", cancel)
	r.UnregisterRun("run-1")

	ok := r.AbortRun("run-1", "session-1")
	if ok {
		t.Fatal("AbortRun should return false after UnregisterRun")
	}
}

// TestUnregisterRun_ClearsSessionIndex verifies that UnregisterRun cleans up
// both activeRuns and sessionRuns.
func TestUnregisterRun_ClearsSessionIndex(t *testing.T) {
	r := NewRouter()
	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	r.RegisterRun("run-1", "session-1", "agent-1", cancel)
	if !r.IsSessionBusy("session-1") {
		t.Fatal("session should be busy")
	}

	r.UnregisterRun("run-1")
	if r.IsSessionBusy("session-1") {
		t.Fatal("session should not be busy after unregister")
	}
}

// TestSessionKeyForRun returns correct session key or empty.
func TestSessionKeyForRun(t *testing.T) {
	r := NewRouter()
	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	r.RegisterRun("run-1", "session-1", "agent-1", cancel)

	if got := r.SessionKeyForRun("run-1"); got != "session-1" {
		t.Fatalf("expected session-1, got %q", got)
	}
	if got := r.SessionKeyForRun("nonexistent"); got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}

// TestActivityClearedOnAbort verifies that activity is cleared when the gateway
// subscriber processes a terminal event. We test the Router methods directly.
func TestActivityClearedOnAbort(t *testing.T) {
	r := NewRouter()
	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	sessionKey := "session-act"
	r.RegisterRun("run-act", sessionKey, "agent-1", cancel)
	r.UpdateActivity(sessionKey, "run-act", "thinking", "", 0)

	if r.GetActivity(sessionKey) == nil {
		t.Fatal("activity should be set")
	}

	// Simulate what gateway.go does on terminal events
	r.ClearActivity(sessionKey)
	if r.GetActivity(sessionKey) != nil {
		t.Fatal("activity should be cleared after ClearActivity")
	}
}
