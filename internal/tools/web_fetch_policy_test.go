package tools

import (
	"context"
	"encoding/json"
	"testing"
)

// ctxWithWebFetchSettings returns a ctx with builtin tool settings containing
// the given web_fetch policy override.
func ctxWithWebFetchSettings(t *testing.T, override webFetchPolicyOverride) context.Context {
	t.Helper()
	raw, err := json.Marshal(override)
	if err != nil {
		t.Fatal(err)
	}
	settings := BuiltinToolSettings{"web_fetch": raw}
	return WithBuiltinToolSettings(context.Background(), settings)
}

func TestResolvePolicy_NoOverride_ReturnsDefaults(t *testing.T) {
	tool := NewWebFetchTool(WebFetchConfig{
		Policy:         "allow_all",
		AllowedDomains: []string{"default.com"},
		BlockedDomains: []string{"blocked.com"},
	})

	pol := tool.resolvePolicy(context.Background())
	if pol.mode != "allow_all" {
		t.Errorf("mode = %q, want allow_all", pol.mode)
	}
	if len(pol.blockedDomains) != 1 || pol.blockedDomains[0] != "blocked.com" {
		t.Errorf("blockedDomains = %v, want [blocked.com]", pol.blockedDomains)
	}
}

func TestResolvePolicy_TenantAllowlist(t *testing.T) {
	tool := NewWebFetchTool(WebFetchConfig{
		Policy:         "allow_all",
		AllowedDomains: []string{"default.com"},
	})

	ctx := ctxWithWebFetchSettings(t, webFetchPolicyOverride{
		Policy:         "allowlist",
		AllowedDomains: []string{"tenant-a.com", "*.api.tenant-a.com"},
	})

	pol := tool.resolvePolicy(ctx)
	if pol.mode != "allowlist" {
		t.Errorf("mode = %q, want allowlist", pol.mode)
	}
	if len(pol.allowedDomains) != 2 {
		t.Errorf("allowedDomains = %v, want 2 entries", pol.allowedDomains)
	}
}

func TestResolvePolicy_TenantBlocklist(t *testing.T) {
	tool := NewWebFetchTool(WebFetchConfig{
		Policy:         "allow_all",
		BlockedDomains: []string{"global-blocked.com"},
	})

	ctx := ctxWithWebFetchSettings(t, webFetchPolicyOverride{
		Policy:         "allow_all",
		BlockedDomains: []string{"tenant-blocked.com"},
	})

	pol := tool.resolvePolicy(ctx)
	// Tenant override replaces global blocked list
	if len(pol.blockedDomains) != 1 || pol.blockedDomains[0] != "tenant-blocked.com" {
		t.Errorf("blockedDomains = %v, want [tenant-blocked.com]", pol.blockedDomains)
	}
}

func TestResolvePolicy_MalformedJSON_FallsBack(t *testing.T) {
	tool := NewWebFetchTool(WebFetchConfig{
		Policy: "allow_all",
	})

	settings := BuiltinToolSettings{
		"web_fetch": []byte(`{not valid json}`),
	}
	ctx := WithBuiltinToolSettings(context.Background(), settings)

	pol := tool.resolvePolicy(ctx)
	if pol.mode != "allow_all" {
		t.Errorf("mode = %q, want allow_all (fallback)", pol.mode)
	}
}

func TestResolvePolicy_EmptyOverride_FallsBack(t *testing.T) {
	tool := NewWebFetchTool(WebFetchConfig{
		Policy:         "allowlist",
		AllowedDomains: []string{"default.com"},
	})

	// Override exists but has empty policy — should fall back to defaults
	ctx := ctxWithWebFetchSettings(t, webFetchPolicyOverride{})

	pol := tool.resolvePolicy(ctx)
	if pol.mode != "allowlist" {
		t.Errorf("mode = %q, want allowlist (default)", pol.mode)
	}
	if len(pol.allowedDomains) != 1 || pol.allowedDomains[0] != "default.com" {
		t.Errorf("allowedDomains = %v, want [default.com]", pol.allowedDomains)
	}
}

func TestResolvePolicy_TenantOverridesGlobal(t *testing.T) {
	tool := NewWebFetchTool(WebFetchConfig{
		Policy:         "allowlist",
		AllowedDomains: []string{"global.com"},
		BlockedDomains: []string{"global-blocked.com"},
	})

	ctx := ctxWithWebFetchSettings(t, webFetchPolicyOverride{
		Policy:         "allow_all",
		BlockedDomains: []string{"tenant-evil.com"},
	})

	pol := tool.resolvePolicy(ctx)
	if pol.mode != "allow_all" {
		t.Errorf("mode = %q, want allow_all (tenant override)", pol.mode)
	}
	if len(pol.blockedDomains) != 1 || pol.blockedDomains[0] != "tenant-evil.com" {
		t.Errorf("blockedDomains = %v, want [tenant-evil.com]", pol.blockedDomains)
	}
}
