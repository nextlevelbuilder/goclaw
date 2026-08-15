package mcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestHeartbeatGet_NotConfigured(t *testing.T) {
	hb := newFakeHeartbeatStore()
	agents := newFakeAgentStore()
	agentID := uuid.New()
	agents.add(&store.AgentData{BaseModel: store.BaseModel{ID: agentID}, AgentKey: "a"})
	srv := newTestMCPServer()
	registerHeartbeatCRUDTools(srv, hb, agents, nil)

	result := callTool(t, srv, "goclaw_heartbeat_get", map[string]any{"agent_id": "a"})
	require.False(t, toolIsError(result))
	assert.Contains(t, toolResultText(result), `"heartbeat":null`)
}

func TestHeartbeatGet_InvalidAgent(t *testing.T) {
	hb := newFakeHeartbeatStore()
	agents := newFakeAgentStore()
	srv := newTestMCPServer()
	registerHeartbeatCRUDTools(srv, hb, agents, nil)

	result := callTool(t, srv, "goclaw_heartbeat_get", map[string]any{"agent_id": "missing"})
	assert.True(t, toolIsError(result))
}

func TestHeartbeatSet_RejectsIntervalBelowMinimum(t *testing.T) {
	hb := newFakeHeartbeatStore()
	agents := newFakeAgentStore()
	agentID := uuid.New()
	agents.add(&store.AgentData{BaseModel: store.BaseModel{ID: agentID}, AgentKey: "a"})
	srv := newTestMCPServer()
	registerHeartbeatCRUDTools(srv, hb, agents, nil)

	result := callTool(t, srv, "goclaw_heartbeat_set", map[string]any{"agent_id": "a", "interval_sec": float64(60)})
	assert.True(t, toolIsError(result))
	assert.Contains(t, toolResultText(result), "minimum interval")
}

func TestHeartbeatSet_HappyPath(t *testing.T) {
	hb := newFakeHeartbeatStore()
	agents := newFakeAgentStore()
	agentID := uuid.New()
	agents.add(&store.AgentData{BaseModel: store.BaseModel{ID: agentID}, AgentKey: "a"})
	srv := newTestMCPServer()
	registerHeartbeatCRUDTools(srv, hb, agents, nil)

	result := callTool(t, srv, "goclaw_heartbeat_set", map[string]any{
		"agent_id": "a", "enabled": true, "interval_sec": float64(600),
	})
	require.False(t, toolIsError(result), toolResultText(result))
	assert.Equal(t, 600, hb.byAgent[agentID].IntervalSec)
	assert.True(t, hb.byAgent[agentID].Enabled)
}

func TestHeartbeatToggle_NotConfigured(t *testing.T) {
	hb := newFakeHeartbeatStore()
	agents := newFakeAgentStore()
	agentID := uuid.New()
	agents.add(&store.AgentData{BaseModel: store.BaseModel{ID: agentID}, AgentKey: "a"})
	srv := newTestMCPServer()
	registerHeartbeatCRUDTools(srv, hb, agents, nil)

	result := callTool(t, srv, "goclaw_heartbeat_toggle", map[string]any{"agent_id": "a", "enabled": true})
	assert.True(t, toolIsError(result))
	assert.Contains(t, toolResultText(result), "not configured")
}

// TestHeartbeatTest_AlwaysUnavailable guards the documented stub behavior.
func TestHeartbeatTest_AlwaysUnavailable(t *testing.T) {
	hb := newFakeHeartbeatStore()
	agents := newFakeAgentStore()
	srv := newTestMCPServer()
	registerHeartbeatCRUDTools(srv, hb, agents, nil)

	result := callTool(t, srv, "goclaw_heartbeat_test", map[string]any{"agent_id": "a"})
	assert.True(t, toolIsError(result))
	assert.Contains(t, toolResultText(result), "not available")
}

func TestHeartbeatChecklist_SetAndGet(t *testing.T) {
	hb := newFakeHeartbeatStore()
	agents := newFakeAgentStore()
	agentID := uuid.New()
	agents.add(&store.AgentData{BaseModel: store.BaseModel{ID: agentID}, AgentKey: "a"})
	srv := newTestMCPServer()
	registerHeartbeatCRUDTools(srv, hb, agents, nil)

	setResult := callTool(t, srv, "goclaw_heartbeat_checklist_set", map[string]any{"agent_id": "a", "content": "- check email"})
	require.False(t, toolIsError(setResult))

	getResult := callTool(t, srv, "goclaw_heartbeat_checklist_get", map[string]any{"agent_id": "a"})
	require.False(t, toolIsError(getResult))
	assert.Contains(t, toolResultText(getResult), "check email")
}
