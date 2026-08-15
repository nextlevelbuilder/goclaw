//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/edition"
	"github.com/nextlevelbuilder/goclaw/internal/hooks"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// Phase 08 — E bucket: DisableLegacyCommandHooks against live PG.
//
// Unit coverage in internal/hooks/migration_command_autodisable_test.go uses
// a fake store + tests the branching. Here we replay the same scenarios
// against the real PG store + live ResolveForEvent path so any drift between
// fake-store stubs and the actual SQL guards is caught.

// makeCommandHook seeds a command-handler hook for the given tenant. Returns
// the row's id; auto-cleanup via t.Cleanup.
func makeCommandHook(t *testing.T, hs hooks.HookStore, tenantID uuid.UUID, source string, enabled bool) uuid.UUID {
	t.Helper()
	cfg := hooks.HookConfig{
		TenantID:    tenantID,
		Scope:       hooks.ScopeTenant,
		Event:       hooks.EventPreToolUse,
		HandlerType: hooks.HandlerCommand,
		Config:      map[string]any{"command": "echo ok"},
		TimeoutMS:   2000,
		OnTimeout:   hooks.DecisionAllow,
		Enabled:     enabled,
		Version:     1,
		Source:      source,
		Metadata:    map[string]any{},
	}
	id, err := hs.Create(crossTenantOwnerCtx(), cfg)
	if err != nil {
		t.Fatalf("Create command hook: %v", err)
	}
	t.Cleanup(func() { purgeAgentHookByID(t, testDB(t), id) })
	return id
}

func makeScriptHook(t *testing.T, hs hooks.HookStore, tenantID uuid.UUID, source string, enabled bool) uuid.UUID {
	t.Helper()
	cfg := hookCfgScript(tenantID, hooks.EventPreToolUse, source, allowAllScript)
	cfg.Enabled = enabled
	if cfg.Source == hooks.SourceBuiltin {
		cfg.Scope = hooks.ScopeGlobal
		cfg.TenantID = hooks.SentinelTenantID
		cfg.ID = uuid.New()
	}
	id, err := hs.Create(crossTenantOwnerCtx(), cfg)
	if err != nil {
		t.Fatalf("Create script hook: %v", err)
	}
	t.Cleanup(func() { purgeAgentHookByID(t, testDB(t), id) })
	return id
}

// test-E1: Standard edition with 3 enabled command hooks across 2 tenants →
// all 3 disabled. Idempotent rerun returns 0.
func TestHooksE1_StandardDisablesAllCommand(t *testing.T) {
	db := testDB(t)
	tenantA, _ := seedTenantAgent(t, db)
	tenantB := seedExtraTenant(t, db)
	hs := pg.NewPGHookStore(db)

	id1 := makeCommandHook(t, hs, tenantA, "ui", true)
	id2 := makeCommandHook(t, hs, tenantA, "api", true)
	id3 := makeCommandHook(t, hs, tenantB, "ui", true)

	n, err := hooks.DisableLegacyCommandHooks(context.Background(), hs, edition.Standard)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	if n < 3 {
		t.Errorf("disabled count=%d, want >= 3", n)
	}
	for _, id := range []uuid.UUID{id1, id2, id3} {
		row, _ := hs.GetByID(crossTenantOwnerCtx(), id)
		if row == nil || row.Enabled {
			t.Errorf("hook %s still enabled after migration", id)
		}
	}

	// Idempotency: second run finds none enabled (we just disabled them all).
	n2, err := hooks.DisableLegacyCommandHooks(context.Background(), hs, edition.Standard)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if n2 != 0 {
		t.Errorf("idempotent re-run disabled=%d, want 0", n2)
	}
}

// test-E2: Lite edition is a no-op even when command hooks are present.
func TestHooksE2_LiteIsNoOp(t *testing.T) {
	db := testDB(t)
	tenantA, _ := seedTenantAgent(t, db)
	hs := pg.NewPGHookStore(db)

	id := makeCommandHook(t, hs, tenantA, "ui", true)

	n, err := hooks.DisableLegacyCommandHooks(context.Background(), hs, edition.Lite)
	if err != nil {
		t.Fatalf("Lite run: %v", err)
	}
	if n != 0 {
		t.Errorf("Lite disabled=%d, want 0", n)
	}
	row, _ := hs.GetByID(crossTenantOwnerCtx(), id)
	if row == nil || !row.Enabled {
		t.Fatalf("Lite touched the row: enabled=%v", row.Enabled)
	}
}

// test-E3: command + builtin script mix → only the command row gets disabled;
// builtin script row left alone (defensive carve-out).
func TestHooksE3_BuiltinScriptUntouched(t *testing.T) {
	db := testDB(t)
	tenantA, _ := seedTenantAgent(t, db)
	hs := pg.NewPGHookStore(db)

	cmdID := makeCommandHook(t, hs, tenantA, "ui", true)
	scriptBuiltinID := makeScriptHook(t, hs, tenantA, hooks.SourceBuiltin, true)

	if _, err := hooks.DisableLegacyCommandHooks(context.Background(), hs, edition.Standard); err != nil {
		t.Fatalf("migration: %v", err)
	}

	// Command row off.
	cmdRow, _ := hs.GetByID(crossTenantOwnerCtx(), cmdID)
	if cmdRow == nil || cmdRow.Enabled {
		t.Errorf("command row %s still enabled", cmdID)
	}
	// Builtin script row untouched.
	scriptRow, _ := hs.GetByID(crossTenantOwnerCtx(), scriptBuiltinID)
	if scriptRow == nil || !scriptRow.Enabled {
		t.Errorf("builtin script row %s incorrectly disabled (defensive carve-out failed)", scriptBuiltinID)
	}
}
