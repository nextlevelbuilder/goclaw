package gateway

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
)

// TestAgentChatRunner_Send_UnknownAgent_ReturnsWrappedError exercises the
// error path only: agentChatRunner.Send needs a live *agent.Router with a
// resolver (DB-backed agent lookup + provider wiring) to reach a real LLM
// call, which is out of scope for a unit test — full happy-path coverage of
// Send lives in internal/agent's own Router/Loop test suite. This verifies
// the adapter's own responsibilities: default agentID resolution ("default"
// when empty) and error wrapping when the underlying agent can't be
// resolved (no resolver configured on a bare Router).
func TestAgentChatRunner_Send_UnknownAgent_ReturnsWrappedError(t *testing.T) {
	router := agent.NewRouter()
	runner := &agentChatRunner{agents: router}

	result, err := runner.Send(context.Background(), "", "", "hello", nil)
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), `get agent "default"`)
}

// TestAgentChatRunner_Abort_NoActiveRuns_ReportsNotFound verifies the
// aggregation logic in Abort when there is nothing to abort, both by runID
// and by sessionKey.
func TestAgentChatRunner_Abort_NoActiveRuns_ReportsNotFound(t *testing.T) {
	router := agent.NewRouter()
	runner := &agentChatRunner{agents: router}

	byRunID, err := runner.Abort(context.Background(), "run-does-not-exist", "")
	require.NoError(t, err)
	assert.True(t, byRunID.OK)
	assert.False(t, byRunID.Aborted)
	assert.True(t, byRunID.NotFound)

	bySessionKey, err := runner.Abort(context.Background(), "", "session-does-not-exist")
	require.NoError(t, err)
	assert.True(t, bySessionKey.OK)
	assert.False(t, bySessionKey.Aborted)
	assert.Empty(t, bySessionKey.RunIDs)
}

// TestAgentChatRunner_SessionStatus_IdleSession verifies the adapter reports
// a non-running status (with no activity/runID) for a session that never had
// a run started against this Router.
func TestAgentChatRunner_SessionStatus_IdleSession(t *testing.T) {
	router := agent.NewRouter()
	runner := &agentChatRunner{agents: router}

	result, err := runner.SessionStatus(context.Background(), "never-run-session")
	require.NoError(t, err)
	assert.False(t, result.IsRunning)
	assert.Empty(t, result.RunID)
	assert.Nil(t, result.Activity)
}
