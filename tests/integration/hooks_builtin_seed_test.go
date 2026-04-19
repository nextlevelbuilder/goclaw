//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/hooks"
	"github.com/nextlevelbuilder/goclaw/internal/hooks/builtin"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// Phase 08 — D bucket: builtin seed reconciliation against live PG.
//
// Seed() runs against shared state (the PG hooks table) — these tests
// share the test DB with other integration tests, so each one purges the
// seeded rows in t.Cleanup. Tests run sequentially via the shared DB lock.

const seededBuiltinName = "pii-redactor"

// loadBuiltins ensures the YAML registry is parsed; safe to call repeatedly.
func loadBuiltins(t *testing.T) {
	t.Helper()
	if err := builtin.Load(); err != nil {
		t.Fatalf("builtin.Load: %v", err)
	}
}

// purgeBuiltinRows removes any seeded builtin rows for the given builtin id —
// matches the BuiltinEventID(name, ev) pattern. Idempotent.
func purgeBuiltinRows(t *testing.T, name string) {
	t.Helper()
	db := testDB(t)
	for _, ev := range builtinEventList(t, name) {
		id := builtin.BuiltinEventID(name, ev)
		db.Exec("DELETE FROM hook_executions WHERE hook_id = $1", id)
		db.Exec("DELETE FROM hooks WHERE id = $1", id)
	}
}

// builtinEventList returns the events the named builtin claims in YAML.
func builtinEventList(t *testing.T, name string) []string {
	t.Helper()
	for _, s := range builtin.RegisteredSpecs() {
		if s.ID == name {
			return s.Events
		}
	}
	return nil
}

// test-D1: fresh seed inserts one row per event with stable UUIDv5 IDs.
func TestHooksD1_SeedInsertsStableUUIDsPerEvent(t *testing.T) {
	loadBuiltins(t)
	purgeBuiltinRows(t, seededBuiltinName)
	t.Cleanup(func() { purgeBuiltinRows(t, seededBuiltinName) })

	hs := pg.NewPGHookStore(testDB(t))
	if err := builtin.Seed(context.Background(), hs, config.HooksConfig{}); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	events := builtinEventList(t, seededBuiltinName)
	if len(events) == 0 {
		t.Fatalf("no events registered for %q", seededBuiltinName)
	}
	for _, ev := range events {
		expectedID := builtin.BuiltinEventID(seededBuiltinName, ev)
		got, err := hs.GetByID(crossTenantOwnerCtx(), expectedID)
		if err != nil {
			t.Fatalf("GetByID %s/%s: %v", seededBuiltinName, ev, err)
		}
		if got == nil {
			t.Fatalf("missing row for %s/%s (id=%s)", seededBuiltinName, ev, expectedID)
		}
		if got.Source != hooks.SourceBuiltin {
			t.Errorf("source=%q, want builtin", got.Source)
		}
		if got.HandlerType != hooks.HandlerScript {
			t.Errorf("handler=%q, want script", got.HandlerType)
		}
	}
}

// test-D1b (idempotency): seed ran twice produces no extra rows. Without H9
// (cfg.ID honor) this would double the row count on each boot.
func TestHooksD1b_SeedIdempotent(t *testing.T) {
	loadBuiltins(t)
	purgeBuiltinRows(t, seededBuiltinName)
	t.Cleanup(func() { purgeBuiltinRows(t, seededBuiltinName) })

	hs := pg.NewPGHookStore(testDB(t))
	if err := builtin.Seed(context.Background(), hs, config.HooksConfig{}); err != nil {
		t.Fatalf("Seed first: %v", err)
	}
	if err := builtin.Seed(context.Background(), hs, config.HooksConfig{}); err != nil {
		t.Fatalf("Seed second: %v", err)
	}

	events := builtinEventList(t, seededBuiltinName)
	for _, ev := range events {
		id := builtin.BuiltinEventID(seededBuiltinName, ev)
		row, err := hs.GetByID(crossTenantOwnerCtx(), id)
		if err != nil || row == nil {
			t.Fatalf("missing row after second seed %s/%s: err=%v", seededBuiltinName, ev, err)
		}
	}
	// Cross-check the COUNT — only one row per (id) should exist (PK guard).
	var n int
	if err := testDB(t).QueryRow(
		`SELECT COUNT(*) FROM hooks WHERE source = 'builtin'`,
	).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != len(events) {
		t.Errorf("builtin rows count=%d, want %d", n, len(events))
	}
}

