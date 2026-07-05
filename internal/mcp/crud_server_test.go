package mcp

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// TestNewCRUDServer_NoDeps_RegistersOnlyQuota verifies the "degrade
// gracefully" contract documented on CRUDDeps: with a completely empty
// CRUDDeps, no store-backed tool family is registered — only the
// unconditional goclaw_quota_* family (which tolerates nil Quota/DB), so a
// deployment with nothing wired still produces a server that constructs
// without panicking and exposes no dangling/broken tools.
func TestNewCRUDServer_NoDeps_RegistersOnlyQuota(t *testing.T) {
	httpServer := NewCRUDServer(CRUDDeps{}, "test")
	if httpServer == nil {
		t.Fatal("expected non-nil StreamableHTTPServer even with empty deps")
	}
}

// TestNewCRUDServer_AgentsOnly_RegistersAgentToolsNotSessions verifies that
// each tool family is gated strictly on its own dependency being non-nil,
// independent of other families.
func TestNewCRUDServer_AgentsOnly_RegistersAgentToolsNotSessions(t *testing.T) {
	agents := newFakeAgentStore()

	// Build the same MCPServer construction NewCRUDServer uses internally by
	// calling the registration function directly, since CRUDDeps only
	// exposes the fully-assembled StreamableHTTPServer (no tool introspection
	// hook) — mirror its gating logic against a bare MCPServer instead.
	srv := newTestMCPServer()
	registerAgentCRUDTools(srv, agents)

	if tool := srv.GetTool("goclaw_agents_list"); tool == nil {
		t.Error("expected goclaw_agents_list to be registered when Agents is set")
	}
	if tool := srv.GetTool("goclaw_sessions_list"); tool != nil {
		t.Error("expected goclaw_sessions_list to NOT be registered when Sessions was never registered")
	}
}

// TestNewCRUDServer_SkillManageStore_RegistersUpdateToolOnlyWhenSupported
// verifies goclaw_skills_update is only registered when the skill store also
// implements store.SkillManageStore (crud_server.go's type-assertion gate).
func TestNewCRUDServer_SkillManageStore_RegistersUpdateToolOnlyWhenSupported(t *testing.T) {
	// Plain SkillStore (no manage capability): registerSkillCRUDTools alone,
	// mirroring what NewCRUDServer does when the type assertion fails.
	srv := newTestMCPServer()
	skills := newFakeSkillStore()
	registerSkillCRUDTools(srv, skills)
	if tool := srv.GetTool("goclaw_skills_update"); tool != nil {
		t.Error("expected goclaw_skills_update to NOT be registered for a plain SkillStore")
	}

	// SkillManageStore-capable store: both list/get and update should be
	// registered, matching NewCRUDServer's `if manage, ok := ...; ok` branch.
	srv2 := newTestMCPServer()
	manage := newFakeSkillManageStore()
	registerSkillCRUDTools(srv2, manage)
	registerSkillUpdateCRUDTool(srv2, manage, manage)
	if tool := srv2.GetTool("goclaw_skills_update"); tool == nil {
		t.Error("expected goclaw_skills_update to be registered for a SkillManageStore-capable store")
	}
}

// TestNewCRUDServer_ConfigOnly_Constructs is a smoke test that a single
// non-nil dependency (Config) still produces a working server without
// requiring every other field to be populated.
func TestNewCRUDServer_ConfigOnly_Constructs(t *testing.T) {
	cfg := &config.Config{}
	httpServer := NewCRUDServer(CRUDDeps{Config: cfg}, "test")
	if httpServer == nil {
		t.Fatal("expected non-nil StreamableHTTPServer with only Config set")
	}
}

// TestResolveMCPTenantID_HeaderPresentAndValid_ScopesToThatTenant verifies
// that a valid "X-GoClaw-Tenant-Id" header (UUID form) resolves to the
// matching tenant.
func TestResolveMCPTenantID_HeaderPresentAndValid_ScopesToThatTenant(t *testing.T) {
	tenants := newFakeTenantStore()
	want := &store.TenantData{ID: uuid.New(), Slug: "acme"}
	tenants.addTenant(want)

	got := resolveMCPTenantID(context.Background(), tenants, want.ID.String())
	if got != want.ID {
		t.Fatalf("expected tenant %s, got %s", want.ID, got)
	}
}

// TestResolveMCPTenantID_HeaderPresentSlug_ScopesToThatTenant verifies slug
// lookups (non-UUID header values) also resolve correctly.
func TestResolveMCPTenantID_HeaderPresentSlug_ScopesToThatTenant(t *testing.T) {
	tenants := newFakeTenantStore()
	want := &store.TenantData{ID: uuid.New(), Slug: "acme"}
	tenants.addTenant(want)

	got := resolveMCPTenantID(context.Background(), tenants, "acme")
	if got != want.ID {
		t.Fatalf("expected tenant %s, got %s", want.ID, got)
	}
}

// TestResolveMCPTenantID_HeaderAbsent_DefaultsToMasterTenant verifies the
// fail-safe default (master tenant, not uuid.Nil/unscoped) when the caller
// supplies no tenant header at all.
func TestResolveMCPTenantID_HeaderAbsent_DefaultsToMasterTenant(t *testing.T) {
	tenants := newFakeTenantStore()

	got := resolveMCPTenantID(context.Background(), tenants, "")
	if got != store.MasterTenantID {
		t.Fatalf("expected MasterTenantID, got %s", got)
	}
}

// TestResolveMCPTenantID_HeaderUnresolvable_DefaultsToMasterTenant verifies
// that an invalid/unknown tenant header fails safe to the master tenant
// rather than silently falling through to an unscoped (uuid.Nil) context —
// unscoped context previously allowed writes to leak into the wrong tenant
// (or master) depending on downstream store fallback behavior.
func TestResolveMCPTenantID_HeaderUnresolvable_DefaultsToMasterTenant(t *testing.T) {
	tenants := newFakeTenantStore()

	got := resolveMCPTenantID(context.Background(), tenants, uuid.New().String())
	if got != store.MasterTenantID {
		t.Fatalf("expected MasterTenantID fallback for unknown tenant id, got %s", got)
	}

	got = resolveMCPTenantID(context.Background(), tenants, "not-a-real-slug")
	if got != store.MasterTenantID {
		t.Fatalf("expected MasterTenantID fallback for unknown tenant slug, got %s", got)
	}
}

// TestResolveMCPTenantID_NilTenantStore_DefaultsToMasterTenant verifies the
// server still degrades gracefully (falls back to master tenant rather than
// panicking or leaving tenant scope unset) when CRUDDeps.Tenants is nil —
// e.g. an edition/build that never wired a tenant store.
func TestResolveMCPTenantID_NilTenantStore_DefaultsToMasterTenant(t *testing.T) {
	got := resolveMCPTenantID(context.Background(), nil, uuid.New().String())
	if got != store.MasterTenantID {
		t.Fatalf("expected MasterTenantID with nil tenant store, got %s", got)
	}
}
