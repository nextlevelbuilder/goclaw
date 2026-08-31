package agent

import "testing"

// Phase 7 Decision 3 — loop-level zombie fence (RunRequest.ownsSession).
//
// ownsSession is the single predicate the loop consults before its three
// user-visible commit sites (history append in makeFlushMessages, session save
// in makeUpdateMetadata, and the final run.completed emit in loop_run.go). Its
// contract:
//   - nil request or nil IsCurrentOwner callback  → true  (UNFENCED: delegations,
//     announce runs, and tests keep the exact pre-Decision-3 behavior).
//   - live callback returning true                → true  (still the owner: commit).
//   - live callback returning false               → false (zombie: suppress).

func TestOwnsSession_NilRequestIsUnfenced(t *testing.T) {
	var req *RunRequest
	if !req.ownsSession() {
		t.Fatal("nil request must be treated as unfenced (true) to preserve legacy behavior")
	}
}

func TestOwnsSession_NilCallbackIsUnfenced(t *testing.T) {
	req := &RunRequest{} // IsCurrentOwner left nil (delegation / announce / test path)
	if !req.ownsSession() {
		t.Fatal("nil IsCurrentOwner must be treated as unfenced (true)")
	}
}

func TestOwnsSession_LiveOwnerCommits(t *testing.T) {
	req := &RunRequest{IsCurrentOwner: func() bool { return true }}
	if !req.ownsSession() {
		t.Fatal("a run that still owns its session must be allowed to commit")
	}
}

func TestOwnsSession_LostOwnershipSuppressed(t *testing.T) {
	req := &RunRequest{IsCurrentOwner: func() bool { return false }}
	if req.ownsSession() {
		t.Fatal("a run that lost session ownership (zombie) must be suppressed")
	}
}

// TestOwnsSession_ReflectsLiveOwnershipTransition proves the fence is evaluated
// live at each commit site — flipping the underlying ownership between calls
// changes the verdict, so a run that owned the session when it started but was
// superseded mid-run is suppressed at its later commit.
func TestOwnsSession_ReflectsLiveOwnershipTransition(t *testing.T) {
	owns := true
	req := &RunRequest{IsCurrentOwner: func() bool { return owns }}

	if !req.ownsSession() {
		t.Fatal("expected ownership before transition")
	}
	owns = false // a replacement run took over the session
	if req.ownsSession() {
		t.Fatal("expected suppression after ownership was lost mid-run")
	}
}
