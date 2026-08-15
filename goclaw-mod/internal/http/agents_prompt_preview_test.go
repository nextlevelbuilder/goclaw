package http

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/mcp"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// mockMCPStoreForAdapter is a minimal store.MCPServerStore fake exercising
// only ListAccessible, the single method mcp.Manager.ListToolsForAgent
// depends on. All other methods are no-ops — this test only verifies that
// mcpPreviewAdapter.ListToolsForAgent preserves the real cached parameter
// schema returned by the underlying *mcp.Manager.
type mockMCPStoreForAdapter struct {
	accessible []store.MCPAccessInfo
}

func (m *mockMCPStoreForAdapter) ListAccessible(_ context.Context, _ uuid.UUID, _ string) ([]store.MCPAccessInfo, error) {
	return m.accessible, nil
}
func (m *mockMCPStoreForAdapter) CreateServer(context.Context, *store.MCPServerData) error { return nil }
func (m *mockMCPStoreForAdapter) GetServer(context.Context, uuid.UUID) (*store.MCPServerData, error) {
	return nil, nil
}
func (m *mockMCPStoreForAdapter) GetServerByName(context.Context, string) (*store.MCPServerData, error) {
	return nil, nil
}
func (m *mockMCPStoreForAdapter) ListServers(context.Context) ([]store.MCPServerData, error) {
	return nil, nil
}
func (m *mockMCPStoreForAdapter) UpdateServer(context.Context, uuid.UUID, map[string]any) error {
	return nil
}
func (m *mockMCPStoreForAdapter) DeleteServer(context.Context, uuid.UUID) error { return nil }
func (m *mockMCPStoreForAdapter) CacheToolDescriptions(context.Context, uuid.UUID, map[string]store.CachedToolInfo) error {
	return nil
}
func (m *mockMCPStoreForAdapter) GrantToAgent(context.Context, *store.MCPAgentGrant) error {
	return nil
}
func (m *mockMCPStoreForAdapter) RevokeFromAgent(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}
func (m *mockMCPStoreForAdapter) ListAgentGrants(context.Context, uuid.UUID) ([]store.MCPAgentGrant, error) {
	return nil, nil
}
func (m *mockMCPStoreForAdapter) ListServerGrants(context.Context, uuid.UUID) ([]store.MCPAgentGrant, error) {
	return nil, nil
}
func (m *mockMCPStoreForAdapter) GrantToUser(context.Context, *store.MCPUserGrant) error {
	return nil
}
func (m *mockMCPStoreForAdapter) RevokeFromUser(context.Context, uuid.UUID, string) error {
	return nil
}
func (m *mockMCPStoreForAdapter) CountAgentGrantsByServer(context.Context) (map[uuid.UUID]int, error) {
	return nil, nil
}
func (m *mockMCPStoreForAdapter) CreateRequest(context.Context, *store.MCPAccessRequest) error {
	return nil
}
func (m *mockMCPStoreForAdapter) ListPendingRequests(context.Context) ([]store.MCPAccessRequest, error) {
	return nil, nil
}
func (m *mockMCPStoreForAdapter) ReviewRequest(context.Context, uuid.UUID, bool, string, string) error {
	return nil
}
func (m *mockMCPStoreForAdapter) GetUserCredentials(context.Context, uuid.UUID, string) (*store.MCPUserCredentials, error) {
	return nil, nil
}
func (m *mockMCPStoreForAdapter) SetUserCredentials(context.Context, uuid.UUID, string, store.MCPUserCredentials) error {
	return nil
}
func (m *mockMCPStoreForAdapter) DeleteUserCredentials(context.Context, uuid.UUID, string) error {
	return nil
}

// TestMCPPreviewAdapter_PreservesRealParameterSchema proves that
// mcpPreviewAdapter.ListToolsForAgent (which bridges *mcp.Manager's
// mcp.MCPToolPreviewInfo into agent.MCPToolPreviewInfo, the shape consumed
// by BuildPreviewPrompt) forwards the real cached JSON Schema parameters
// instead of silently dropping them. This is a regression test: the field
// conversion previously only copied RegisteredName and Description, always
// zeroing Parameters, so every MCP tool fell back to the bare
// {"type":"object"} placeholder in prompt preview even when a genuine
// schema was cached from a live MCP connection (see 1290d4f1's
// tool_cache work in internal/mcp/manager.go).
func TestMCPPreviewAdapter_PreservesRealParameterSchema(t *testing.T) {
	serverID := uuid.New()
	toolCache := map[string]store.CachedToolInfo{
		"update_dns_record": {
			Description: "Update a DNS record",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"zone_id":{"type":"string"},"record_id":{"type":"string"}},"required":["zone_id","record_id"]}`),
		},
	}
	settings, err := json.Marshal(map[string]any{"tool_cache": toolCache})
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}

	mockStore := &mockMCPStoreForAdapter{
		accessible: []store.MCPAccessInfo{
			{
				Server: store.MCPServerData{
					BaseModel: store.BaseModel{ID: serverID},
					Name:      "cloudflare",
					Enabled:   true,
					Settings:  settings,
				},
				ToolAllow: nil, // unrestricted grant — matches production's typical config
				ToolDeny:  nil,
			},
		},
	}

	mgr := mcp.NewManager(tools.NewRegistry(), mcp.WithStore(mockStore))
	adapter := NewMCPPreviewAdapter(mgr)

	got, err := adapter.ListToolsForAgent(context.Background(), uuid.New(), "user-1")
	if err != nil {
		t.Fatalf("ListToolsForAgent: %v", err)
	}

	var found bool
	for _, mt := range got {
		if mt.RegisteredName != "mcp_cloudflare__update_dns_record" {
			continue
		}
		found = true
		if len(mt.Parameters) == 0 {
			t.Fatal("expected real cached parameter schema to be preserved through the adapter, got empty Parameters")
		}
		var schema map[string]any
		if err := json.Unmarshal(mt.Parameters, &schema); err != nil {
			t.Fatalf("unmarshal parameters: %v", err)
		}
		props, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("expected real schema properties, got %+v", schema)
		}
		if _, ok := props["zone_id"]; !ok {
			t.Errorf("expected real schema property 'zone_id', got %+v", props)
		}
		if _, ok := props["record_id"]; !ok {
			t.Errorf("expected real schema property 'record_id', got %+v", props)
		}
	}
	if !found {
		t.Fatalf("expected mcp_cloudflare__update_dns_record in adapter result, got: %+v", got)
	}
}
