package mcp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nextlevelbuilder/goclaw/internal/hooks"
)

func TestHooksCreate_HappyPath(t *testing.T) {
	store := newFakeHookStore()
	srv := newTestMCPServer()
	registerHooksCRUDTools(srv, store)

	result := callTool(t, srv, "goclaw_hooks_create", map[string]any{
		"config": map[string]any{
			"handler_type": "http",
			"event":        "pre_tool_use",
			"scope":        "global",
			"config":       map[string]any{"url": "https://example.com"},
		},
	})
	require.False(t, toolIsError(result), toolResultText(result))
	assert.Len(t, store.created, 1)
}

func TestHooksCreate_MissingRequiredFields(t *testing.T) {
	store := newFakeHookStore()
	srv := newTestMCPServer()
	registerHooksCRUDTools(srv, store)

	result := callTool(t, srv, "goclaw_hooks_create", map[string]any{
		"config": map[string]any{"handler_type": "http"},
	})
	assert.True(t, toolIsError(result))
}

func TestHooksList_FiltersByEnabled(t *testing.T) {
	store := newFakeHookStore()
	store.created[uuid.New()] = hooks.HookConfig{Enabled: true}
	srv := newTestMCPServer()
	registerHooksCRUDTools(srv, store)

	result := callTool(t, srv, "goclaw_hooks_list", map[string]any{})
	require.False(t, toolIsError(result))
	assert.Contains(t, toolResultText(result), "hooks")
}

func TestHooksToggle_And_Delete(t *testing.T) {
	store := newFakeHookStore()
	id, err := store.Create(context.Background(), hooks.HookConfig{HandlerType: "http", Event: "pre_tool_use", Scope: "global"})
	require.NoError(t, err)
	srv := newTestMCPServer()
	registerHooksCRUDTools(srv, store)

	toggled := callTool(t, srv, "goclaw_hooks_toggle", map[string]any{"hook_id": id.String(), "enabled": true})
	require.False(t, toolIsError(toggled))

	deleted := callTool(t, srv, "goclaw_hooks_delete", map[string]any{"hook_id": id.String()})
	require.False(t, toolIsError(deleted))

	deleteAgain := callTool(t, srv, "goclaw_hooks_delete", map[string]any{"hook_id": id.String()})
	assert.True(t, toolIsError(deleteAgain))
}

func TestHooksUpdate_NotFound(t *testing.T) {
	store := newFakeHookStore()
	srv := newTestMCPServer()
	registerHooksCRUDTools(srv, store)

	result := callTool(t, srv, "goclaw_hooks_update", map[string]any{
		"hook_id": uuid.New().String(),
		"updates": map[string]any{"enabled": false},
	})
	assert.True(t, toolIsError(result))
}

// TestHooksTest_And_History_AreDocumentedStubs guards the two
// intentionally-unimplemented tools on this MCP surface against accidental
// silent regression.
func TestHooksTest_And_History_AreDocumentedStubs(t *testing.T) {
	store := newFakeHookStore()
	srv := newTestMCPServer()
	registerHooksCRUDTools(srv, store)

	testResult := callTool(t, srv, "goclaw_hooks_test", map[string]any{"config": map[string]any{}})
	assert.True(t, toolIsError(testResult))
	assert.Contains(t, toolResultText(testResult), "not available")

	historyResult := callTool(t, srv, "goclaw_hooks_history", map[string]any{})
	require.False(t, toolIsError(historyResult))
	assert.Contains(t, toolResultText(historyResult), "not yet implemented")
}
