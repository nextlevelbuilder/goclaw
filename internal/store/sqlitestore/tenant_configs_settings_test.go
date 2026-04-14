//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// newTenantToolCfgFixture returns a ready-to-use SQLiteBuiltinToolTenantConfigStore
// backed by a temp DB with tenants + builtin_tools rows pre-seeded (FK targets).
func newTenantToolCfgFixture(t *testing.T) (*SQLiteBuiltinToolTenantConfigStore, uuid.UUID) {
	t.Helper()

	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	// Seed a tenant + tool to satisfy FK constraints.
	tenantID := uuid.New()
	if _, err := db.Exec(
		`INSERT INTO tenants (id, name, slug, status, settings, created_at, updated_at)
		 VALUES (?, ?, ?, 'active', '{}',
		         strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
		         strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))`,
		tenantID, "tenant-a", "tenant-a-"+tenantID.String()[:8],
	); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	for _, tool := range []string{"web_search", "web_fetch", "tts"} {
		if _, err := db.Exec(
			`INSERT INTO builtin_tools (name, display_name, description, category, enabled, settings, requires, metadata, created_at, updated_at)
			 VALUES (?, ?, '', 'test', 1, '{}', '[]', '{}',
			         strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
			         strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))`,
			tool, tool,
		); err != nil {
			t.Fatalf("seed builtin_tool %s: %v", tool, err)
		}
	}

	return NewSQLiteBuiltinToolTenantConfigStore(db), tenantID
}

// ---- GetSettings / SetSettings round-trip ----

func TestSQLiteTenantCfg_SettingsRoundTrip(t *testing.T) {
	s, tid := newTenantToolCfgFixture(t)
	ctx := context.Background()

	raw := json.RawMessage(`{"exa":{"enabled":true,"max_results":15}}`)
	if err := s.SetSettings(ctx, tid, "web_search", raw); err != nil {
		t.Fatalf("SetSettings: %v", err)
	}

	got, err := s.GetSettings(ctx, tid, "web_search")
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if string(got) != string(raw) {
		t.Errorf("GetSettings = %s, want %s", got, raw)
	}
}

func TestSQLiteTenantCfg_GetSettings_MissingRow_ReturnsNilNil(t *testing.T) {
	s, tid := newTenantToolCfgFixture(t)
	ctx := context.Background()

	got, err := s.GetSettings(ctx, tid, "tts")
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for missing row, got %s", got)
	}
}

func TestSQLiteTenantCfg_GetSettings_NullColumn_ReturnsNilNil(t *testing.T) {
	s, tid := newTenantToolCfgFixture(t)
	ctx := context.Background()

	// Create row via Set(enabled) — settings column stays NULL.
	if err := s.Set(ctx, tid, "tts", true); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, err := s.GetSettings(ctx, tid, "tts")
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for NULL settings column, got %s", got)
	}
}

// ---- ErrInvalidTenant sentinel ----

func TestSQLiteTenantCfg_NilTenant_ReturnsErrInvalidTenant(t *testing.T) {
	s, _ := newTenantToolCfgFixture(t)
	ctx := context.Background()

	_, err := s.GetSettings(ctx, uuid.Nil, "web_search")
	if !errors.Is(err, store.ErrInvalidTenant) {
		t.Errorf("GetSettings nil tenant err = %v, want ErrInvalidTenant", err)
	}

	err = s.SetSettings(ctx, uuid.Nil, "web_search", json.RawMessage(`{}`))
	if !errors.Is(err, store.ErrInvalidTenant) {
		t.Errorf("SetSettings nil tenant err = %v, want ErrInvalidTenant", err)
	}

	_, err = s.ListAllSettings(ctx, uuid.Nil)
	if !errors.Is(err, store.ErrInvalidTenant) {
		t.Errorf("ListAllSettings nil tenant err = %v, want ErrInvalidTenant", err)
	}

	// Existing methods now guarded too.
	err = s.Set(ctx, uuid.Nil, "web_search", true)
	if !errors.Is(err, store.ErrInvalidTenant) {
		t.Errorf("Set nil tenant err = %v, want ErrInvalidTenant", err)
	}
}

// ---- Column preservation ----

// SetSettings must preserve the existing enabled flag.
func TestSQLiteTenantCfg_SetSettings_PreservesEnabled(t *testing.T) {
	s, tid := newTenantToolCfgFixture(t)
	ctx := context.Background()

	// Set enabled first.
	if err := s.Set(ctx, tid, "web_search", true); err != nil {
		t.Fatalf("Set: %v", err)
	}
	// Then overwrite settings.
	raw := json.RawMessage(`{"brave":{"max_results":20}}`)
	if err := s.SetSettings(ctx, tid, "web_search", raw); err != nil {
		t.Fatalf("SetSettings: %v", err)
	}

	all, err := s.ListAll(ctx, tid)
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if !all["web_search"] {
		t.Errorf("enabled flag lost after SetSettings: %#v", all)
	}
}

