package agent

import (
	"context"
	"testing"
)

// Phase 7 Decision 3 — router ownership generation fence.
//
// These tests verify the four mandated invariants:
//   (a) Run A register → force abort → run B register → A resume: A observes
//       non-ownership (no append/save/emit) while B behaves normally.
//   (b) A superseded/aborted run's ownership is lost while the replacement's is
//       intact — the DB attempt-token CAS is a SEPARATE authority and is not
//       exercised here (see store tests); this test asserts the router fence.
//   (c) A late UnregisterRun for the aborted run A does NOT remove ownership held
//       by replacement run B (CompareAndDelete keyed on exact {runID,generation}).
//   (d) A normal, non-aborted run owns its session for exactly its lifetime and
//       relinquishes ownership on UnregisterRun.

// TestOwnership_AbortedRunLosesOwnershipReplacementKeepsIt covers (a) and (b):
// after run A is registered and aborted, a replacement run B on the same session
// takes ownership with a strictly higher generation. A's original generation is
// no longer the current owner (its zombie writes would be suppressed), while B is.
func TestOwnership_AbortedRunLosesOwnershipReplacementKeepsIt(t *testing.T) {
	r := NewRouter()
	sessionKey := "session-own"

	// Run A registers and owns the session.
	_, cancelA := context.WithCancel(context.Background())
	_, genA := r.RegisterRun(context.Background(), "run-a", sessionKey, "agent-1", cancelA)
	if !r.IsCurrentOwner(sessionKey, "run-a", genA) {
		t.Fatal("run A must own the session immediately after RegisterRun")
	}

	// Force-abort A: this closes Done and (via UnregisterRun in the forced path)
	// invalidates ownership. We drive it deterministically by unregistering A,
	// which is exactly what the forced-abort timeout path does.
	r.UnregisterRun("run-a")
	if r.IsCurrentOwner(sessionKey, "run-a", genA) {
		t.Fatal("run A must lose ownership after UnregisterRun (would be a zombie)")
	}

	// Replacement run B registers on the SAME session and must take ownership
	// with a strictly higher generation.
	_, cancelB := context.WithCancel(context.Background())
	_, genB := r.RegisterRun(context.Background(), "run-b", sessionKey, "agent-1", cancelB)
	if genB <= genA {
		t.Fatalf("replacement run B must get a strictly higher generation: genA=%d genB=%d", genA, genB)
	}
	if !r.IsCurrentOwner(sessionKey, "run-b", genB) {
		t.Fatal("replacement run B must own the session")
	}
	// A's old generation is still a non-owner — its zombie commits stay suppressed.
	if r.IsCurrentOwner(sessionKey, "run-a", genA) {
		t.Fatal("aborted run A must remain a non-owner after B takes over")
	}
}

// TestOwnership_ReplacementBeforeAbortedUnregister covers the harder race in (a)
// and (c): run B registers and takes ownership BEFORE the aborted run A's late
// UnregisterRun fires. The late UnregisterRun for A must be a no-op for the
// session index (CompareAndDelete keyed on A's {runID,generation} does not match
// B's owner entry), so B keeps ownership.
func TestOwnership_ReplacementBeforeAbortedUnregister(t *testing.T) {
	r := NewRouter()
	sessionKey := "session-race-own"

	// Run A registers and owns the session.
	_, cancelA := context.WithCancel(context.Background())
	_, genA := r.RegisterRun(context.Background(), "run-a", sessionKey, "agent-1", cancelA)

	// Run A is force-aborted: state flips, but its goroutine is stuck and has NOT
	// yet called UnregisterRun. Meanwhile a replacement run B registers.
	// RegisterRun assigns B a higher generation and takes ownership (A's slot is
	// held by an older generation, so the CAS overwrites it).
	_, cancelB := context.WithCancel(context.Background())
	_, genB := r.RegisterRun(context.Background(), "run-b", sessionKey, "agent-1", cancelB)
	if !r.IsCurrentOwner(sessionKey, "run-b", genB) {
		t.Fatal("replacement run B must own the session after registering over an aborted A")
	}
	if r.IsCurrentOwner(sessionKey, "run-a", genA) {
		t.Fatal("aborted run A must no longer own the session once B registers")
	}

	// NOW run A's stuck goroutine finally calls UnregisterRun. Because the session
	// index maps to sessionOwner{run-b, genB} — not {run-a, genA} — the keyed
	// CompareAndDelete must NOT remove B's ownership.
	r.UnregisterRun("run-a")
	if !r.IsCurrentOwner(sessionKey, "run-b", genB) {
		t.Fatal("late UnregisterRun for aborted A must NOT evict replacement B's ownership")
	}

	// And B's own eventual UnregisterRun releases the session cleanly.
	r.UnregisterRun("run-b")
	if r.IsCurrentOwner(sessionKey, "run-b", genB) {
		t.Fatal("run B must release ownership on its own UnregisterRun")
	}
	if r.IsSessionBusy(sessionKey) {
		t.Fatal("session must be free after both runs unregister")
	}
}

// TestOwnership_NormalRunOwnsExactlyItsLifetime covers (d): a normal run owns the
// session from RegisterRun until UnregisterRun, and a stale generation never
// reports ownership.
func TestOwnership_NormalRunOwnsExactlyItsLifetime(t *testing.T) {
	r := NewRouter()
	sessionKey := "session-normal"

	_, cancel := context.WithCancel(context.Background())
	_, gen := r.RegisterRun(context.Background(), "run-normal", sessionKey, "agent-1", cancel)

	if !r.IsCurrentOwner(sessionKey, "run-normal", gen) {
		t.Fatal("normal run must own its session during its lifetime")
	}
	// A wrong generation for the same runID must not report ownership.
	if r.IsCurrentOwner(sessionKey, "run-normal", gen+1) {
		t.Fatal("a mismatched generation must never report ownership")
	}
	// A wrong runID must not report ownership.
	if r.IsCurrentOwner(sessionKey, "other-run", gen) {
		t.Fatal("a mismatched runID must never report ownership")
	}

	r.UnregisterRun("run-normal")
	if r.IsCurrentOwner(sessionKey, "run-normal", gen) {
		t.Fatal("normal run must relinquish ownership on UnregisterRun")
	}
}

// TestOwnership_UnknownSessionIsNotOwned guards the fence's fail-closed behavior:
// an unknown session (never registered, or fully unregistered) reports no owner,
// so IsCurrentOwner is false for any runID/generation. This is what makes the
// loop's nil-fence-vs-live-fence distinction safe: a live fence on a released
// session returns false (suppress), never a false positive.
func TestOwnership_UnknownSessionIsNotOwned(t *testing.T) {
	r := NewRouter()
	if r.IsCurrentOwner("no-such-session", "run-x", 1) {
		t.Fatal("unknown session must never report ownership")
	}
}
