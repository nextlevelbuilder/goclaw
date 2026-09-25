//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// seedChannelAgent inserts a minimal active agent and returns its UUID.
func seedChannelAgent(t *testing.T, db *sql.DB, tenant uuid.UUID, key string) uuid.UUID {
	t.Helper()
	id := uuid.NewString()
	mustExec(t, db, `INSERT INTO agents (id, tenant_id, agent_key, agent_type, status, provider, model, owner_id)
		VALUES (?, ?, ?, 'predefined', 'active', 'p', 'm', 'o')`, id, tenant.String(), key)
	return uuid.MustParse(id)
}

// TestSQLiteMergeConfig_PreservesSiblingKeys verifies MergeConfig shallow-merges
// a patch into channel_instances.config without clobbering sibling keys.
func TestSQLiteMergeConfig_PreservesSiblingKeys(t *testing.T) {
	_, db := newTestSQLiteSecureCLI(t)
	tenant := seedTenant(t, db, "mergecfg-a")
	ci := NewSQLiteChannelInstanceStore(db, testEncKey)
	ctx := store.WithTenantID(context.Background(), tenant)

	inst := &store.ChannelInstanceData{
		TenantID:    tenant,
		Name:        "zalo-oa-merge",
		ChannelType: "zalo_oa",
		AgentID:     seedChannelAgent(t, db, tenant, "merge-agent"),
		Config:      json.RawMessage(`{"dm_policy":"pairing"}`),
	}
	if err := ci.Create(ctx, inst); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := ci.MergeConfig(ctx, inst.ID, map[string]any{"poll_cursor": map[string]int64{"u1": 42}}); err != nil {
		t.Fatalf("MergeConfig: %v", err)
	}

	got, err := ci.Get(ctx, inst.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(got.Config, &cfg); err != nil {
		t.Fatalf("config unmarshal: %v", err)
	}
	if cfg["dm_policy"] != "pairing" {
		t.Errorf("sibling dm_policy = %v, want pairing (clobbered)", cfg["dm_policy"])
	}
	cursor, ok := cfg["poll_cursor"].(map[string]any)
	if !ok {
		t.Fatalf("poll_cursor missing or wrong type: %#v", cfg)
	}
	if cursor["u1"] != float64(42) {
		t.Errorf("poll_cursor.u1 = %v, want 42", cursor["u1"])
	}
}

// TestSQLiteMergeConfig_ShallowReplaceNestedObjects proves top-level keys are
// REPLACED (PG jsonb || semantics), not deep-merged (json_patch semantics):
// a nested key absent from the new patch must disappear from the DB.
func TestSQLiteMergeConfig_ShallowReplaceNestedObjects(t *testing.T) {
	_, db := newTestSQLiteSecureCLI(t)
	tenant := seedTenant(t, db, "mergecfg-shallow")
	ci := NewSQLiteChannelInstanceStore(db, testEncKey)
	ctx := store.WithTenantID(context.Background(), tenant)

	inst := &store.ChannelInstanceData{
		TenantID:    tenant,
		Name:        "zalo-oa-shallow",
		ChannelType: "zalo_oa",
		AgentID:     seedChannelAgent(t, db, tenant, "shallow-agent"),
		Config:      json.RawMessage(`{}`),
	}
	if err := ci.Create(ctx, inst); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := ci.MergeConfig(ctx, inst.ID, map[string]any{
		"poll_cursor": map[string]int64{"u1": 1, "u2": 2},
	}); err != nil {
		t.Fatalf("MergeConfig #1: %v", err)
	}
	if err := ci.MergeConfig(ctx, inst.ID, map[string]any{
		"poll_cursor": map[string]int64{"u2": 2},
	}); err != nil {
		t.Fatalf("MergeConfig #2: %v", err)
	}

	got, err := ci.Get(ctx, inst.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(got.Config, &cfg); err != nil {
		t.Fatalf("config unmarshal: %v", err)
	}
	cursor, ok := cfg["poll_cursor"].(map[string]any)
	if !ok {
		t.Fatalf("poll_cursor missing: %#v", cfg)
	}
	if _, exists := cursor["u1"]; exists {
		t.Errorf("u1 survived shallow replace: %#v", cursor)
	}
	if cursor["u2"] != float64(2) {
		t.Errorf("u2 = %v, want 2", cursor["u2"])
	}
}

// TestSQLiteMergeConfig_TenantIsolation verifies MergeConfig refuses to touch
// a foreign tenant's instance when called under a tenant-scoped ctx.
func TestSQLiteMergeConfig_TenantIsolation(t *testing.T) {
	_, db := newTestSQLiteSecureCLI(t)
	tenantA := seedTenant(t, db, "mergecfg-iso-a")
	tenantB := seedTenant(t, db, "mergecfg-iso-b")
	ci := NewSQLiteChannelInstanceStore(db, testEncKey)
	ctxA := store.WithTenantID(context.Background(), tenantA)
	ctxB := store.WithTenantID(context.Background(), tenantB)

	inst := &store.ChannelInstanceData{
		TenantID:    tenantA,
		Name:        "zalo-oa-iso",
		ChannelType: "zalo_oa",
		AgentID:     seedChannelAgent(t, db, tenantA, "iso-agent"),
		Config:      json.RawMessage(`{"dm_policy":"pairing"}`),
	}
	if err := ci.Create(ctxA, inst); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := ci.MergeConfig(ctxB, inst.ID, map[string]any{"poll_cursor": map[string]int64{"u1": 1}}); err != nil {
		t.Fatalf("MergeConfig under tenant B: %v", err)
	}

	got, err := ci.Get(ctxA, inst.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(got.Config, &cfg); err != nil {
		t.Fatalf("config unmarshal: %v", err)
	}
	if _, ok := cfg["poll_cursor"]; ok {
		t.Errorf("poll_cursor appeared under tenant A after tenant-B merge: %#v", cfg)
	}
}

func TestSQLiteMergeConfig_NullConfig(t *testing.T) {
	_, db := newTestSQLiteSecureCLI(t)
	tenant := seedTenant(t, db, "mergecfg-null")
	ci := NewSQLiteChannelInstanceStore(db, testEncKey)
	ctx := store.WithTenantID(context.Background(), tenant)
	inst := &store.ChannelInstanceData{
		Name: "null-config", ChannelType: "zalo_oa",
		AgentID: seedChannelAgent(t, db, tenant, "null-agent"),
		Config:  json.RawMessage(`null`),
	}
	if err := ci.Create(ctx, inst); err != nil {
		t.Fatal(err)
	}
	if err := ci.MergeConfig(ctx, inst.ID, map[string]any{"poll_cursor": map[string]int64{"user": 42}}); err != nil {
		t.Fatal(err)
	}
	got, err := ci.Get(ctx, inst.ID)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Cursor map[string]int64 `json:"poll_cursor"`
	}
	if err := json.Unmarshal(got.Config, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Cursor["user"] != 42 {
		t.Fatalf("cursor lost after merging null config: %s", got.Config)
	}
}

var _ store.ChannelInstanceStore = (*SQLiteChannelInstanceStore)(nil)
