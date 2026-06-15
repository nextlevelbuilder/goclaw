package methods

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// TestSessionsReset_ClearsCLISession is a regression guard ensuring the
// sessions.reset RPC also clears the Claude CLI-backed session, matching the
// /reset chat command. Previously the RPC only reset the native session store,
// leaving claude-cli history (.jsonl + CLAUDE.md) in place.
func TestSessionsReset_ClearsCLISession(t *testing.T) {
	sess := newStubSessionStore()
	// Empty userID matches nullClient()'s empty UserID, so the ownership
	// check in handleReset passes and we reach the reset path.
	sess.addSession("reset-key", "")
	m := buildSessionMethods(t, sess)
	client := nullClient()

	var called bool
	var gotKey string
	orig := cliSessionReset
	cliSessionReset = func(_, sessionKey string) {
		called = true
		gotKey = sessionKey
	}
	t.Cleanup(func() { cliSessionReset = orig })

	req := sessionReqFrame(t, protocol.MethodSessionsReset, map[string]any{"key": "reset-key"})
	m.handleReset(context.Background(), client, req)

	if !called {
		t.Fatal("sessions.reset did not clear the Claude CLI session")
	}
	if gotKey != "reset-key" {
		t.Errorf("CLI reset key = %q, want %q", gotKey, "reset-key")
	}
	if len(sess.resetCalled) != 1 || sess.resetCalled[0] != "reset-key" {
		t.Errorf("native store.Reset calls = %v, want [reset-key]", sess.resetCalled)
	}
}
