package mcp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestAgentsCRUD_CreateGetUpdateDelete(t *testing.T) {
	agents := newFakeAgentStore()
	srv := newTestMCPServer()
	registerAgentCRUDTools(srv, agents)

	created := callTool(t, srv, "goclaw_agents_create", map[string]any{
		"agent_key": "support-bot",
	})
	require.False(t, toolIsError(created), "create should succeed: %s", toolResultText(created))
	assert.Contains(t, toolResultText(created), "support-bot")

	// Happy path: get by agent_key.
	got := callTool(t, srv, "goclaw_agents_get", map[string]any{"agent_key": "support-bot"})
	require.False(t, toolIsError(got))
	assert.Contains(t, toolResultText(got), "support-bot")

	// Error path: get with neither id nor agent_key.
	missingArgs := callTool(t, srv, "goclaw_agents_get", map[string]any{})
	assert.True(t, toolIsError(missingArgs))

	// Error path: get a nonexistent agent by id.
	notFound := callTool(t, srv, "goclaw_agents_get", map[string]any{"id": uuid.New().String()})
	assert.True(t, toolIsError(notFound))

	var id uuid.UUID
	for k := range agents.byID {
		id = k
	}

	updated := callTool(t, srv, "goclaw_agents_update", map[string]any{
		"id": id.String(), "display_name": "Support Bot v2",
	})
	require.False(t, toolIsError(updated), toolResultText(updated))
	assert.Contains(t, toolResultText(updated), "Support Bot v2")

	// Error path: update with no fields to update.
	noFields := callTool(t, srv, "goclaw_agents_update", map[string]any{"id": id.String()})
	assert.True(t, toolIsError(noFields))

	deleted := callTool(t, srv, "goclaw_agents_delete", map[string]any{"id": id.String()})
	require.False(t, toolIsError(deleted))
	assert.Contains(t, toolResultText(deleted), "true")

	// Error path: deleting again fails (already gone).
	deleteAgain := callTool(t, srv, "goclaw_agents_delete", map[string]any{"id": id.String()})
	assert.True(t, toolIsError(deleteAgain))
}

func TestAgentsCRUD_List(t *testing.T) {
	agents := newFakeAgentStore()
	agents.add(&store.AgentData{BaseModel: store.BaseModel{ID: uuid.New()}, AgentKey: "a", OwnerID: "u1"})
	agents.add(&store.AgentData{BaseModel: store.BaseModel{ID: uuid.New()}, AgentKey: "b", OwnerID: "u2"})
	srv := newTestMCPServer()
	registerAgentCRUDTools(srv, agents)

	result := callTool(t, srv, "goclaw_agents_list", map[string]any{"owner_id": "u1"})
	require.False(t, toolIsError(result))
	assert.Contains(t, toolResultText(result), `"a"`)
	assert.NotContains(t, toolResultText(result), `"b"`)
}

func TestAgentRuntimeCRUD_GetAndWait(t *testing.T) {
	srv := newTestMCPServer()
	agents := newFakeAgentStore()

	var called []string
	lookup := AgentRuntimeLookup(func(_ context.Context, agentID string) (string, bool, error) {
		called = append(called, agentID)
		return "resolved-" + agentID, agentID == "running-agent", nil
	})
	registerAgentRuntimeCRUDTools(srv, agents, lookup)

	idle := callTool(t, srv, "goclaw_agent_get", map[string]any{"agent_id": "idle-agent"})
	require.False(t, toolIsError(idle))
	assert.Contains(t, toolResultText(idle), `"isRunning":false`)

	running := callTool(t, srv, "goclaw_agent_wait", map[string]any{"agent_id": "running-agent"})
	require.False(t, toolIsError(running))
	assert.Contains(t, toolResultText(running), `"status":"running"`)

	assert.Equal(t, []string{"idle-agent", "running-agent"}, called)
}

func TestAgentIdentityGet_DefaultsWhenAgentMissing(t *testing.T) {
	agents := newFakeAgentStore()
	srv := newTestMCPServer()
	registerAgentRuntimeCRUDTools(srv, agents, func(_ context.Context, agentID string) (string, bool, error) { return agentID, false, nil })

	result := callTool(t, srv, "goclaw_agent_identity_get", map[string]any{"agent_id": "unknown"})
	require.False(t, toolIsError(result))
	assert.Contains(t, toolResultText(result), `"agentId":"unknown"`)
}

func TestAgentsFilesGet_RejectsDisallowedFileName(t *testing.T) {
	agents := newFakeAgentStore()
	agents.add(&store.AgentData{BaseModel: store.BaseModel{ID: uuid.New()}, AgentKey: "default"})
	srv := newTestMCPServer()
	registerAgentRuntimeCRUDTools(srv, agents, func(_ context.Context, agentID string) (string, bool, error) { return agentID, false, nil })

	result := callTool(t, srv, "goclaw_agents_files_get", map[string]any{"agent_id": "default", "name": "TOOLS.md"})
	assert.True(t, toolIsError(result), "TOOLS.md is intentionally excluded from allowedAgentContextFiles")
}

func TestAgentsFilesSetAndGet_RoundTrip(t *testing.T) {
	agents := newFakeAgentStore()
	agents.add(&store.AgentData{BaseModel: store.BaseModel{ID: uuid.New()}, AgentKey: "default"})
	srv := newTestMCPServer()
	registerAgentRuntimeCRUDTools(srv, agents, func(_ context.Context, agentID string) (string, bool, error) { return agentID, false, nil })

	setResult := callTool(t, srv, "goclaw_agents_files_set", map[string]any{
		"agent_id": "default", "name": "SOUL.md", "content": "be helpful",
	})
	require.False(t, toolIsError(setResult))

	getResult := callTool(t, srv, "goclaw_agents_files_get", map[string]any{"agent_id": "default", "name": "SOUL.md"})
	require.False(t, toolIsError(getResult))
	assert.Contains(t, toolResultText(getResult), "be helpful")
}
