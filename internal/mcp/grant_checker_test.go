package mcp

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
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
func (m *mockMCPStore) ListServers(ctx context.Context) ([]store.MCPServerData, error) {
	return nil, nil
}
func (m *mockMCPStore) UpdateServer(ctx context.Context, id uuid.UUID, updates map[string]any) error {
	return nil
}
func (m *mockMCPStore) DeleteServer(ctx context.Context, id uuid.UUID) error           { return nil }
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
	result1, reason1 := gc.IsAllowed(ctx, agentID, userID, serverID, "any_tool")
	if !result1 {
		t.Errorf("expected allowed for accessible server, reason=%q", reason1)
	}
	if mockStore.callCount != 1 {
		t.Errorf("expected 1 DB call, got %d", mockStore.callCount)
	}

	// Second call — cache hit, no additional DB call
	result2, reason2 := gc.IsAllowed(ctx, agentID, userID, serverID, "another_tool")
	if !result2 {
		t.Errorf("expected allowed for accessible server (cached), reason=%q", reason2)
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

func TestStoreGrantChecker_BusInvalidationSurvivesOtherMCPSubscribers(t *testing.T) {
	initialServerID := uuid.New()
	newServerID := uuid.New()
	agentID := uuid.New()
	userID := "system"

	mockStore := &mockMCPStore{
		accessible: []store.MCPAccessInfo{
			{Server: store.MCPServerData{BaseModel: store.BaseModel{ID: initialServerID}, Name: "initial-server"}},
		},
	}

	msgBus := bus.New()
	gc := NewStoreGrantChecker(mockStore, msgBus)
	ctx := context.Background()

	if allowed, reason := gc.IsAllowed(ctx, agentID, userID, initialServerID, "tool"); !allowed {
		t.Fatalf("expected initial server allowed, reason=%q", reason)
	}
	if mockStore.callCount != 1 {
		t.Fatalf("expected 1 DB call after warm cache, got %d", mockStore.callCount)
	}

	// The gateway also subscribes to MCP cache events. Subscriber IDs must be
	// unique; otherwise this later subscription replaces the grant checker and
	// its stale cache survives grant changes.
	otherSubscriberCalled := false
	msgBus.Subscribe(bus.TopicCacheMCP, func(event bus.Event) {
		if event.Name == protocol.EventCacheInvalidate {
			otherSubscriberCalled = true
		}
	})

	mockStore.accessible = []store.MCPAccessInfo{
		{Server: store.MCPServerData{BaseModel: store.BaseModel{ID: initialServerID}, Name: "initial-server"}},
		{Server: store.MCPServerData{BaseModel: store.BaseModel{ID: newServerID}, Name: "new-server"}},
	}

	msgBus.Broadcast(bus.Event{
		Name:    protocol.EventCacheInvalidate,
		Payload: bus.CacheInvalidatePayload{Kind: bus.CacheKindMCP},
	})
	if !otherSubscriberCalled {
		t.Fatal("expected other MCP subscriber to receive invalidation")
	}

	if allowed, reason := gc.IsAllowed(ctx, agentID, userID, newServerID, "tool"); !allowed {
		t.Fatalf("expected grant checker cache to refresh after bus invalidation, reason=%q", reason)
	}
	if mockStore.callCount != 2 {
		t.Fatalf("expected cache miss after bus invalidation, got callCount=%d", mockStore.callCount)
	}
}

func TestStoreGrantChecker_DenyReasonSurfaced(t *testing.T) {
	serverID := uuid.New()
	agentID := uuid.New()
	userID := "test-user"

	mockStore := &mockMCPStore{
		accessible: []store.MCPAccessInfo{
			{
				Server:    store.MCPServerData{BaseModel: store.BaseModel{ID: serverID}, Name: "test-server"},
				ToolAllow: []string{"only_this"},
			},
		},
	}

	gc := NewStoreGrantChecker(mockStore, nil)
	ctx := context.Background()

	// Tool not in allow list — reason must identify why
	allowed, reason := gc.IsAllowed(ctx, agentID, userID, serverID, "blocked_tool")
	if allowed {
		t.Fatal("expected denied for tool not in allow list")
	}
	if reason != "tool_not_in_allow_list" {
		t.Errorf("expected reason=tool_not_in_allow_list, got %q", reason)
	}

	// Different server entirely — reason: server_not_accessible
	otherServer := uuid.New()
	allowed, reason = gc.IsAllowed(ctx, agentID, userID, otherServer, "anything")
	if allowed {
		t.Fatal("expected denied for ungranted server")
	}
	if reason != "server_not_accessible" {
		t.Errorf("expected reason=server_not_accessible, got %q", reason)
	}
}

// TestStoreGrantChecker_EmptyEntryNotCached verifies the self-healing guard
// that prevents permanent denial when ListAccessible transiently returns no
// rows. Without this guard, a single empty result pins denial on the (agent,
// user) pair until a bus invalidate event — which can be hours or never.
func TestStoreGrantChecker_EmptyEntryNotCached(t *testing.T) {
	serverID := uuid.New()
	agentID := uuid.New()
	userID := "system"

	mockStore := &mockMCPStore{accessible: nil} // simulate transient empty
	gc := NewStoreGrantChecker(mockStore, nil)
	ctx := context.Background()

	// First call hits the store and sees zero rows.
	allowed1, reason1 := gc.IsAllowed(ctx, agentID, userID, serverID, "tool")
	if allowed1 {
		t.Fatalf("expected denied on empty accessible, got allowed (reason=%q)", reason1)
	}
	if mockStore.callCount != 1 {
		t.Fatalf("expected 1 DB call on first miss, got %d", mockStore.callCount)
	}

	// Second call — if empty had been cached, callCount would stay at 1.
	// The guard forces a re-query so the user recovers when the underlying
	// transient condition clears.
	allowed2, _ := gc.IsAllowed(ctx, agentID, userID, serverID, "tool")
	if allowed2 {
		t.Fatal("expected denied while accessible is still empty")
	}
	if mockStore.callCount != 2 {
		t.Errorf("expected re-query on empty entry, got callCount=%d", mockStore.callCount)
	}

	// Flip the store to non-empty (simulates the transient condition clearing).
	mockStore.accessible = []store.MCPAccessInfo{
		{Server: store.MCPServerData{BaseModel: store.BaseModel{ID: serverID}, Name: "test"}},
	}
	allowed3, reason3 := gc.IsAllowed(ctx, agentID, userID, serverID, "tool")
	if !allowed3 {
		t.Errorf("expected allowed after store recovered, got denied (reason=%q)", reason3)
	}
	if mockStore.callCount != 3 {
		t.Errorf("expected fresh query after recovery, got callCount=%d", mockStore.callCount)
	}

	// After a successful non-empty load, the entry IS cached — subsequent
	// calls must NOT re-query (otherwise we lose the cache hit benefit).
	gc.IsAllowed(ctx, agentID, userID, serverID, "tool")
	if mockStore.callCount != 3 {
		t.Errorf("expected cache hit after successful load, got callCount=%d", mockStore.callCount)
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
	if allowed, _ := gc.IsAllowed(ctx, agentID, userID, serverID, "allowed_tool"); !allowed {
		t.Error("expected true for tool in allow list")
	}

	// Not allowed tool
	if allowed, _ := gc.IsAllowed(ctx, agentID, userID, serverID, "blocked_tool"); allowed {
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
	if allowed, _ := gc.IsAllowed(ctx, agentID, userID, serverID, "any_tool"); allowed {
		t.Error("expected false when no grant exists")
	}
}

func TestStoreGrantChecker_NilStore_AllowsAll(t *testing.T) {
	gc := NewStoreGrantChecker(nil, nil)
	ctx := context.Background()

	// Nil store = config-path mode, skip check
	if allowed, _ := gc.IsAllowed(ctx, uuid.New(), "user", uuid.New(), "any_tool"); !allowed {
		t.Error("expected true when store is nil (config-path mode)")
	}
}
