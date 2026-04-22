package mcp

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// mockMCPStore implements store.MCPServerStore for testing.
type mockMCPStore struct {
	accessible []store.MCPAccessInfo
	callCount  int32
}

func (m *mockMCPStore) ListAccessible(ctx context.Context, agentID uuid.UUID, userID string) ([]store.MCPAccessInfo, error) {
	atomic.AddInt32(&m.callCount, 1)
	return m.accessible, nil
}

// Stub implementations for interface compliance (not used in grant_checker tests)
func (m *mockMCPStore) CreateServer(ctx context.Context, s *store.MCPServerData) error { return nil }
func (m *mockMCPStore) GetServer(ctx context.Context, id uuid.UUID) (*store.MCPServerData, error) {
	return nil, nil
}
func (m *mockMCPStore) GetServerByName(ctx context.Context, name string) (*store.MCPServerData, error) {
	return nil, nil
}
func (m *mockMCPStore) ListServers(ctx context.Context) ([]store.MCPServerData, error) { return nil, nil }
func (m *mockMCPStore) UpdateServer(ctx context.Context, id uuid.UUID, updates map[string]any) error {
	return nil
}
func (m *mockMCPStore) DeleteServer(ctx context.Context, id uuid.UUID) error        { return nil }
func (m *mockMCPStore) GrantToAgent(ctx context.Context, g *store.MCPAgentGrant) error { return nil }
func (m *mockMCPStore) RevokeFromAgent(ctx context.Context, serverID, agentID uuid.UUID) error {
	return nil
}
func (m *mockMCPStore) ListAgentGrants(ctx context.Context, agentID uuid.UUID) ([]store.MCPAgentGrant, error) {
	return nil, nil
}
func (m *mockMCPStore) ListServerGrants(ctx context.Context, serverID uuid.UUID) ([]store.MCPAgentGrant, error) {
	return nil, nil
}
func (m *mockMCPStore) GrantToUser(ctx context.Context, g *store.MCPUserGrant) error { return nil }
func (m *mockMCPStore) RevokeFromUser(ctx context.Context, serverID uuid.UUID, userID string) error {
	return nil
}
func (m *mockMCPStore) CountAgentGrantsByServer(ctx context.Context) (map[uuid.UUID]int, error) {
	return nil, nil
}
func (m *mockMCPStore) CreateRequest(ctx context.Context, req *store.MCPAccessRequest) error {
	return nil
}
func (m *mockMCPStore) ListPendingRequests(ctx context.Context) ([]store.MCPAccessRequest, error) {
	return nil, nil
}
func (m *mockMCPStore) ReviewRequest(ctx context.Context, requestID uuid.UUID, approved bool, reviewedBy, note string) error {
	return nil
}
func (m *mockMCPStore) GetUserCredentials(ctx context.Context, serverID uuid.UUID, userID string) (*store.MCPUserCredentials, error) {
	return nil, nil
}
func (m *mockMCPStore) SetUserCredentials(ctx context.Context, serverID uuid.UUID, userID string, creds store.MCPUserCredentials) error {
	return nil
}
func (m *mockMCPStore) DeleteUserCredentials(ctx context.Context, serverID uuid.UUID, userID string) error {
	return nil
}

func TestStoreGrantChecker_CacheHit(t *testing.T) {
	serverID := uuid.New()
	agentID := uuid.New()
	userID := "test-user"

	mockStore := &mockMCPStore{
		accessible: []store.MCPAccessInfo{
			{Server: store.MCPServerData{BaseModel: store.BaseModel{ID: serverID}, Name: "test-server"}},
		},
	}

	gc := NewStoreGrantChecker(mockStore, nil)
	ctx := context.Background()

	// First call — cache miss, queries store
	result1 := gc.IsAllowed(ctx, agentID, userID, serverID, "any_tool")
	if !result1 {
		t.Error("expected allowed for accessible server")
	}
	if mockStore.callCount != 1 {
		t.Errorf("expected 1 DB call, got %d", mockStore.callCount)
	}

	// Second call — cache hit, no additional DB call
	result2 := gc.IsAllowed(ctx, agentID, userID, serverID, "another_tool")
	if !result2 {
		t.Error("expected allowed for accessible server (cached)")
	}
	if mockStore.callCount != 1 {
		t.Errorf("expected 1 DB call (cache hit), got %d", mockStore.callCount)
	}
}

func TestStoreGrantChecker_InvalidateClearsCache(t *testing.T) {
	serverID := uuid.New()
	agentID := uuid.New()
	userID := "test-user"

	mockStore := &mockMCPStore{
		accessible: []store.MCPAccessInfo{
			{Server: store.MCPServerData{BaseModel: store.BaseModel{ID: serverID}, Name: "test-server"}},
		},
	}

	gc := NewStoreGrantChecker(mockStore, nil)
	ctx := context.Background()

	// Warm the cache
	gc.IsAllowed(ctx, agentID, userID, serverID, "tool")
	if mockStore.callCount != 1 {
		t.Fatalf("expected 1 call after first IsAllowed, got %d", mockStore.callCount)
	}

	// Invalidate cache
	gc.Invalidate()

	// Next call should query store again
	gc.IsAllowed(ctx, agentID, userID, serverID, "tool")
	if mockStore.callCount != 2 {
		t.Errorf("expected 2 calls after invalidation, got %d", mockStore.callCount)
	}
}

func TestStoreGrantChecker_ToolAllowFilter(t *testing.T) {
	serverID := uuid.New()
	agentID := uuid.New()
	userID := "test-user"

	mockStore := &mockMCPStore{
		accessible: []store.MCPAccessInfo{
			{
				Server:    store.MCPServerData{BaseModel: store.BaseModel{ID: serverID}, Name: "test-server"},
				ToolAllow: []string{"allowed_tool", "another_allowed"},
			},
		},
	}

	gc := NewStoreGrantChecker(mockStore, nil)
	ctx := context.Background()

	// Allowed tool
	if !gc.IsAllowed(ctx, agentID, userID, serverID, "allowed_tool") {
		t.Error("expected true for tool in allow list")
	}

	// Not allowed tool
	if gc.IsAllowed(ctx, agentID, userID, serverID, "blocked_tool") {
		t.Error("expected false for tool not in allow list")
	}
}

func TestStoreGrantChecker_NoGrant_Denied(t *testing.T) {
	serverID := uuid.New()
	agentID := uuid.New()
	userID := "test-user"

	mockStore := &mockMCPStore{
		accessible: []store.MCPAccessInfo{}, // No accessible servers
	}

	gc := NewStoreGrantChecker(mockStore, nil)
	ctx := context.Background()

	// No grant = denied
	if gc.IsAllowed(ctx, agentID, userID, serverID, "any_tool") {
		t.Error("expected false when no grant exists")
	}
}

func TestStoreGrantChecker_NilStore_AllowsAll(t *testing.T) {
	gc := NewStoreGrantChecker(nil, nil)
	ctx := context.Background()

	// Nil store = config-path mode, skip check
	if !gc.IsAllowed(ctx, uuid.New(), "user", uuid.New(), "any_tool") {
		t.Error("expected true when store is nil (config-path mode)")
	}
}
