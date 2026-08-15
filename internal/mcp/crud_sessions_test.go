package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestSessionsList_RequiresAgentID(t *testing.T) {
	sessions := newFakeSessionStore()
	srv := newTestMCPServer()
	registerSessionCRUDTools(srv, sessions)

	missing := callTool(t, srv, "goclaw_sessions_list", map[string]any{})
	assert.True(t, toolIsError(missing))

	sessions.add("s1:agent1", &store.SessionData{Key: "s1:agent1"})
	ok := callTool(t, srv, "goclaw_sessions_list", map[string]any{"agent_id": "agent1"})
	require.False(t, toolIsError(ok))
}

func TestSessionsGet_NotFound(t *testing.T) {
	sessions := newFakeSessionStore()
	srv := newTestMCPServer()
	registerSessionCRUDTools(srv, sessions)

	result := callTool(t, srv, "goclaw_sessions_get", map[string]any{"key": "missing"})
	assert.True(t, toolIsError(result))
}

func TestSessionsGet_Found(t *testing.T) {
	sessions := newFakeSessionStore()
	sessions.add("sess-1", &store.SessionData{Key: "sess-1", Label: "hello"})
	srv := newTestMCPServer()
	registerSessionCRUDTools(srv, sessions)

	result := callTool(t, srv, "goclaw_sessions_get", map[string]any{"key": "sess-1"})
	require.False(t, toolIsError(result))
	assert.Contains(t, toolResultText(result), "hello")
}

func TestSessionsPatch_UpdatesLabelAndMetadata(t *testing.T) {
	sessions := newFakeSessionStore()
	sessions.add("sess-1", &store.SessionData{Key: "sess-1"})
	srv := newTestMCPServer()
	registerSessionCRUDTools(srv, sessions)

	result := callTool(t, srv, "goclaw_sessions_patch", map[string]any{
		"key":      "sess-1",
		"label":    "renamed",
		"metadata": map[string]any{"foo": "bar"},
	})
	require.False(t, toolIsError(result))
	assert.Equal(t, "renamed", sessions.labels["sess-1"])
	assert.Equal(t, "bar", sessions.metadata["sess-1"]["foo"])
}

func TestSessionsPatch_NotFound(t *testing.T) {
	sessions := newFakeSessionStore()
	srv := newTestMCPServer()
	registerSessionCRUDTools(srv, sessions)

	result := callTool(t, srv, "goclaw_sessions_patch", map[string]any{"key": "missing"})
	assert.True(t, toolIsError(result))
}

func TestSessionsDelete_HappyAndErrorPath(t *testing.T) {
	sessions := newFakeSessionStore()
	sessions.add("sess-1", &store.SessionData{Key: "sess-1"})
	srv := newTestMCPServer()
	registerSessionCRUDTools(srv, sessions)

	ok := callTool(t, srv, "goclaw_sessions_delete", map[string]any{"key": "sess-1"})
	require.False(t, toolIsError(ok))
	assert.Contains(t, sessions.deleted, "sess-1")

	sessions.deleteErr = assertErr("boom")
	failed := callTool(t, srv, "goclaw_sessions_delete", map[string]any{"key": "sess-2"})
	assert.True(t, toolIsError(failed))
}

func TestSessionsReset(t *testing.T) {
	sessions := newFakeSessionStore()
	srv := newTestMCPServer()
	registerSessionCRUDTools(srv, sessions)

	result := callTool(t, srv, "goclaw_sessions_reset", map[string]any{"key": "sess-1"})
	require.False(t, toolIsError(result))
	assert.Contains(t, sessions.resetKeys, "sess-1")
}

func TestSessionsCompact_TruncatesHistory(t *testing.T) {
	sessions := newFakeSessionStore()
	sessions.history["sess-1"] = []providers.Message{
		{Role: "user", Content: "1"}, {Role: "user", Content: "2"}, {Role: "user", Content: "3"},
	}
	srv := newTestMCPServer()
	registerSessionCRUDTools(srv, sessions)

	result := callTool(t, srv, "goclaw_sessions_compact", map[string]any{"key": "sess-1", "keep_last": 1})
	require.False(t, toolIsError(result))
	assert.Len(t, sessions.history["sess-1"], 1)
	assert.Contains(t, toolResultText(result), `"original":3`)
}
