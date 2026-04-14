package agent

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// TestIsRunning_TenantScopedLookup asserts that Router.IsRunning accepts ctx
// and looks up under the tenant-scoped cache key. A previous bare
// `r.agents[agentID]` lookup always returned false for any tenant-scoped
// deployment, so the WS `agents.list` response incorrectly showed every live
// agent as `isRunning: false`.
func TestIsRunning_TenantScopedLookup(t *testing.T) {
	r := NewRouter()
	tenantA := uuid.New()
	tenantB := uuid.New()

	ctxA := store.WithTenantID(context.Background(), tenantA)
	ctxB := store.WithTenantID(context.Background(), tenantB)

	// Directly populate cache under canonical tenant-scoped key with a
	// running stub agent in tenant A.
	keyA := agentCacheKey(ctxA, "foo")
	r.agents[keyA] = &agentEntry{
		agent:    &stubAgent{id: "foo", running: true},
		cachedAt: time.Now(),
	}

	if !r.IsRunning(ctxA, "foo") {
		t.Error("IsRunning(ctxA, foo) should return true — agent cached under tenantA")
	}
	if r.IsRunning(ctxB, "foo") {
		t.Error("IsRunning(ctxB, foo) should return false — tenantB has no entry")
	}

	// Empty ctx → no tenant → bare key lookup. Should still return false
	// because the actual entry is under a tenant-scoped key.
	if r.IsRunning(context.Background(), "foo") {
		t.Error("IsRunning(emptyCtx, foo) should return false — bare lookup cannot see tenant-scoped entries")
	}
}

// TestIsRunning_NoBareLookupLeak guards against regressing to the pre-fix
// bare-key lookup which could surface a cross-tenant leak for
// non-tenant-scoped routers.
func TestIsRunning_NoBareLookupLeak(t *testing.T) {
	r := NewRouter()

	// Bare entry (no tenant) with running=true.
	r.agents["bare-agent"] = &agentEntry{
		agent:    &stubAgent{id: "bare-agent", running: true},
		cachedAt: time.Now(),
	}

	// Without a tenant, ctx-scoped lookup yields the bare key directly.
	if !r.IsRunning(context.Background(), "bare-agent") {
		t.Error("IsRunning should find a bare entry when ctx has no tenant")
	}

	// WITH a tenant, the cache key becomes `tenant:bare-agent` which does
	// NOT exist — must return false (no fallback to bare lookup).
	tenantID := uuid.New()
	ctxT := store.WithTenantID(context.Background(), tenantID)
	if r.IsRunning(ctxT, "bare-agent") {
		t.Error("IsRunning must NOT fall back to bare lookup when tenant scope is present — cross-tenant leak")
	}
}
