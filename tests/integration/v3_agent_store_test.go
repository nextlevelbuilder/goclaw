//go:build integration

package integration

import (
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

func newTestAgent(tenantID uuid.UUID, suffix string) store.AgentData {
	id := uuid.New()
	if suffix == "" {
		suffix = id.String()[:8]
	}
	return store.AgentData{
		BaseModel: store.BaseModel{ID: id},
		TenantID:  tenantID,
		AgentKey:  "test-agent-" + suffix,
		AgentType: "predefined",
		Status:    "active",
		Provider:  "test-provider",
		Model:     "test-model",
		OwnerID:   "test-owner",
	}
}

func TestStoreAgent_CreateAndGet(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	as := pg.NewPGAgentStore(db)

	agent := newTestAgent(tenantID, uuid.New().String()[:8])
	agent.DisplayName = "Test Agent"

	if err := as.Create(ctx, &agent); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM agents WHERE id = $1", agent.ID) })

	// GetByKey.
	got, err := as.GetByKey(ctx, agent.AgentKey)
	if err != nil {
		t.Fatalf("GetByKey: %v", err)
	}
	if got.AgentKey != agent.AgentKey {
		t.Errorf("AgentKey: expected %q, got %q", agent.AgentKey, got.AgentKey)
	}
	if got.DisplayName != agent.DisplayName {
		t.Errorf("DisplayName: expected %q, got %q", agent.DisplayName, got.DisplayName)
	}
	if got.Model != agent.Model {
		t.Errorf("Model: expected %q, got %q", agent.Model, got.Model)
	}
	if got.Provider != agent.Provider {
		t.Errorf("Provider: expected %q, got %q", agent.Provider, got.Provider)
	}
	if got.AgentType != agent.AgentType {
		t.Errorf("AgentType: expected %q, got %q", agent.AgentType, got.AgentType)
	}

	// GetByID.
	got2, err := as.GetByID(ctx, agent.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got2.ID != agent.ID {
		t.Errorf("GetByID: ID mismatch: expected %v, got %v", agent.ID, got2.ID)
	}
}

func TestStoreAgent_Update(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	as := pg.NewPGAgentStore(db)

	agent := newTestAgent(tenantID, uuid.New().String()[:8])
	if err := as.Create(ctx, &agent); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM agents WHERE id = $1", agent.ID) })

	// Update display_name.
	if err := as.Update(ctx, agent.ID, map[string]any{"display_name": "Updated Name"}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := as.GetByID(ctx, agent.ID)
	if err != nil {
		t.Fatalf("GetByID after update: %v", err)
	}
	if got.DisplayName != "Updated Name" {
		t.Errorf("DisplayName after update: expected %q, got %q", "Updated Name", got.DisplayName)
	}
}

func TestStoreAgent_Delete(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	as := pg.NewPGAgentStore(db)

	agent := newTestAgent(tenantID, uuid.New().String()[:8])
	if err := as.Create(ctx, &agent); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Delete (hard delete).
	if err := as.Delete(ctx, agent.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// GetByKey should return error.
	if _, err := as.GetByKey(ctx, agent.AgentKey); err == nil {
		t.Error("after Delete: GetByKey expected error, got nil")
	}

	// DB row should be gone.
	var count int
	db.QueryRow("SELECT COUNT(*) FROM agents WHERE id = $1", agent.ID).Scan(&count)
	if count != 0 {
		t.Errorf("after Delete: expected 0 DB rows, got %d", count)
	}
}

func TestStoreAgent_List(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	as := pg.NewPGAgentStore(db)

	var user1Agents, user2Agents []uuid.UUID

	// Create 3 agents for user1.
	for i := 0; i < 3; i++ {
		a := newTestAgent(tenantID, uuid.New().String()[:8])
		a.OwnerID = "user1"
		if err := as.Create(ctx, &a); err != nil {
			t.Fatalf("Create user1 agent %d: %v", i, err)
		}
		user1Agents = append(user1Agents, a.ID)
	}

	// Create 2 agents for user2.
	for i := 0; i < 2; i++ {
		a := newTestAgent(tenantID, uuid.New().String()[:8])
		a.OwnerID = "user2"
		if err := as.Create(ctx, &a); err != nil {
			t.Fatalf("Create user2 agent %d: %v", i, err)
		}
		user2Agents = append(user2Agents, a.ID)
	}

	t.Cleanup(func() {
		for _, id := range append(user1Agents, user2Agents...) {
			db.Exec("DELETE FROM agents WHERE id = $1", id)
		}
	})

	// List user1 — should have exactly 3.
	list1, err := as.List(ctx, "user1")
	if err != nil {
		t.Fatalf("List user1: %v", err)
	}
	// Count only our test agents (filter by our known IDs).
	count1 := countInList(list1, user1Agents)
	if count1 != 3 {
		t.Errorf("List user1: expected 3 test agents, got %d", count1)
	}

	// List user2 — should have exactly 2.
	list2, err := as.List(ctx, "user2")
	if err != nil {
		t.Fatalf("List user2: %v", err)
	}
	count2 := countInList(list2, user2Agents)
	if count2 != 2 {
		t.Errorf("List user2: expected 2 test agents, got %d", count2)
	}
}

// countInList counts how many agents from ids appear in the list.
func countInList(list []store.AgentData, ids []uuid.UUID) int {
	set := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	count := 0
	for _, a := range list {
		if set[a.ID] {
			count++
		}
	}
	return count
}

func TestStoreAgent_DefaultAgent(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	as := pg.NewPGAgentStore(db)

	// Create agent A as default.
	agentA := newTestAgent(tenantID, uuid.New().String()[:8])
	agentA.IsDefault = true
	if err := as.Create(ctx, &agentA); err != nil {
		t.Fatalf("Create agentA: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM agents WHERE id = $1", agentA.ID) })

	// GetDefault should return A.
	def, err := as.GetDefault(ctx)
	if err != nil {
		t.Fatalf("GetDefault: %v", err)
	}
	if def.ID != agentA.ID {
		t.Errorf("GetDefault: expected agentA (%v), got %v", agentA.ID, def.ID)
	}

	// Create agent B, update B to be default.
	agentB := newTestAgent(tenantID, uuid.New().String()[:8])
	if err := as.Create(ctx, &agentB); err != nil {
		t.Fatalf("Create agentB: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM agents WHERE id = $1", agentB.ID) })

	if err := as.Update(ctx, agentB.ID, map[string]any{"is_default": true}); err != nil {
		t.Fatalf("Update agentB is_default: %v", err)
	}

	// GetDefault should return B.
	def2, err := as.GetDefault(ctx)
	if err != nil {
		t.Fatalf("GetDefault after B set: %v", err)
	}
	if def2.ID != agentB.ID {
		t.Errorf("GetDefault: expected agentB (%v), got %v", agentB.ID, def2.ID)
	}

	// GetByID(A) should have IsDefault=false.
	gotA, err := as.GetByID(ctx, agentA.ID)
	if err != nil {
		t.Fatalf("GetByID agentA: %v", err)
	}
	if gotA.IsDefault {
		t.Error("agentA.IsDefault should be false after B was set as default")
	}
}

func TestStoreAgent_ShareAndAccess(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	as := pg.NewPGAgentStore(db)

	agent := newTestAgent(tenantID, uuid.New().String()[:8])
	agent.OwnerID = "user1"
	if err := as.Create(ctx, &agent); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM agent_shares WHERE agent_id = $1", agent.ID)
		db.Exec("DELETE FROM agents WHERE id = $1", agent.ID)
	})

	// Share with user2 as viewer.
	if err := as.ShareAgent(ctx, agent.ID, "user2", "viewer", "user1"); err != nil {
		t.Fatalf("ShareAgent: %v", err)
	}

	// CanAccess user2 — should be true with role viewer.
	ok, role, err := as.CanAccess(ctx, agent.ID, "user2")
	if err != nil {
		t.Fatalf("CanAccess user2: %v", err)
	}
	if !ok {
		t.Error("CanAccess user2: expected true")
	}
	if role != "viewer" {
		t.Errorf("CanAccess user2 role: expected %q, got %q", "viewer", role)
	}

	// CanAccess user3 — should be false (not shared).
	ok3, _, err3 := as.CanAccess(ctx, agent.ID, "user3")
	if err3 != nil {
		t.Fatalf("CanAccess user3: %v", err3)
	}
	if ok3 {
		t.Error("CanAccess user3: expected false for unshared user")
	}
}

