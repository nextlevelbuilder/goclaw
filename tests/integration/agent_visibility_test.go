//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// Removing a member must hand their agents to the workspace.
//
// The failure this prevents is silent and nasty: a personal agent is invisible to
// everyone but its owner, so removing the owner leaves rows NOBODY can see which
// keep firing on whatever schedule they were armed with. Nothing errors, nothing
// logs, and the only symptom is an unexplained run and a bill.
func TestRemovingAMemberHandsTheirAgentsToTheOrg(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	tenantID := uuid.Must(uuid.NewV7())
	if _, err := db.Exec(
		`INSERT INTO tenants (id, name, slug, status) VALUES ($1, 'vis', $2, 'active')`,
		tenantID, "vis-"+tenantID.String()[:8]); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM agents WHERE tenant_id = $1", tenantID)
		db.Exec("DELETE FROM tenant_users WHERE tenant_id = $1", tenantID)
		db.Exec("DELETE FROM tenants WHERE id = $1", tenantID)
	})

	const leaver, stayer = "user-leaving", "user-staying"
	for _, u := range []string{leaver, stayer} {
		if _, err := db.Exec(
			`INSERT INTO tenant_users (tenant_id, user_id, role) VALUES ($1, $2, 'member')`,
			tenantID, u); err != nil {
			t.Fatalf("add member %s: %v", u, err)
		}
	}

	newAgent := func(owner, key string) uuid.UUID {
		id := uuid.Must(uuid.NewV7())
		if _, err := db.Exec(
			`INSERT INTO agents (id, agent_key, display_name, owner_id, tenant_id, provider, model, workspace, status)
			 VALUES ($1, $2, $2, $3, $4, 'llm-service', 'x', '/tmp/ws', 'active')`,
			id, key+"-"+id.String()[:8], owner, tenantID); err != nil {
			t.Fatalf("create agent for %s: %v", owner, err)
		}
		return id
	}
	leaverAgent := newAgent(leaver, "leaver-agent")
	stayerAgent := newAgent(stayer, "stayer-agent")

	agents := pg.NewPGAgentStore(db)
	scoped := store.WithTenantID(ctx, tenantID)

	// BEFORE: the stayer cannot see the leaver's agent. That is the privacy
	// property this whole change is protecting; if it were already visible the
	// test below would pass for the wrong reason.
	visible, err := agents.ListAccessible(scoped, stayer)
	if err != nil {
		t.Fatalf("list before: %v", err)
	}
	if containsAgent(visible, leaverAgent) {
		t.Fatal("a member could already see a colleague's private agent — the privacy fix is not in effect")
	}

	tenants := pg.NewPGTenantStore(db)
	if err := tenants.RemoveUser(ctx, tenantID, leaver); err != nil {
		t.Fatalf("remove user: %v", err)
	}

	// AFTER: the workspace inherits it.
	visible, err = agents.ListAccessible(scoped, stayer)
	if err != nil {
		t.Fatalf("list after: %v", err)
	}
	if !containsAgent(visible, leaverAgent) {
		t.Error("the leaver's agent is visible to nobody but still exists — it will keep firing if armed")
	}
	if !containsAgent(visible, stayerAgent) {
		t.Error("the remaining member lost sight of their OWN agent")
	}

	// The membership row is gone, and the two halves committed together.
	var members int
	if err := db.QueryRow(
		`SELECT count(*) FROM tenant_users WHERE tenant_id = $1 AND user_id = $2`,
		tenantID, leaver).Scan(&members); err != nil {
		t.Fatalf("count members: %v", err)
	}
	if members != 0 {
		t.Errorf("member row survived removal (%d)", members)
	}

	// Only the LEAVER's agents were touched.
	var stayerVisibility string
	if err := db.QueryRow(`SELECT visibility FROM agents WHERE id = $1`, stayerAgent).Scan(&stayerVisibility); err != nil {
		t.Fatalf("read stayer visibility: %v", err)
	}
	if stayerVisibility != "private" {
		t.Errorf("a remaining member's agent was published to the org (visibility=%q)", stayerVisibility)
	}
}

func containsAgent(list []store.AgentData, id uuid.UUID) bool {
	for _, a := range list {
		if a.ID == id {
			return true
		}
	}
	return false
}
