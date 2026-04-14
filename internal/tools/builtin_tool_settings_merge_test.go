package tools

import (
	"bytes"
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ---- BuiltinToolSettingsFromCtx merge semantics (4-tier overlay) ----

func TestBuiltinToolSettingsFromCtx_EmptyCtx_ReturnsNil(t *testing.T) {
	if got := BuiltinToolSettingsFromCtx(context.Background()); got != nil {
		t.Errorf("empty ctx: want nil, got %v", got)
	}
}

func TestBuiltinToolSettingsFromCtx_PerAgentOnly(t *testing.T) {
	perAgent := BuiltinToolSettings{"web_search": []byte(`{"k":"per-agent"}`)}
	ctx := WithBuiltinToolSettings(context.Background(), perAgent)

	got := BuiltinToolSettingsFromCtx(ctx)
	if !bytes.Equal(got["web_search"], []byte(`{"k":"per-agent"}`)) {
		t.Errorf("per-agent only: got %s", got["web_search"])
	}

	// Fast path: same map returned (no copy).
	if &got == &perAgent {
		// maps are reference types — compare underlying via len + value
	}
	if len(got) != 1 {
		t.Errorf("expected single entry, got %d", len(got))
	}
}

func TestBuiltinToolSettingsFromCtx_TenantOnly(t *testing.T) {
	tenant := BuiltinToolSettings{"web_search": []byte(`{"k":"tenant"}`)}
	ctx := WithTenantToolSettings(context.Background(), tenant)

	got := BuiltinToolSettingsFromCtx(ctx)
	if !bytes.Equal(got["web_search"], []byte(`{"k":"tenant"}`)) {
		t.Errorf("tenant only: got %s", got["web_search"])
	}
}

func TestBuiltinToolSettingsFromCtx_BothMergeTenantWinsOverGlobal(t *testing.T) {
	// WithBuiltinToolSettings carries tier 3 (global defaults).
	// WithTenantToolSettings carries tier 2 (tenant admin override).
	// Tenant must win at tool-name level.
	global := BuiltinToolSettings{
		"web_search": []byte(`{"k":"global"}`),
		"web_fetch":  []byte(`{"k":"global"}`),
	}
	tenant := BuiltinToolSettings{
		"web_search": []byte(`{"k":"tenant"}`), // overrides global
		"tts":        []byte(`{"k":"tenant"}`), // tenant-only
	}

	ctx := WithBuiltinToolSettings(context.Background(), global)
	ctx = WithTenantToolSettings(ctx, tenant)

	got := BuiltinToolSettingsFromCtx(ctx)
	if len(got) != 3 {
		t.Errorf("merged: want 3 entries, got %d (%v)", len(got), got)
	}
	// web_search: tenant wins (overrides global default).
	if !bytes.Equal(got["web_search"], []byte(`{"k":"tenant"}`)) {
		t.Errorf("tenant should override global for web_search, got %s", got["web_search"])
	}
	// web_fetch: only global has it — global value surfaces.
	if !bytes.Equal(got["web_fetch"], []byte(`{"k":"global"}`)) {
		t.Errorf("web_fetch should come from global, got %s", got["web_fetch"])
	}
	// tts: only tenant has it.
	if !bytes.Equal(got["tts"], []byte(`{"k":"tenant"}`)) {
		t.Errorf("tts should come from tenant, got %s", got["tts"])
	}
}

func TestBuiltinToolSettingsFromCtx_RunContextFallback(t *testing.T) {
	// Neither ctx key set but RunContext has settings — should fall back.
	rc := &store.RunContext{
		BuiltinToolSettings: map[string][]byte{"web_search": []byte(`{"from":"rc"}`)},
	}
	ctx := store.WithRunContext(context.Background(), rc)

	got := BuiltinToolSettingsFromCtx(ctx)
	if !bytes.Equal(got["web_search"], []byte(`{"from":"rc"}`)) {
		t.Errorf("RunContext fallback: got %s", got["web_search"])
	}
}

func TestBuiltinToolSettingsFromCtx_BuiltinCtxKeyOverridesRunContext(t *testing.T) {
	// Builtin ctx key must take precedence over RunContext fallback.
	// RunContext only serves empty-both-keys case.
	rc := &store.RunContext{
		BuiltinToolSettings: map[string][]byte{"web_search": []byte(`{"from":"rc"}`)},
	}
	global := BuiltinToolSettings{"web_search": []byte(`{"from":"global"}`)}

	ctx := store.WithRunContext(context.Background(), rc)
	ctx = WithBuiltinToolSettings(ctx, global)

	got := BuiltinToolSettingsFromCtx(ctx)
	if !bytes.Equal(got["web_search"], []byte(`{"from":"global"}`)) {
		t.Errorf("ctx key must override RunContext, got %s", got["web_search"])
	}
}

// ---- TenantToolSettingsFromCtx raw accessor ----

func TestTenantToolSettingsFromCtx_RoundTrip(t *testing.T) {
	tenant := BuiltinToolSettings{"web_search": []byte(`{"k":"tenant"}`)}
	ctx := WithTenantToolSettings(context.Background(), tenant)

	got := TenantToolSettingsFromCtx(ctx)
	if len(got) != 1 || !bytes.Equal(got["web_search"], tenant["web_search"]) {
		t.Errorf("raw tenant round-trip failed: %v", got)
	}

	// Empty ctx → nil
	if TenantToolSettingsFromCtx(context.Background()) != nil {
		t.Errorf("empty ctx should return nil tenant settings")
	}
}

// ---- Fast path allocation check ----
// When only one tier is present, the merge function must return the same
// underlying map without allocating. We verify by checking equality after
// mutation — if a copy was made, the original wouldn't see the change.
//
// Note: this is a behavioral assertion, not an allocation benchmark.

func TestBuiltinToolSettingsFromCtx_FastPath_ReturnsSameMap(t *testing.T) {
	perAgent := BuiltinToolSettings{"web_search": []byte(`original`)}
	ctx := WithBuiltinToolSettings(context.Background(), perAgent)

	got := BuiltinToolSettingsFromCtx(ctx)
	// Mutate the returned map — if it's the same map, perAgent sees the change.
	got["web_fetch"] = []byte(`mutated`)
	if _, ok := perAgent["web_fetch"]; !ok {
		t.Errorf("fast path should return same map, got a copy")
	}
}