// test-D2: when DB has an older metadata.version than the embedded spec, a
// re-seed UPDATEs each row. We simulate this by directly downgrading the
// metadata.version column post-seed, then re-seeding and checking the bump.
func TestHooksD2_VersionBumpUpdatesRows(t *testing.T) {
	loadBuiltins(t)
	purgeBuiltinRows(t, seededBuiltinName)
	t.Cleanup(func() { purgeBuiltinRows(t, seededBuiltinName) })

	db := testDB(t)
	hs := pg.NewPGHookStore(db)

	if err := builtin.Seed(context.Background(), hs, config.HooksConfig{}); err != nil {
		t.Fatalf("Seed initial: %v", err)
	}
	events := builtinEventList(t, seededBuiltinName)

	// Force-downgrade metadata.version to 0 for every seeded row.
	for _, ev := range events {
		id := builtin.BuiltinEventID(seededBuiltinName, ev)
		_, err := db.Exec(
			`UPDATE hooks SET metadata = jsonb_set(metadata, '{version}', '0'::jsonb) WHERE id = $1`,
			id,
		)
		if err != nil {
			t.Fatalf("downgrade %s/%s: %v", seededBuiltinName, ev, err)
		}
	}

	// Re-seed: embedVersion (≥1) > dbVersion (0) → triggers Update.
	if err := builtin.Seed(context.Background(), hs, config.HooksConfig{}); err != nil {
		t.Fatalf("Seed re-run: %v", err)
	}

	// Confirm metadata.version was bumped back up by the seeder.
	for _, ev := range events {
		id := builtin.BuiltinEventID(seededBuiltinName, ev)
		row, err := hs.GetByID(crossTenantOwnerCtx(), id)
		if err != nil || row == nil {
			t.Fatalf("GetByID after bump %s/%s: err=%v row=%v", seededBuiltinName, ev, err, row)
		}
		v := jsonNumber(row.Metadata["version"])
		if v == 0 {
			t.Errorf("metadata.version still 0 after bump (row=%s/%s)", seededBuiltinName, ev)
		}
	}
}

// test-D3: when DB version > embed version, seeder logs WARN and leaves the
// DB row alone (no rollback). We simulate by setting metadata.version=99 and
// asserting the row stays at 99 after re-seed.
func TestHooksD3_DowngradeSkippedWithWarning(t *testing.T) {
	loadBuiltins(t)
	purgeBuiltinRows(t, seededBuiltinName)
	t.Cleanup(func() { purgeBuiltinRows(t, seededBuiltinName) })

	db := testDB(t)
	hs := pg.NewPGHookStore(db)

	if err := builtin.Seed(context.Background(), hs, config.HooksConfig{}); err != nil {
		t.Fatalf("Seed initial: %v", err)
	}
	events := builtinEventList(t, seededBuiltinName)
	for _, ev := range events {
		id := builtin.BuiltinEventID(seededBuiltinName, ev)
		if _, err := db.Exec(
			`UPDATE hooks SET metadata = jsonb_set(metadata, '{version}', '99'::jsonb) WHERE id = $1`,
			id,
		); err != nil {
			t.Fatalf("upgrade: %v", err)
		}
	}

	if err := builtin.Seed(context.Background(), hs, config.HooksConfig{}); err != nil {
		t.Fatalf("Seed re-run: %v", err)
	}

	for _, ev := range events {
		id := builtin.BuiltinEventID(seededBuiltinName, ev)
		row, _ := hs.GetByID(crossTenantOwnerCtx(), id)
		if v := jsonNumber(row.Metadata["version"]); v != 99 {
			t.Errorf("metadata.version=%d after downgrade-skip; want 99 (no rollback)", v)
		}
	}
}