func TestStoreAgent_RevokeShare(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	as := pg.NewPGAgentStore(db)

	agent := newTestAgent(tenantID, uuid.New().String()[:8])
	agent.OwnerID = "user1"
	if err := as.Create(ctx, &agent); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM agent_shares WHERE agent_id = $1", agent.ID)
		db.Exec("DELETE FROM agents WHERE id = $1", agent.ID)
	})

	// Share then revoke.
	if err := as.ShareAgent(ctx, agent.ID, "user2", "viewer", "user1"); err != nil {
		t.Fatalf("ShareAgent: %v", err)
	}
	if err := as.RevokeShare(ctx, agent.ID, "user2"); err != nil {
		t.Fatalf("RevokeShare: %v", err)
	}

	// CanAccess should be false.
	ok, _, err := as.CanAccess(ctx, agent.ID, "user2")
	if err != nil {
		t.Fatalf("CanAccess after revoke: %v", err)
	}
	if ok {
		t.Error("CanAccess after revoke: expected false")
	}
}

func TestStoreAgent_ListAccessible(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	as := pg.NewPGAgentStore(db)

	// Agent A — owned by user1.
	agentA := newTestAgent(tenantID, uuid.New().String()[:8])
	agentA.OwnerID = "list-user1"
	if err := as.Create(ctx, &agentA); err != nil {
		t.Fatalf("Create agentA: %v", err)
	}

	// Agent B — is_default=true.
	agentB := newTestAgent(tenantID, uuid.New().String()[:8])
	agentB.IsDefault = true
	agentB.OwnerID = "list-other"
	if err := as.Create(ctx, &agentB); err != nil {
		t.Fatalf("Create agentB: %v", err)
	}

	// Agent C — shared to user1.
	agentC := newTestAgent(tenantID, uuid.New().String()[:8])
	agentC.OwnerID = "list-other2"
	if err := as.Create(ctx, &agentC); err != nil {
		t.Fatalf("Create agentC: %v", err)
	}
	if err := as.ShareAgent(ctx, agentC.ID, "list-user1", "viewer", "list-other2"); err != nil {
		t.Fatalf("ShareAgent: %v", err)
	}

	t.Cleanup(func() {
		db.Exec("DELETE FROM agent_shares WHERE agent_id = $1", agentC.ID)
		for _, id := range []uuid.UUID{agentA.ID, agentB.ID, agentC.ID} {
			db.Exec("DELETE FROM agents WHERE id = $1", id)
		}
	})

	// ListAccessible for user1 should include A, B, C.
	list, err := as.ListAccessible(ctx, "list-user1")
	if err != nil {
		t.Fatalf("ListAccessible: %v", err)
	}

	found := map[uuid.UUID]bool{}
	for _, a := range list {
		found[a.ID] = true
	}
	if !found[agentA.ID] {
		t.Error("ListAccessible: agentA (owned) not found")
	}
	if !found[agentB.ID] {
		t.Error("ListAccessible: agentB (default) not found")
	}
	if !found[agentC.ID] {
		t.Error("ListAccessible: agentC (shared) not found")
	}
}

