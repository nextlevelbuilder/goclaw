//go:build integration

package integration

import (
	"encoding/json"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// TestPGMergeConfig_PreservesSiblingKeys verifies the PG MergeConfig shallow
// JSONB merge preserves sibling config keys.
func TestPGMergeConfig_PreservesSiblingKeys(t *testing.T) {
	db := testDB(t)
	tenant, agent := seedTenantAgent(t, db)
	ci := pg.NewPGChannelInstanceStore(db, testEncryptionKey)
	ctx := tenantCtx(tenant)

	inst := &store.ChannelInstanceData{
		TenantID:    tenant,
		Name:        "zalo-oa-merge-pg",
		ChannelType: "zalo_oa",
		AgentID:     agent,
		Config:      json.RawMessage(`{"dm_policy":"pairing","poll_cursor":{"evicted":1}}`),
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
	if _, exists := cursor["evicted"]; exists {
		t.Fatalf("evicted cursor survived shallow replacement: %#v", cursor)
	}
}

// TestPGMergeConfig_TenantIsolation verifies a tenant-scoped MergeConfig
// cannot mutate a foreign tenant's instance.
func TestPGMergeConfig_TenantIsolation(t *testing.T) {
	db := testDB(t)
	tenantA, agentA := seedTenantAgent(t, db)
	tenantB, _ := seedTenantAgent(t, db)
	ci := pg.NewPGChannelInstanceStore(db, testEncryptionKey)
	ctxA := tenantCtx(tenantA)
	ctxB := tenantCtx(tenantB)

	inst := &store.ChannelInstanceData{
		TenantID:    tenantA,
		Name:        "zalo-oa-iso-pg",
		ChannelType: "zalo_oa",
		AgentID:     agentA,
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

func TestPGMergeConfig_NullConfig(t *testing.T) {
	db := testDB(t)
	tenant, agent := seedTenantAgent(t, db)
	ci := pg.NewPGChannelInstanceStore(db, testEncryptionKey)
	ctx := tenantCtx(tenant)
	inst := &store.ChannelInstanceData{
		Name: "null-config", ChannelType: "zalo_oa",
		AgentID: agent, Config: json.RawMessage(`null`),
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
