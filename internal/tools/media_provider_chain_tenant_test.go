package tools

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// Phase 6 verification: proves the 4-tier overlay (built in Phase 3) flows
// end-to-end through ResolveMediaProviderChain without any tool-side code
// change. Tenant-layer settings must override global-layer settings, and
// per-agent override (arg) must still beat both.

// chainJSON builds a media provider chain settings blob for the given
// provider name. Single-enabled entry keeps the test assertions terse.
func chainJSON(provider string) []byte {
	return []byte(`{"providers":[{"provider":"` + provider + `","model":"m","enabled":true}]}`)
}

// TestResolveMediaProviderChain_TenantOverrideWins verifies tier-2 tenant
// bytes take precedence over tier-3 global bytes when both are set for
// the same tool name. This is the core Phase 6 promise.
func TestResolveMediaProviderChain_TenantOverrideWins(t *testing.T) {
	global := BuiltinToolSettings{"create_image": chainJSON("global_provider")}
	tenant := BuiltinToolSettings{"create_image": chainJSON("tenant_provider")}

	ctx := WithBuiltinToolSettings(context.Background(), global)
	ctx = WithTenantToolSettings(ctx, tenant)

	chain := ResolveMediaProviderChain(
		ctx, "create_image",
		"", "", // no per-agent override
		[]string{"fallback"}, // hardcoded default priority
		map[string]string{},
		providers.NewRegistry(nil),
	)

	if len(chain) != 1 {
		t.Fatalf("chain len = %d, want 1 (%v)", len(chain), chain)
	}
	if chain[0].Provider != "tenant_provider" {
		t.Errorf("chain[0].Provider = %q, want tenant_provider (tenant layer must beat global)", chain[0].Provider)
	}
}

// TestResolveMediaProviderChain_PerAgentArgBeatsTenant verifies the
// function-argument per-agent override (passed directly, not via ctx) still
// wins over tenant ctx settings. This is the top of the precedence ladder.
func TestResolveMediaProviderChain_PerAgentArgBeatsTenant(t *testing.T) {
	tenant := BuiltinToolSettings{"create_image": chainJSON("tenant_provider")}
	ctx := WithTenantToolSettings(context.Background(), tenant)

	chain := ResolveMediaProviderChain(
		ctx, "create_image",
		"per_agent_provider", "per_agent_model", // per-agent arg wins
		[]string{"fallback"},
		map[string]string{},
		providers.NewRegistry(nil),
	)

	if len(chain) != 1 {
		t.Fatalf("chain len = %d, want 1", len(chain))
	}
	if chain[0].Provider != "per_agent_provider" {
		t.Errorf("chain[0].Provider = %q, want per_agent_provider", chain[0].Provider)
	}
}

// TestResolveMediaProviderChain_FallsBackToGlobalWhenNoTenant verifies
// that with only the global tier set (no tenant ctx key), the function
// returns the global chain. Backward-compat regression guard.
func TestResolveMediaProviderChain_FallsBackToGlobalWhenNoTenant(t *testing.T) {
	global := BuiltinToolSettings{"create_image": chainJSON("global_provider")}
	ctx := WithBuiltinToolSettings(context.Background(), global)

	chain := ResolveMediaProviderChain(
		ctx, "create_image",
		"", "",
		[]string{"fallback"},
		map[string]string{},
		providers.NewRegistry(nil),
	)

	if len(chain) != 1 || chain[0].Provider != "global_provider" {
		t.Errorf("chain = %v, want single entry 'global_provider'", chain)
	}
}

// TestResolveMediaProviderChain_TenantOnly verifies tenant-layer-only
// config (no global row) still surfaces. Critical for tenants that want
// to opt into a provider that's disabled by default.
func TestResolveMediaProviderChain_TenantOnly(t *testing.T) {
	tenant := BuiltinToolSettings{"create_image": chainJSON("tenant_provider")}
	ctx := WithTenantToolSettings(context.Background(), tenant)

	chain := ResolveMediaProviderChain(
		ctx, "create_image",
		"", "",
		[]string{"fallback"},
		map[string]string{},
		providers.NewRegistry(nil),
	)

	if len(chain) != 1 || chain[0].Provider != "tenant_provider" {
		t.Errorf("chain = %v, want single entry 'tenant_provider'", chain)
	}
}

// TestResolveMediaProviderChain_DifferentTools_Independent verifies that
// tenant settings for one tool (create_image) don't leak into another
// tool (create_audio) — merge happens at tool-name level.
func TestResolveMediaProviderChain_DifferentTools_Independent(t *testing.T) {
	tenant := BuiltinToolSettings{"create_image": chainJSON("image_tenant")}
	ctx := WithTenantToolSettings(context.Background(), tenant)

	// Query a different tool — tenant has no entry for it.
	chain := ResolveMediaProviderChain(
		ctx, "create_audio",
		"", "",
		[]string{}, // empty priority — nothing to build
		map[string]string{},
		providers.NewRegistry(nil),
	)

	// No per-agent, no matching tenant key, no global, no hardcoded → empty chain.
	if len(chain) != 0 {
		t.Errorf("create_audio chain = %v, want empty (no cross-tool leak)", chain)
	}
}
