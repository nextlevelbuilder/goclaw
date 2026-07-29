//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func seedSQLiteLinkAgent(t *testing.T, db *sql.DB, tenantID uuid.UUID, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := db.Exec(
		`INSERT INTO agents (id, tenant_id, agent_key, agent_type, status, provider, model, owner_id)
		 VALUES (?,?,?,'predefined','active','test','test-model','owner')`,
		id.String(), tenantID.String(), key)
	if err != nil {
		t.Fatalf("seed agent %q: %v", key, err)
	}
	return id
}

func newSQLiteLinkFixture(t *testing.T) (*SQLiteAgentLinkStore, *sql.DB, context.Context, uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	db := newHookTestDB(t)
	tenantID, agentA := seedHookTenantAgent(t, db)
	if _, err := db.Exec(`UPDATE agents SET agent_key = 'agent-a' WHERE id = ?`, agentA.String()); err != nil {
		t.Fatalf("rename agent A: %v", err)
	}
	agentB := seedSQLiteLinkAgent(t, db, tenantID, "agent-b")
	agentC := seedSQLiteLinkAgent(t, db, tenantID, "agent-c")
	ctx := store.WithTenantID(context.Background(), tenantID)
	return NewSQLiteAgentLinkStore(db), db, ctx, agentA, agentB, agentC
}

func sqliteLink(source, target uuid.UUID, direction string) *store.AgentLinkData {
	return &store.AgentLinkData{
		SourceAgentID: source,
		TargetAgentID: target,
		Direction:     direction,
		Status:        store.LinkStatusActive,
		MaxConcurrent: 1,
		CreatedBy:     "test",
	}
}

func targetKeys(t *testing.T, linkStore *SQLiteAgentLinkStore, ctx context.Context, from uuid.UUID) []string {
	t.Helper()
	targets, err := linkStore.DelegateTargets(ctx, from)
	if err != nil {
		t.Fatalf("DelegateTargets(%s): %v", from, err)
	}
	keys := make([]string, len(targets))
	for i := range targets {
		keys[i] = targets[i].TargetAgentKey
	}
	return keys
}

func TestSQLiteAgentLinkStoreDelegateTargetsDirections(t *testing.T) {
	tests := []struct {
		name      string
		direction string
		wantFromA []string
		wantFromB []string
	}{
		{
			name:      "outbound",
			direction: store.LinkDirectionOutbound,
			wantFromA: []string{"agent-b"},
		},
		{
			name:      "inbound",
			direction: store.LinkDirectionInbound,
			wantFromB: []string{"agent-a"},
		},
		{
			name:      "bidirectional",
			direction: store.LinkDirectionBidirectional,
			wantFromA: []string{"agent-b"},
			wantFromB: []string{"agent-a"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			linkStore, _, ctx, agentA, agentB, _ := newSQLiteLinkFixture(t)
			if err := linkStore.CreateLink(ctx, sqliteLink(agentA, agentB, tt.direction)); err != nil {
				t.Fatalf("CreateLink: %v", err)
			}

			if got := targetKeys(t, linkStore, ctx, agentA); !slicesEqual(got, tt.wantFromA) {
				t.Fatalf("targets from A = %v, want %v", got, tt.wantFromA)
			}
			if got := targetKeys(t, linkStore, ctx, agentB); !slicesEqual(got, tt.wantFromB) {
				t.Fatalf("targets from B = %v, want %v", got, tt.wantFromB)
			}
		})
	}
}

