package mcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestChannelsList_And_Status(t *testing.T) {
	mgr := channels.NewManager(bus.New())
	srv := newTestMCPServer()
	registerChannelsCRUDTools(srv, mgr)

	list := callTool(t, srv, "goclaw_channels_list", map[string]any{})
	require.False(t, toolIsError(list))

	status := callTool(t, srv, "goclaw_channels_status", map[string]any{})
	require.False(t, toolIsError(status))
}

// TestChannelsToggle_AlwaysNotImplemented guards the documented stub
// behavior against accidental regression (or accidental silent
// implementation without updating the tool description).
func TestChannelsToggle_AlwaysNotImplemented(t *testing.T) {
	mgr := channels.NewManager(bus.New())
	srv := newTestMCPServer()
	registerChannelsCRUDTools(srv, mgr)

	result := callTool(t, srv, "goclaw_channels_toggle", map[string]any{"channel": "telegram", "enabled": true})
	assert.True(t, toolIsError(result))
	assert.Contains(t, toolResultText(result), "not implemented")
}

func TestChannelInstances_CreateGetUpdateDelete(t *testing.T) {
	insts := newFakeChannelInstanceStore()
	agents := newFakeAgentStore()
	agentID := uuid.New()
	agents.add(&store.AgentData{BaseModel: store.BaseModel{ID: agentID}, AgentKey: "support"})
	srv := newTestMCPServer()
	registerChannelInstancesCRUDTools(srv, insts, agents)

	created := callTool(t, srv, "goclaw_channel_instances_create", map[string]any{
		"name": "tg-1", "channel_type": "telegram", "agent_id": "support",
	})
	require.False(t, toolIsError(created), toolResultText(created))
	assert.Contains(t, toolResultText(created), "tg-1")
	// Credentials must never appear in cleartext.
	assert.NotContains(t, toolResultText(created), "***\":\"realvalue")

	var id uuid.UUID
	for k := range insts.byID {
		id = k
	}

	got := callTool(t, srv, "goclaw_channel_instances_get", map[string]any{"id": id.String()})
	require.False(t, toolIsError(got))

	invalidType := callTool(t, srv, "goclaw_channel_instances_create", map[string]any{
		"name": "bad", "channel_type": "not-a-real-type", "agent_id": "support",
	})
	assert.True(t, toolIsError(invalidType))

	updated := callTool(t, srv, "goclaw_channel_instances_update", map[string]any{
		"id": id.String(), "updates": map[string]any{"enabled": false},
	})
	require.False(t, toolIsError(updated))
	assert.False(t, insts.byID[id].Enabled)

	deleted := callTool(t, srv, "goclaw_channel_instances_delete", map[string]any{"id": id.String()})
	require.False(t, toolIsError(deleted))
}

func TestChannelInstancesDelete_RefusesDefaultInstance(t *testing.T) {
	insts := newFakeChannelInstanceStore()
	agents := newFakeAgentStore()
	id := uuid.New()
	insts.byID[id] = &store.ChannelInstanceData{BaseModel: store.BaseModel{ID: id}, Name: "telegram"} // "telegram" is a default/seeded name
	srv := newTestMCPServer()
	registerChannelInstancesCRUDTools(srv, insts, agents)

	result := callTool(t, srv, "goclaw_channel_instances_delete", map[string]any{"id": id.String()})
	assert.True(t, toolIsError(result))
	assert.Contains(t, toolResultText(result), "cannot delete a default instance")
}
