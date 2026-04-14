package agent

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// TestInvalidateTenant_OnlyAffectsMatchingTenant verifies that
// Router.InvalidateTenant deletes only cache entries whose key is prefixed
// with the target tenant's UUID, leaving other tenants and bare
// (non-tenant) entries untouched.
func TestInvalidateTenant_OnlyAffectsMatchingTenant(t *testing.T) {
	r := NewRouter()

	tenantA := uuid.New()
	tenantB := uuid.New()

	ctxA := store.WithTenantID(context.Background(), tenantA)
	ctxB := store.WithTenantID(context.Background(), tenantB)

	// Entries for three scopes: tenantA, tenantB, bare (no tenant).
	keyA1 := agentCacheKey(ctxA, "agent-1")
	keyA2 := agentCacheKey(ctxA, "agent-2")
	keyB1 := agentCacheKey(ctxB, "agent-1")
	keyBare := agentCacheKey(context.Background(), "agent-1")

	r.agents[keyA1] = &agentEntry{}
	r.agents[keyA2] = &agentEntry{}
	r.agents[keyB1] = &agentEntry{}
	r.agents[keyBare] = &agentEntry{}

	r.InvalidateTenant(tenantA)

	if _, ok := r.agents[keyA1]; ok {
		t.Error("tenantA entry agent-1 should have been invalidated")
	}
	if _, ok := r.agents[keyA2]; ok {
		t.Error("tenantA entry agent-2 should have been invalidated")
	}
	if _, ok := r.agents[keyB1]; !ok {
		t.Error("tenantB entry must survive tenantA invalidation")
	}
	if _, ok := r.agents[keyBare]; !ok {
		t.Error("bare (non-tenant) entry must survive tenantA invalidation")
	}
}

// TestInvalidateTenant_NilIsNoop verifies the uuid.Nil guard — passing Nil
// must not wipe any entries (callers use InvalidateAll for global wipes).
func TestInvalidateTenant_NilIsNoop(t *testing.T) {
	r := NewRouter()
	tenantA := uuid.New()

	ctxA := store.WithTenantID(context.Background(), tenantA)
	keyA := agentCacheKey(ctxA, "agent-1")
	keyBare := agentCacheKey(context.Background(), "agent-1")

	r.agents[keyA] = &agentEntry{}
	r.agents[keyBare] = &agentEntry{}

	r.InvalidateTenant(uuid.Nil)

	if len(r.agents) != 2 {
		t.Errorf("InvalidateTenant(uuid.Nil) must be a no-op, got %d entries remaining (want 2)", len(r.agents))
	}
}

// TestInvalidateTenant_SubstringSafety verifies that a tenant with a UUID
// that is a prefix (by string) of another tenant's key segment cannot
// accidentally wipe the other. UUIDs are fixed-length so this can only
// happen if matching is purely substring-based. The ":" suffix in the
// prefix guards against it.
func TestInvalidateTenant_SubstringSafety(t *testing.T) {
	r := NewRouter()

	// Same UUID used in two roles — as a tenant key prefix and inside an
	// agent key. If the matcher incorrectly matched anywhere in the key,
	// InvalidateTenant(tenantA) would wipe the second entry too.
	tenantA := uuid.New()
	tenantB := uuid.New()

	ctxA := store.WithTenantID(context.Background(), tenantA)
	ctxB := store.WithTenantID(context.Background(), tenantB)

	keyA := agentCacheKey(ctxA, "agent-1")
	// Craft a pathological key under tenantB whose agent name contains
	// tenantA's UUID. Should NOT be deleted.
	keyB := agentCacheKey(ctxB, "agent-"+tenantA.String())

	r.agents[keyA] = &agentEntry{}
	r.agents[keyB] = &agentEntry{}

	r.InvalidateTenant(tenantA)

	if _, ok := r.agents[keyA]; ok {
		t.Error("tenantA key should have been deleted")
	}
	if _, ok := r.agents[keyB]; !ok {
		t.Error("tenantB key containing tenantA UUID in agent name must NOT be deleted")
	}
}