func TestStoreAgent_TenantIsolation(t *testing.T) {
	db := testDB(t)
	tenantA, _, tenantB, _ := seedTwoTenants(t, db)
	ctxA := tenantCtx(tenantA)
	ctxB := tenantCtx(tenantB)
	as := pg.NewPGAgentStore(db)

	// Create agent in tenant A.
	agent := newTestAgent(tenantA, uuid.New().String()[:8])
	if err := as.Create(ctxA, &agent); err != nil {
		t.Fatalf("Create in tenant A: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM agents WHERE id = $1", agent.ID) })

	// GetByKey from tenant B — should return error (nil/not found).
	if got, err := as.GetByKey(ctxB, agent.AgentKey); err == nil && got != nil {
		t.Error("tenant isolation broken: tenant B got agent created in tenant A")
	}
}

func TestStoreAgent_CrossTenantMode(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctxA := tenantCtx(tenantID)
	as := pg.NewPGAgentStore(db)

	// Create agent in tenant A.
	agent := newTestAgent(tenantID, uuid.New().String()[:8])
	if err := as.Create(ctxA, &agent); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM agents WHERE id = $1", agent.ID) })

	// GetByID with crossTenantCtx should return the agent.
	got, err := as.GetByID(crossTenantCtx(), agent.ID)
	if err != nil {
		t.Fatalf("GetByID crossTenant: %v", err)
	}
	if got.ID != agent.ID {
		t.Errorf("crossTenant: expected ID %v, got %v", agent.ID, got.ID)
	}
}