// Set(enabled) must preserve the existing settings column.
func TestSQLiteTenantCfg_Set_PreservesSettings(t *testing.T) {
	s, tid := newTenantToolCfgFixture(t)
	ctx := context.Background()

	raw := json.RawMessage(`{"brave":{"max_results":20}}`)
	if err := s.SetSettings(ctx, tid, "web_search", raw); err != nil {
		t.Fatalf("SetSettings: %v", err)
	}
	// Then toggle enabled.
	if err := s.Set(ctx, tid, "web_search", false); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, err := s.GetSettings(ctx, tid, "web_search")
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if string(got) != string(raw) {
		t.Errorf("settings lost after Set(enabled), got %s", got)
	}
}

// SetSettings(nil) clears the settings column without deleting the row.
func TestSQLiteTenantCfg_SetSettings_Nil_ClearsColumn(t *testing.T) {
	s, tid := newTenantToolCfgFixture(t)
	ctx := context.Background()

	raw := json.RawMessage(`{"brave":{"max_results":20}}`)
	if err := s.SetSettings(ctx, tid, "web_search", raw); err != nil {
		t.Fatalf("SetSettings initial: %v", err)
	}
	if err := s.Set(ctx, tid, "web_search", true); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Clear settings.
	if err := s.SetSettings(ctx, tid, "web_search", nil); err != nil {
		t.Fatalf("SetSettings nil: %v", err)
	}

	got, err := s.GetSettings(ctx, tid, "web_search")
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil settings after clear, got %s", got)
	}

	// Enabled must still be true — row not deleted.
	all, err := s.ListAll(ctx, tid)
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if !all["web_search"] {
		t.Errorf("enabled flag lost after SetSettings(nil): %#v", all)
	}
}

// ---- ListAllSettings ----

func TestSQLiteTenantCfg_ListAllSettings(t *testing.T) {
	s, tid := newTenantToolCfgFixture(t)
	ctx := context.Background()

	rawA := json.RawMessage(`{"a":1}`)
	rawB := json.RawMessage(`{"b":2}`)
	if err := s.SetSettings(ctx, tid, "web_search", rawA); err != nil {
		t.Fatalf("SetSettings web_search: %v", err)
	}
	if err := s.SetSettings(ctx, tid, "web_fetch", rawB); err != nil {
		t.Fatalf("SetSettings web_fetch: %v", err)
	}
	// A row with NULL settings should be excluded.
	if err := s.Set(ctx, tid, "tts", true); err != nil {
		t.Fatalf("Set tts: %v", err)
	}

	all, err := s.ListAllSettings(ctx, tid)
	if err != nil {
		t.Fatalf("ListAllSettings: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("ListAllSettings len = %d, want 2 (tts row with NULL settings excluded)", len(all))
	}
	if string(all["web_search"]) != string(rawA) {
		t.Errorf("web_search = %s, want %s", all["web_search"], rawA)
	}
	if string(all["web_fetch"]) != string(rawB) {
		t.Errorf("web_fetch = %s, want %s", all["web_fetch"], rawB)
	}
	if _, has := all["tts"]; has {
		t.Errorf("ListAllSettings should exclude rows with NULL settings, but got tts")
	}
}

// ---- Cross-tenant isolation ----

func TestSQLiteTenantCfg_CrossTenantIsolation(t *testing.T) {
	s, tidA := newTenantToolCfgFixture(t)
	ctx := context.Background()

	// Seed a second tenant on the same DB by reaching into the store.
	// (We borrow the underlying db handle via a minimal shim — simpler than a second fixture.)
	if _, err := s.db.Exec(
		`INSERT INTO tenants (id, name, slug, status, settings, created_at, updated_at)
		 VALUES (?, ?, ?, 'active', '{}',
		         strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
		         strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))`,
		uuid.New().String(), "tenant-b", "tenant-b",
	); err != nil {
		t.Fatalf("seed tenant b: %v", err)
	}
	var tidBStr string
	if err := s.db.QueryRow(`SELECT id FROM tenants WHERE slug = 'tenant-b'`).Scan(&tidBStr); err != nil {
		t.Fatalf("fetch tenant b id: %v", err)
	}
	tidB, err := uuid.Parse(tidBStr)
	if err != nil {
		t.Fatalf("parse tenant b id: %v", err)
	}

	// Tenant A writes settings.
	if err := s.SetSettings(ctx, tidA, "web_search", json.RawMessage(`{"secret":"A"}`)); err != nil {
		t.Fatalf("A SetSettings: %v", err)
	}

	// Tenant B must not see A's settings.
	got, err := s.GetSettings(ctx, tidB, "web_search")
	if err != nil {
		t.Fatalf("B GetSettings: %v", err)
	}
	if got != nil {
		t.Errorf("tenant B leaked A's settings: %s", got)
	}

	allB, err := s.ListAllSettings(ctx, tidB)
	if err != nil {
		t.Fatalf("B ListAllSettings: %v", err)
	}
	if len(allB) != 0 {
		t.Errorf("tenant B ListAllSettings = %v, want empty", allB)
	}
}
