//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// End-to-end for the share-with-org toggle: a personal agent, flipped to 'org' via
// the same UPDATE the WS handler issues, becomes visible to a colleague through
// ListAccessible — the actual code path a page load takes, not an assumption about
// what the visibility='org' clause added in migration 000084 does.
//
// The unit tests in internal/gateway/methods pin the AUTHORIZATION (owner-only,
// never locked, only private/org) against a stub store. This is the one thing a
// stub cannot prove: that the column ListAccessible reads is the SAME one this
// write lands in.
func TestVisibilityToggleMakesAgentVisibleToColleague(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	tenantID := uuid.Must(uuid.NewV7())
	if _, err := db.Exec(
		`INSERT INTO tenants (id, name, slug, status) VALUES ($1, 'vt', $2, 'active')`,
		tenantID, "vt-"+tenantID.String()[:8]); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM agents WHERE tenant_id = $1", tenantID)
		db.Exec("DELETE FROM tenants WHERE id = $1", tenantID)
	})

	const owner, colleague = "owner-1", "colleague-1"
	agentID := uuid.Must(uuid.NewV7())
	if _, err := db.Exec(
		`INSERT INTO agents (id, agent_key, display_name, owner_id, tenant_id, provider, model, workspace, status)
		 VALUES ($1, 'my-researcher', 'My Researcher', $2, $3, 'llm-service', 'x', '/tmp/ws', 'active')`,
		agentID, owner, tenantID); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	agents := pg.NewPGAgentStore(db)
	scoped := store.WithTenantID(ctx, tenantID)

	// BEFORE: private by default. A colleague must not see it — this is the
	// invariant the whole private-agents change protects.
	visible, err := agents.ListAccessible(scoped, colleague)
	if err != nil {
		t.Fatalf("list before: %v", err)
	}
	if containsAgent(visible, agentID) {
		t.Fatal("a personal agent was visible to a colleague before it was shared")
	}

	// The toggle: exactly the write agents_update.go issues for visibility='org'.
	if err := agents.Update(scoped, agentID, map[string]any{"visibility": "org"}); err != nil {
		t.Fatalf("toggle to org: %v", err)
	}

	// AFTER: now visible to the colleague.
	visible, err = agents.ListAccessible(scoped, colleague)
	if err != nil {
		t.Fatalf("list after: %v", err)
	}
	if !containsAgent(visible, agentID) {
		t.Error("agent set to visibility='org' is still not visible to a colleague")
	}

	// And the owner still sees it — sharing is additive, not a handoff.
	visible, err = agents.ListAccessible(scoped, owner)
	if err != nil {
		t.Fatalf("list for owner: %v", err)
	}
	if !containsAgent(visible, agentID) {
		t.Error("the owner lost sight of their own agent after sharing it")
	}

	// Toggling back to private must revoke the colleague's view — this is not a
	// one-way door.
	if err := agents.Update(scoped, agentID, map[string]any{"visibility": "private"}); err != nil {
		t.Fatalf("toggle back to private: %v", err)
	}
	visible, err = agents.ListAccessible(scoped, colleague)
	if err != nil {
		t.Fatalf("list after revert: %v", err)
	}
	if containsAgent(visible, agentID) {
		t.Error("agent reverted to visibility='private' is still visible to the colleague")
	}
}