// test-D4: BuiltinDisable list flips the enabled flag. Subset of disable list
// containing the seeded builtin name forces enabled=false on the row.
func TestHooksD4_BuiltinDisableListForcesOff(t *testing.T) {
	loadBuiltins(t)
	purgeBuiltinRows(t, seededBuiltinName)
	t.Cleanup(func() { purgeBuiltinRows(t, seededBuiltinName) })

	hs := pg.NewPGHookStore(testDB(t))
	if err := builtin.Seed(context.Background(), hs, config.HooksConfig{
		BuiltinDisable: []string{seededBuiltinName},
	}); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	for _, ev := range builtinEventList(t, seededBuiltinName) {
		id := builtin.BuiltinEventID(seededBuiltinName, ev)
		row, err := hs.GetByID(crossTenantOwnerCtx(), id)
		if err != nil || row == nil {
			t.Fatalf("GetByID %s/%s: err=%v row=%v", seededBuiltinName, ev, err, row)
		}
		if row.Enabled {
			t.Errorf("row %s/%s enabled despite builtin_disable list", seededBuiltinName, ev)
		}
	}
}

// test-D5: store-level builtin protection — Update with non-enabled patch
// fails ErrBuiltinReadOnly under user ctx; Update with enabled-only patch
// succeeds. Delete fails. SeedBypass ctx makes everything succeed.
func TestHooksD5_BuiltinReadonlyProtection(t *testing.T) {
	loadBuiltins(t)
	purgeBuiltinRows(t, seededBuiltinName)
	t.Cleanup(func() { purgeBuiltinRows(t, seededBuiltinName) })

	hs := pg.NewPGHookStore(testDB(t))
	if err := builtin.Seed(context.Background(), hs, config.HooksConfig{}); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	events := builtinEventList(t, seededBuiltinName)
	if len(events) == 0 {
		t.Fatalf("no events for %q", seededBuiltinName)
	}
	id := builtin.BuiltinEventID(seededBuiltinName, events[0])

	userCtx := crossTenantOwnerCtx() // master scope, NOT seed bypass

	// (a) non-enabled patch must reject.
	err := hs.Update(userCtx, id, map[string]any{"matcher": "x"})
	if !errors.Is(err, hooks.ErrBuiltinReadOnly) {
		t.Errorf("Update matcher: err=%v, want ErrBuiltinReadOnly", err)
	}
	// (b) enabled-only patch must succeed.
	if err := hs.Update(userCtx, id, map[string]any{"enabled": false}); err != nil {
		t.Errorf("Update enabled toggle: %v", err)
	}
	// (c) Delete must reject.
	if err := hs.Delete(userCtx, id); !errors.Is(err, hooks.ErrBuiltinReadOnly) {
		t.Errorf("Delete: err=%v, want ErrBuiltinReadOnly", err)
	}
	// (d) Seed-bypass ctx allows wider Update + Delete.
	bypassCtx := hooks.WithSeedBypass(userCtx)
	if err := hs.Update(bypassCtx, id, map[string]any{"matcher": "y"}); err != nil {
		t.Errorf("Update under bypass: %v", err)
	}
	if err := hs.Delete(bypassCtx, id); err != nil {
		t.Errorf("Delete under bypass: %v", err)
	}
}

// jsonNumber extracts an int from a metadata value (PG JSONB decodes ints to
// float64 when round-tripped through json.Unmarshal).
func jsonNumber(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	default:
		return 0
	}
}

// _ asserts uuid is referenced — keeps the import explicit for tests adding
// fixtures with manual IDs in the future.
var _ = uuid.Nil
