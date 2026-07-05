package mcp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestResolveAgentUUID_ByUUID(t *testing.T) {
	agents := newFakeAgentStore()
	id := uuid.New()
	agents.add(&store.AgentData{BaseModel: store.BaseModel{ID: id}, AgentKey: "support"})

	got, err := resolveAgentUUID(context.Background(), agents, id.String())
	require.NoError(t, err)
	assert.Equal(t, id, got)
}

func TestResolveAgentUUID_ByKey(t *testing.T) {
	agents := newFakeAgentStore()
	id := uuid.New()
	agents.add(&store.AgentData{BaseModel: store.BaseModel{ID: id}, AgentKey: "support"})

	got, err := resolveAgentUUID(context.Background(), agents, "support")
	require.NoError(t, err)
	assert.Equal(t, id, got)
}

func TestResolveAgentUUID_NotFound(t *testing.T) {
	agents := newFakeAgentStore()
	_, err := resolveAgentUUID(context.Background(), agents, "missing-agent")
	assert.Error(t, err)
}

func TestResolveAgentInfo_ByUUIDAndKey(t *testing.T) {
	agents := newFakeAgentStore()
	id := uuid.New()
	agents.add(&store.AgentData{BaseModel: store.BaseModel{ID: id}, AgentKey: "support", DisplayName: "Support"})

	byID, err := resolveAgentInfo(context.Background(), agents, id.String())
	require.NoError(t, err)
	assert.Equal(t, "Support", byID.DisplayName)

	byKey, err := resolveAgentInfo(context.Background(), agents, "support")
	require.NoError(t, err)
	assert.Equal(t, id, byKey.ID)
}

func TestJSONToolResult_MarshalsValue(t *testing.T) {
	result, err := jsonToolResult(map[string]string{"hello": "world"})
	require.NoError(t, err)
	assert.False(t, toolIsError(result))
	assert.Contains(t, toolResultText(result), `"hello":"world"`)
}

func TestJSONToolResult_UnmarshalableValue_ReturnsToolError(t *testing.T) {
	// channels cannot be JSON-marshaled; jsonToolResult should report this as
	// a tool-level error rather than a Go error/panic.
	result, err := jsonToolResult(make(chan int))
	require.NoError(t, err)
	assert.True(t, toolIsError(result))
	assert.Contains(t, toolResultText(result), "marshal result")
}

func TestToolError_WrapsPrefixAndMessage(t *testing.T) {
	result, err := toolError("agents.get", assertErr("boom"))
	require.NoError(t, err)
	assert.True(t, toolIsError(result))
	assert.Equal(t, "agents.get: boom", toolResultText(result))
}

// assertErr is a tiny error constructor to avoid importing "errors" solely
// for one string-error test case.
type assertErr string

func (e assertErr) Error() string { return string(e) }
