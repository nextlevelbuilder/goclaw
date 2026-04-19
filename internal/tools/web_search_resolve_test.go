package tools

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// TestResolveChain_CacheHit verifies that resolveChain returns cached chains.
// Scenario 7: back-to-back same-tenant calls return the same cached slice.
func TestResolveChain_CacheHit(t *testing.T) {
	fake := newFakeSecretsStore()
	tool := &WebSearchTool{
		secrets:    fake,
		cache:      newWebCache(defaultCacheMaxEntries, defaultCacheTTL),
		chainCache: newTenantChainCache(),
	}

	tid := uuid.New()
	ctx := context.Background()
	ctx = store.WithTenantID(ctx, tid)

	// Set a provider key
	fake.Set(ctx, "tools.web.brave.api_key", "test-key-brave")

	// First call → resolves and caches
	chain1 := tool.resolveChain(ctx)

	// Second call → should hit cache
	chain2 := tool.resolveChain(ctx)

	if len(chain1) == 0 || len(chain2) == 0 {
		t.Fatalf("expected non-empty chains, got %d and %d", len(chain1), len(chain2))
	}

	// Both should have same provider order
	if len(chain1) != len(chain2) {
		t.Errorf("chain length mismatch: %d vs %d", len(chain1), len(chain2))
	}

	for i := 0; i < len(chain1); i++ {
		if chain1[i].Name() != chain2[i].Name() {
			t.Errorf("provider %d: %s vs %s", i, chain1[i].Name(), chain2[i].Name())
		}
	}
}

// TestResolveChain_TenantIsolation verifies that two tenants get independent chains.
// Scenario 8: switching tenant context yields different chains.
func TestResolveChain_TenantIsolation(t *testing.T) {
	fake := newFakeSecretsStore()
	tool := &WebSearchTool{
		secrets:    fake,
		cache:      newWebCache(defaultCacheMaxEntries, defaultCacheTTL),
		chainCache: newTenantChainCache(),
	}

	tenantA := uuid.New()
	tenantB := uuid.New()

	// Setup tenant A: Brave key only
	ctxA := context.Background()
	ctxA = store.WithTenantID(ctxA, tenantA)
	fake.Set(ctxA, "tools.web.brave.api_key", "test-key-brave-A")

	// Setup tenant B: Exa key only
	ctxB := context.Background()
	ctxB = store.WithTenantID(ctxB, tenantB)
	fake.Set(ctxB, "tools.web.exa.api_key", "test-key-exa-B")

	// Resolve chains for each tenant
	chainA := tool.resolveChain(ctxA)
	chainB := tool.resolveChain(ctxB)

	// Tenant A should have Brave
	foundBraveInA := false
	for _, p := range chainA {
		if p.Name() == "brave" {
			foundBraveInA = true
			break
		}
	}
	if !foundBraveInA {
		t.Error("tenant A chain missing Brave")
	}

	// Tenant B should have Exa
	foundExaInB := false
	for _, p := range chainB {
		if p.Name() == "exa" {
			foundExaInB = true
			break
		}
	}
	if !foundExaInB {
		t.Error("tenant B chain missing Exa")
	}

	// Tenant A should NOT have Exa
	foundExaInA := false
	for _, p := range chainA {
		if p.Name() == "exa" {
			foundExaInA = true
			break
		}
	}
	if foundExaInA {
		t.Error("tenant A chain should not have Exa")
	}

	// Tenant B should NOT have Brave
	foundBraveInB := false
	for _, p := range chainB {
		if p.Name() == "brave" {
			foundBraveInB = true
			break
		}
	}
	if foundBraveInB {
		t.Error("tenant B chain should not have Brave")
	}
}

// TestResolveChain_CacheInvalidation verifies cache invalidation refreshes the chain.
func TestResolveChain_CacheInvalidation(t *testing.T) {
	fake := newFakeSecretsStore()
	tool := &WebSearchTool{
		secrets:    fake,
		cache:      newWebCache(defaultCacheMaxEntries, defaultCacheTTL),
		chainCache: newTenantChainCache(),
	}

	tid := uuid.New()
	ctx := context.Background()
	ctx = store.WithTenantID(ctx, tid)

	// Initial state: only DDG
	chain1 := tool.resolveChain(ctx)
	if len(chain1) != 1 || chain1[0].Name() != "duckduckgo" {
		t.Fatalf("initial chain should be DDG only, got %v", chainNames(chain1))
	}

	// Add a provider key and invalidate cache
	fake.Set(ctx, "tools.web.brave.api_key", "test-key-brave-new")
	tool.chainCache.Invalidate(tid)

	// Next resolve should pick up the new key
	chain2 := tool.resolveChain(ctx)
	if len(chain2) != 2 {
		t.Errorf("after invalidation, expected 2 providers, got %d", len(chain2))
	}

	foundBrave := false
	for _, p := range chain2 {
		if p.Name() == "brave" {
			foundBrave = true
			break
		}
	}
	if !foundBrave {
		t.Error("after invalidation, Brave should be in chain")
	}
}

// TestResolveChain_TTLCacheExpiry verifies that expired cache entries trigger refresh.
func TestResolveChain_TTLCacheExpiry(t *testing.T) {
	fake := newFakeSecretsStore()
	tool := &WebSearchTool{
		secrets:    fake,
		cache:      newWebCache(defaultCacheMaxEntries, defaultCacheTTL),
		chainCache: newTenantChainCache(),
	}

	tid := uuid.New()
	ctx := context.Background()
	ctx = store.WithTenantID(ctx, tid)

	// Initial state: DDG only
	chain1 := tool.resolveChain(ctx)
	if len(chain1) != 1 {
		t.Fatalf("initial chain should be 1 provider, got %d", len(chain1))
	}

	// Manually expire the cache entry
	tool.chainCache.entries[tid] = tenantChainEntry{
		chain:   chain1,
		expires: time.Now().Add(-1 * time.Second), // expired
	}

	// Add provider and resolve again — should bypass expired cache
	fake.Set(ctx, "tools.web.brave.api_key", "test-key-brave-after-ttl")
	chain2 := tool.resolveChain(ctx)

	if len(chain2) != 2 {
		t.Errorf("after TTL expiry, expected 2 providers, got %d", len(chain2))
	}
}
