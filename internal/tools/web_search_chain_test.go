package tools

import (
	"context"
	"testing"
)

// fakeSearchProvider implements SearchProvider for chain-resolution tests.
// Only Name() is exercised — Search is never called.
type fakeSearchProvider struct{ name string }

func (f *fakeSearchProvider) Name() string { return f.name }
func (f *fakeSearchProvider) Search(_ context.Context, _ searchParams) ([]searchResult, error) {
	return nil, nil
}

// defaultChain builds a deterministic default provider list used across tests.
func defaultChain() []SearchProvider {
	return []SearchProvider{
		&fakeSearchProvider{name: "brave"},
		&fakeSearchProvider{name: "duckduckgo"},
	}
}

// chainNames extracts provider names in order for terse assertions.
func chainNames(chain []SearchProvider) []string {
	out := make([]string, 0, len(chain))
	for _, p := range chain {
		out = append(out, p.Name())
	}
	return out
}

// ---- No override → defaults unchanged ----

func TestResolveWebSearchChain_NoOverride_ReturnsDefaults(t *testing.T) {
	got := ResolveWebSearchChain(context.Background(), defaultChain())
	if len(got) != 2 || got[0].Name() != "brave" || got[1].Name() != "duckduckgo" {
		t.Errorf("no override: got %v, want [brave duckduckgo]", chainNames(got))
	}
}

// Empty defaults → nil result.
func TestResolveWebSearchChain_EmptyDefaults_ReturnsNil(t *testing.T) {
	got := ResolveWebSearchChain(context.Background(), nil)
	if got != nil {
		t.Errorf("empty defaults: got %v, want nil", got)
	}
}

// ---- provider_order reorders ----

func TestResolveWebSearchChain_ProviderOrderReorders(t *testing.T) {
	override := []byte(`{"provider_order":["duckduckgo","brave"]}`)
	tenant := BuiltinToolSettings{"web_search": override}
	ctx := WithTenantToolSettings(context.Background(), tenant)

	got := chainNames(ResolveWebSearchChain(ctx, defaultChain()))
	want := []string{"duckduckgo", "brave"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("reorder: got %v, want %v", got, want)
	}
}

// provider_order including only one provider → chain has only that provider.
func TestResolveWebSearchChain_ProviderOrderFiltersOut(t *testing.T) {
	override := []byte(`{"provider_order":["duckduckgo"]}`)
	tenant := BuiltinToolSettings{"web_search": override}
	ctx := WithTenantToolSettings(context.Background(), tenant)

	got := chainNames(ResolveWebSearchChain(ctx, defaultChain()))
	if len(got) != 1 || got[0] != "duckduckgo" {
		t.Errorf("filter: got %v, want [duckduckgo]", got)
	}
}

// Unknown providers in provider_order are skipped (no injection path).
func TestResolveWebSearchChain_ProviderOrderIgnoresUnknown(t *testing.T) {
	override := []byte(`{"provider_order":["malicious_new_provider","brave"]}`)
	tenant := BuiltinToolSettings{"web_search": override}
	ctx := WithTenantToolSettings(context.Background(), tenant)

	got := chainNames(ResolveWebSearchChain(ctx, defaultChain()))
	if len(got) != 1 || got[0] != "brave" {
		t.Errorf("unknown skip: got %v, want [brave]", got)
	}
}

// ---- per-provider enabled flag ----

func TestResolveWebSearchChain_DisablesProviderViaEnabledFalse(t *testing.T) {
	override := []byte(`{"brave":{"enabled":false}}`)
	tenant := BuiltinToolSettings{"web_search": override}
	ctx := WithTenantToolSettings(context.Background(), tenant)

	got := chainNames(ResolveWebSearchChain(ctx, defaultChain()))
	if len(got) != 1 || got[0] != "duckduckgo" {
		t.Errorf("disable brave: got %v, want [duckduckgo]", got)
	}
}

// enabled=true is a no-op (default is always "enabled" by virtue of being in defaults).
func TestResolveWebSearchChain_EnabledTrueIsNoOp(t *testing.T) {
	override := []byte(`{"brave":{"enabled":true},"duckduckgo":{"enabled":true}}`)
	tenant := BuiltinToolSettings{"web_search": override}
	ctx := WithTenantToolSettings(context.Background(), tenant)

	got := chainNames(ResolveWebSearchChain(ctx, defaultChain()))
	if len(got) != 2 {
		t.Errorf("enabled=true no-op: got %v, want 2 providers", got)
	}
}

// ---- malformed JSON → falls back to defaults ----

func TestResolveWebSearchChain_MalformedJSON_FallsBackToDefaults(t *testing.T) {
	override := []byte(`{not valid json`)
	tenant := BuiltinToolSettings{"web_search": override}
	ctx := WithTenantToolSettings(context.Background(), tenant)

	got := chainNames(ResolveWebSearchChain(ctx, defaultChain()))
	if len(got) != 2 {
		t.Errorf("malformed: got %v, want defaults", got)
	}
}

// ---- combined: provider_order + disabled provider in the order ----

func TestResolveWebSearchChain_OrderAndDisableCombined(t *testing.T) {
	override := []byte(`{"provider_order":["brave","duckduckgo"],"brave":{"enabled":false}}`)
	tenant := BuiltinToolSettings{"web_search": override}
	ctx := WithTenantToolSettings(context.Background(), tenant)

	got := chainNames(ResolveWebSearchChain(ctx, defaultChain()))
	// brave is in the order but disabled → skipped; duckduckgo survives.
	if len(got) != 1 || got[0] != "duckduckgo" {
		t.Errorf("order+disable: got %v, want [duckduckgo]", got)
	}
}

// ---- global layer (WithBuiltinToolSettings) also drives the override ----
// Ensures the merged view from BuiltinToolSettingsFromCtx surfaces global-layer
// web_search settings just like tenant-layer.
func TestResolveWebSearchChain_GlobalLayerDrivesOverride(t *testing.T) {
	override := []byte(`{"provider_order":["duckduckgo"]}`)
	global := BuiltinToolSettings{"web_search": override}
	ctx := WithBuiltinToolSettings(context.Background(), global)

	got := chainNames(ResolveWebSearchChain(ctx, defaultChain()))
	if len(got) != 1 || got[0] != "duckduckgo" {
		t.Errorf("global layer: got %v, want [duckduckgo]", got)
	}
}

// Tenant layer beats global for the same tool (regression for Phase 3 precedence).
func TestResolveWebSearchChain_TenantBeatsGlobal(t *testing.T) {
	global := BuiltinToolSettings{"web_search": []byte(`{"provider_order":["brave"]}`)}
	tenant := BuiltinToolSettings{"web_search": []byte(`{"provider_order":["duckduckgo"]}`)}

	ctx := WithBuiltinToolSettings(context.Background(), global)
	ctx = WithTenantToolSettings(ctx, tenant)

	got := chainNames(ResolveWebSearchChain(ctx, defaultChain()))
	if len(got) != 1 || got[0] != "duckduckgo" {
		t.Errorf("tenant beats global: got %v, want [duckduckgo]", got)
	}
}