func TestSQLiteAgentLinkStoreDelegateTargetsRefreshesAfterMutations(t *testing.T) {
	linkStore, db, ctx, agentA, agentB, _ := newSQLiteLinkFixture(t)
	link := sqliteLink(agentA, agentB, store.LinkDirectionOutbound)
	if err := linkStore.CreateLink(ctx, link); err != nil {
		t.Fatalf("CreateLink: %v", err)
	}
	if got := targetKeys(t, linkStore, ctx, agentA); !slicesEqual(got, []string{"agent-b"}) {
		t.Fatalf("initial targets = %v", got)
	}

	if err := linkStore.UpdateLink(ctx, link.ID, map[string]any{"status": store.LinkStatusDisabled}); err != nil {
		t.Fatalf("disable link: %v", err)
	}
	if got := targetKeys(t, linkStore, ctx, agentA); len(got) != 0 {
		t.Fatalf("targets after disable = %v, want none", got)
	}

	if err := linkStore.UpdateLink(ctx, link.ID, map[string]any{
		"status":    store.LinkStatusActive,
		"direction": store.LinkDirectionInbound,
	}); err != nil {
		t.Fatalf("reverse link direction: %v", err)
	}
	if got := targetKeys(t, linkStore, ctx, agentA); len(got) != 0 {
		t.Fatalf("targets from A after inbound update = %v, want none", got)
	}
	if got := targetKeys(t, linkStore, ctx, agentB); !slicesEqual(got, []string{"agent-a"}) {
		t.Fatalf("targets from B after inbound update = %v", got)
	}

	if _, err := db.Exec(`UPDATE agents SET status = 'inactive' WHERE id = ?`, agentA.String()); err != nil {
		t.Fatalf("deactivate effective target: %v", err)
	}
	if got := targetKeys(t, linkStore, ctx, agentB); len(got) != 0 {
		t.Fatalf("targets with inactive effective target = %v, want none", got)
	}

	if _, err := db.Exec(`UPDATE agents SET status = 'active' WHERE id = ?`, agentA.String()); err != nil {
		t.Fatalf("reactivate target: %v", err)
	}
	if err := linkStore.DeleteLink(ctx, link.ID); err != nil {
		t.Fatalf("DeleteLink: %v", err)
	}
	if got := targetKeys(t, linkStore, ctx, agentB); len(got) != 0 {
		t.Fatalf("targets after delete = %v, want none", got)
	}
}

func TestSQLiteAgentLinkStoreDelegateTargetsTenantScoped(t *testing.T) {
	linkStore, _, ctx, agentA, agentB, _ := newSQLiteLinkFixture(t)
	if err := linkStore.CreateLink(ctx, sqliteLink(agentA, agentB, store.LinkDirectionOutbound)); err != nil {
		t.Fatalf("CreateLink: %v", err)
	}

	otherTenantCtx := store.WithTenantID(context.Background(), uuid.New())
	if got := targetKeys(t, linkStore, otherTenantCtx, agentA); len(got) != 0 {
		t.Fatalf("cross-tenant targets = %v, want none", got)
	}
}

func TestSQLiteAgentLinkStoreGetLinkBetweenNotFound(t *testing.T) {
	linkStore, _, ctx, agentA, agentB, _ := newSQLiteLinkFixture(t)

	link, err := linkStore.GetLinkBetween(ctx, agentA, agentB)
	if err != nil {
		t.Fatalf("GetLinkBetween: %v", err)
	}
	if link != nil {
		t.Fatalf("GetLinkBetween = %#v, want nil", link)
	}
}

func TestSQLiteAgentLinkStoreGetLinkBetweenPrefersDelegatorAuthoredRow(t *testing.T) {
	linkStore, _, ctx, agentA, agentB, _ := newSQLiteLinkFixture(t)

	reverse := sqliteLink(agentB, agentA, store.LinkDirectionInbound)
	if err := linkStore.CreateLink(ctx, reverse); err != nil {
		t.Fatalf("CreateLink(reverse): %v", err)
	}
	direct := sqliteLink(agentA, agentB, store.LinkDirectionOutbound)
	if err := linkStore.CreateLink(ctx, direct); err != nil {
		t.Fatalf("CreateLink(direct): %v", err)
	}

	link, err := linkStore.GetLinkBetween(ctx, agentA, agentB)
	if err != nil {
		t.Fatalf("GetLinkBetween: %v", err)
	}
	if link == nil {
		t.Fatal("GetLinkBetween returned nil")
	}
	if link.ID != direct.ID {
		t.Fatalf("GetLinkBetween chose %s, want delegator-authored %s", link.ID, direct.ID)
	}
}

func TestSQLiteAgentLinkStoreGetLinkBetweenPropagatesDatabaseError(t *testing.T) {
	linkStore, db, ctx, agentA, agentB, _ := newSQLiteLinkFixture(t)
	if err := db.Close(); err != nil {
		t.Fatalf("close fixture DB: %v", err)
	}

	if _, err := linkStore.GetLinkBetween(ctx, agentA, agentB); err == nil {
		t.Fatal("GetLinkBetween error = nil after database close")
	}
}

func slicesEqual(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
