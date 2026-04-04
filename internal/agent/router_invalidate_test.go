package agent

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// TestInvalidateAgent_MatchesAgentKeyNotUUID verifies that InvalidateAgent(agentKey)
// clears entries cached under agentKey but NOT entries cached under UUID.
// This documents the root cause of the stale-cache bug: heartbeat/cron paths
// previously used UUID in session keys, creating cache entries keyed by UUID
// that InvalidateAgent(agentKey) could never match.
func TestInvalidateAgent_MatchesAgentKeyNotUUID(t *testing.T) {
	r := NewRouter()
	agentKey := "my-agent"
	agentUUID := uuid.New().String()
	tenantID := uuid.New()

	// Simulate chat path: cache entry keyed by agentKey
	ctx := store.WithTenantID(context.Background(), tenantID)
	chatKey := agentCacheKey(ctx, agentKey)
	r.agents[chatKey] = &agentEntry{}

	// Simulate old heartbeat path: cache entry keyed by UUID (the bug)
	uuidKey := agentCacheKey(ctx, agentUUID)
	r.agents[uuidKey] = &agentEntry{}

	// InvalidateAgent with agentKey should clear agentKey entry
	r.InvalidateAgent(agentKey)

	if _, ok := r.agents[chatKey]; ok {
		t.Error("InvalidateAgent(agentKey) should have cleared the agentKey-based cache entry")
	}

	// UUID entry is NOT cleared — this is why the bug existed
	if _, ok := r.agents[uuidKey]; !ok {
		t.Error("InvalidateAgent(agentKey) should NOT match UUID-based entries (documenting the bug)")
	}

	// InvalidateAgent with UUID clears the UUID entry (belt-and-suspenders fix)
	r.InvalidateAgent(agentUUID)
	if _, ok := r.agents[uuidKey]; ok {
		t.Error("InvalidateAgent(UUID) should have cleared the UUID-based cache entry")
	}
}

// TestInvalidateAgent_TenantScoped verifies that InvalidateAgent clears entries
// across all tenants for the same agentKey (suffix match).
func TestInvalidateAgent_TenantScoped(t *testing.T) {
	r := NewRouter()
	agentKey := "default"
	tenantA := uuid.New()
	tenantB := uuid.New()

	ctxA := store.WithTenantID(context.Background(), tenantA)
	ctxB := store.WithTenantID(context.Background(), tenantB)

	keyA := agentCacheKey(ctxA, agentKey)
	keyB := agentCacheKey(ctxB, agentKey)
	r.agents[keyA] = &agentEntry{}
	r.agents[keyB] = &agentEntry{}

	r.InvalidateAgent(agentKey)

	if len(r.agents) != 0 {
		t.Errorf("InvalidateAgent should clear all tenant-scoped entries, got %d remaining", len(r.agents))
	}
}
