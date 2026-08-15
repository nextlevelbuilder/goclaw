package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

func TestChatSend_HappyPath(t *testing.T) {
	runner := &fakeChatRunner{}
	sessions := newFakeSessionStore()
	srv := newTestMCPServer()
	registerChatCRUDTools(srv, runner, sessions)

	result := callTool(t, srv, "goclaw_chat_send", map[string]any{"message": "hello"})
	require.False(t, toolIsError(result), toolResultText(result))
	assert.Equal(t, "hello", runner.lastMessage)
}

func TestChatSend_RunnerError(t *testing.T) {
	runner := &fakeChatRunner{sendErr: assertErr("provider down")}
	sessions := newFakeSessionStore()
	srv := newTestMCPServer()
	registerChatCRUDTools(srv, runner, sessions)

	result := callTool(t, srv, "goclaw_chat_send", map[string]any{"message": "hello"})
	assert.True(t, toolIsError(result))
}

func TestChatSend_NilRunner(t *testing.T) {
	sessions := newFakeSessionStore()
	srv := newTestMCPServer()
	registerChatCRUDTools(srv, nil, sessions)

	result := callTool(t, srv, "goclaw_chat_send", map[string]any{"message": "hello"})
	assert.True(t, toolIsError(result))
	assert.Contains(t, toolResultText(result), "chat runtime not available")
}

func TestChatHistory(t *testing.T) {
	sessions := newFakeSessionStore()
	sessions.history["sess-1"] = []providers.Message{{Role: "user", Content: "hi"}}
	runner := &fakeChatRunner{}
	srv := newTestMCPServer()
	registerChatCRUDTools(srv, runner, sessions)

	result := callTool(t, srv, "goclaw_chat_history", map[string]any{"session_key": "sess-1"})
	require.False(t, toolIsError(result))
	assert.Contains(t, toolResultText(result), "hi")
}

func TestChatInject_AddsLabeledMessage(t *testing.T) {
	sessions := newFakeSessionStore()
	runner := &fakeChatRunner{}
	srv := newTestMCPServer()
	registerChatCRUDTools(srv, runner, sessions)

	result := callTool(t, srv, "goclaw_chat_inject", map[string]any{
		"session_key": "sess-1", "message": "note text", "label": "note",
	})
	require.False(t, toolIsError(result))
	require.Len(t, sessions.history["sess-1"], 1)
	assert.Contains(t, sessions.history["sess-1"][0].Content, "note text")
}

func TestChatAbort_RequiresRunIDOrSessionKey(t *testing.T) {
	runner := &fakeChatRunner{}
	sessions := newFakeSessionStore()
	srv := newTestMCPServer()
	registerChatCRUDTools(srv, runner, sessions)

	result := callTool(t, srv, "goclaw_chat_abort", map[string]any{})
	assert.True(t, toolIsError(result))
}

func TestChatSessionStatus_NilRunner(t *testing.T) {
	sessions := newFakeSessionStore()
	srv := newTestMCPServer()
	registerChatCRUDTools(srv, nil, sessions)

	result := callTool(t, srv, "goclaw_chat_session_status", map[string]any{"session_key": "sess-1"})
	assert.True(t, toolIsError(result))
}

func TestChatBehaviorPreview_NoChannelManager(t *testing.T) {
	cfg := &config.Config{}
	srv := newTestMCPServer()
	registerChatBehaviorCRUDTool(srv, cfg, nil)

	result := callTool(t, srv, "goclaw_chat_behavior_preview", map[string]any{"content": "hello"})
	require.False(t, toolIsError(result))
}
