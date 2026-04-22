package agent

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// stubAgent is a minimal Agent implementation for router tests.
// ID() returns a fixed agent_key and IsRunning() tracks a bool.
type stubAgent struct {
	id      string
	running bool
}

func (s *stubAgent) ID() string                                          { return s.id }
func (s *stubAgent) UUID() uuid.UUID                                     { return uuid.Nil }
func (s *stubAgent) OtherConfig() json.RawMessage                        { return nil }
func (s *stubAgent) Run(context.Context, RunRequest) (*RunResult, error) { return nil, nil }
func (s *stubAgent) IsRunning() bool                                     { return s.running }
func (s *stubAgent) Model() string                                       { return "test-model" }
func (s *stubAgent) ProviderName() string                                { return "test" }
func (s *stubAgent) Provider() providers.Provider                        { return nil }

// stubResolver builds a ResolverFunc that returns a stubAgent with a
// predetermined ID. If idByInput is set, the returned agent's ID is derived
// from the input (used for dual-tenant tests where each tenant has a distinct
// UUID but the same agent_key).
func stubResolver(agentKey string) ResolverFunc {
	return func(_ context.Context, _ string) (Agent, error) {
		return &stubAgent{id: agentKey}, nil
	}
}

// TestRouterGet_UUIDInputStoresCanonicalKey verifies that when the caller
// passes a UUID-like string to Get(), the cache entry lands under
// tenantID:agentKey (canonical), NOT tenantID:uuidStr. Exercises the
// canonicalization path via a real resolver call.
func TestRouterGet_UUIDInputStoresCanonicalKey(t *testing.T) {
	r := NewRouter()
	r.SetResolver(stubResolver("goctech-leader"))

	tenantID := uuid.New()
	ctx := store.WithTenantID(context.Background(), tenantID)
	agentUUID := uuid.New().String()

	ag, err := r.Get(ctx, agentUUID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if ag.ID() != "goctech-leader" {
		t.Fatalf("ag.ID() = %q, want %q", ag.ID(), "goctech-leader")
	}

	canonical := tenantID.String() + ":goctech-leader"
	nonCanonical := tenantID.String() + ":" + agentUUID

	r.mu.RLock()
	_, canonicalExists := r.agents[canonical]
	_, nonCanonicalExists := r.agents[nonCanonical]
	r.mu.RUnlock()

	if !canonicalExists {
		t.Errorf("expected canonical key %q in cache", canonical)
	}
	if nonCanonicalExists {
		t.Errorf("unexpected non-canonical key %q in cache", nonCanonical)
	}
}

// TestRouterGet_IdempotentCacheUnderKeyOrUUID verifies that calling Get()
// first with agent_key then with UUID produces exactly ONE cache entry — the
// canonical tenantID:agent_key.
func TestRouterGet_IdempotentCacheUnderKeyOrUUID(t *testing.T) {
	r := NewRouter()
	var resolveCount atomic.Int32
	r.SetResolver(func(_ context.Context, _ string) (Agent, error) {
		resolveCount.Add(1)
		return &stubAgent{id: "my-agent"}, nil
	})

	tenantID := uuid.New()
	ctx := store.WithTenantID(context.Background(), tenantID)

	if _, err := r.Get(ctx, "my-agent"); err != nil {
		t.Fatalf("Get(agent_key): %v", err)
	}
	if _, err := r.Get(ctx, uuid.New().String()); err != nil {
		t.Fatalf("Get(uuid): %v", err)
	}

	r.mu.RLock()
	total := 0
	for k := range r.agents {
		if strings.HasPrefix(k, tenantID.String()+":") {
			total++
		}
	}
	r.mu.RUnlock()

	if total != 1 {
		t.Errorf("expected exactly 1 tenant-scoped cache entry, got %d", total)
	}
}

// TestRouterGet_UUIDCallerResolvesEveryTime documents the honest cost of
// canonicalization: a caller that keeps passing the UUID form never hits the
// canonical key on read, so the resolver runs on every call. Pin this
// behavior so future refactors don't pretend the cache covers UUID inputs.
func TestRouterGet_UUIDCallerResolvesEveryTime(t *testing.T) {
	r := NewRouter()
	var resolveCount atomic.Int32
	r.SetResolver(func(_ context.Context, _ string) (Agent, error) {
		resolveCount.Add(1)
		return &stubAgent{id: "fixed-key"}, nil
	})

	tenantID := uuid.New()
	ctx := store.WithTenantID(context.Background(), tenantID)

	uuidStr := uuid.New().String()
	for range 3 {
		if _, err := r.Get(ctx, uuidStr); err != nil {
			t.Fatalf("Get: %v", err)
		}
	}

	// Each call must miss the raw uuidStr key and fall through to resolver,
	// because Get writes to the CANONICAL key, not the raw input key.
	if got := resolveCount.Load(); got != 3 {
		t.Errorf("expected resolver to run 3 times (UUID caller is un-cached), got %d", got)
	}
}

// TestRouterGet_DualTenantSameAgentKey — staging MCP finding.
// `tieu-ho` exists in both Master and Việt Org tenants with different UUIDs.
// Verify the router stores independent entries per tenant and invalidation
// on one tenant does not affect the other.
func TestRouterGet_DualTenantSameAgentKey(t *testing.T) {
	r := NewRouter()
	r.SetResolver(stubResolver("tieu-ho"))

	tenantA := uuid.New()
	tenantB := uuid.New()
	ctxA := store.WithTenantID(context.Background(), tenantA)
	ctxB := store.WithTenantID(context.Background(), tenantB)

	if _, err := r.Get(ctxA, "tieu-ho"); err != nil {
		t.Fatalf("Get tenantA: %v", err)
	}
	if _, err := r.Get(ctxB, "tieu-ho"); err != nil {
		t.Fatalf("Get tenantB: %v", err)
	}

	keyA := tenantA.String() + ":tieu-ho"
	keyB := tenantB.String() + ":tieu-ho"

	r.mu.RLock()
	_, existsA := r.agents[keyA]
	_, existsB := r.agents[keyB]
	r.mu.RUnlock()

	if !existsA {
		t.Errorf("tenantA cache entry %q missing", keyA)
	}
	if !existsB {
		t.Errorf("tenantB cache entry %q missing", keyB)
	}
}

// TestRouterGet_StaleRawUUIDEntryEvictedOnGet pins that a pre-hardening
// fragmented entry written under a raw UUID key still goes through the
// TTL-based eviction branch when a UUID-form caller arrives after TTL expiry.
// Scenario: an earlier build that did not canonicalize wrote the entry at
// `tenantID:<uuidStr>`, the new build loads that state (synthesized here via
// direct map write), TTL expires, and a UUID caller with the same UUID must
// evict + re-resolve + write the canonical `tenantID:agentKey` entry, leaving
// no fragmented entries behind.
func TestRouterGet_StaleRawUUIDEntryEvictedOnGet(t *testing.T) {
	r := NewRouter()

	var resolveCount atomic.Int32
	r.SetResolver(func(_ context.Context, _ string) (Agent, error) {
		resolveCount.Add(1)
		return &stubAgent{id: "fresh-agent"}, nil
	})

	tenantID := uuid.New()
	ctx := store.WithTenantID(context.Background(), tenantID)
	uuidStr := uuid.New().String()
	rawKey := tenantID.String() + ":" + uuidStr
	canonicalKey := tenantID.String() + ":fresh-agent"

	// Synthesize the pre-hardening fragmented entry directly. Stale cachedAt
	// forces the initial-miss eviction path.
	r.mu.Lock()
	r.agents[rawKey] = &agentEntry{
		agent:    &stubAgent{id: "fresh-agent"},
		cachedAt: time.Now().Add(-2 * defaultRouterTTL),
	}
	r.mu.Unlock()

	if _, err := r.Get(ctx, uuidStr); err != nil {
		t.Fatalf("Get(uuid) with fragmented raw-UUID entry: %v", err)
	}
	if resolveCount.Load() != 1 {
		t.Errorf("resolver calls = %d, want 1 (raw-UUID entry should have been evicted as stale)", resolveCount.Load())
	}

	r.mu.RLock()
	_, rawExists := r.agents[rawKey]
	_, canonicalExists := r.agents[canonicalKey]
	r.mu.RUnlock()

	if rawExists {
		t.Errorf("fragmented raw-UUID entry %q still in cache after TTL eviction", rawKey)
	}
	if !canonicalExists {
		t.Errorf("canonical entry %q missing after resolver rewrite", canonicalKey)
	}
}

// TestRouterGet_CanonicalDoubleCheckEvictsStaleEntry pins the TTL check inside
// the canonical double-check branch. Scenario: an earlier caller wrote the
// canonical entry, TTL expires, a UUID-form caller arrives. The raw UUID key
// is not in the map so the initial-miss eviction branch does nothing; the
// resolver runs and the canonical double-check finds the stale entry. Without
// the TTL re-check this would return the stale agent indefinitely — only
// InvalidateAgent could rescue it. The fix evicts + rewrites.
func TestRouterGet_CanonicalDoubleCheckEvictsStaleEntry(t *testing.T) {
	r := NewRouter()

	var resolveCount atomic.Int32
	calls := 0
	r.SetResolver(func(_ context.Context, _ string) (Agent, error) {
		resolveCount.Add(1)
		calls++
		return &stubAgent{id: "goctech-leader", running: calls == 1}, nil
	})

	tenantID := uuid.New()
	ctx := store.WithTenantID(context.Background(), tenantID)
	canonicalKey := tenantID.String() + ":goctech-leader"

	// Prime canonical entry with agent_key caller.
	if _, err := r.Get(ctx, "goctech-leader"); err != nil {
		t.Fatalf("initial Get(agent_key): %v", err)
	}
	if got := resolveCount.Load(); got != 1 {
		t.Fatalf("resolver calls after prime = %d, want 1", got)
	}

	// Force the canonical entry to look stale (cachedAt far past TTL).
	r.mu.Lock()
	entry, ok := r.agents[canonicalKey]
	if !ok {
		r.mu.Unlock()
		t.Fatalf("canonical entry %q missing after prime", canonicalKey)
	}
	entry.cachedAt = time.Now().Add(-2 * defaultRouterTTL)
	r.mu.Unlock()

	// UUID-form caller arrives. Initial miss branch does nothing (raw UUID key
	// was never written). Resolver runs. Canonical double-check must detect the
	// stale entry and evict+rewrite, not return it.
	uuidInput := uuid.New().String()
	got, err := r.Get(ctx, uuidInput)
	if err != nil {
		t.Fatalf("Get(uuid) after stale prime: %v", err)
	}
	if resolveCount.Load() != 2 {
		t.Errorf("resolver calls after stale Get = %d, want 2", resolveCount.Load())
	}
	if got.IsRunning() {
		// The stale entry had running=true (calls==1). The fresh resolver return
		// has running=false (calls==2). If IsRunning is true we got the stale one.
		t.Errorf("Get returned stale entry (running=true) instead of fresh resolver result")
	}

	// Canonical entry must now hold a fresh cachedAt.
	r.mu.RLock()
	fresh, ok := r.agents[canonicalKey]
	r.mu.RUnlock()
	if !ok {
		t.Fatalf("canonical entry %q missing after stale refresh", canonicalKey)
	}
	if time.Since(fresh.cachedAt) > time.Second {
		t.Errorf("canonical entry cachedAt not refreshed: age = %v", time.Since(fresh.cachedAt))
	}
}
