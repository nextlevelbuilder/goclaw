//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestSQLiteListUserTeamIDsTenantAndActiveIsolation(t *testing.T) {
	s, ctx, tenantA, leadID := sqliteAuditFixture(t)
	tenantB := uuid.New()
	mustExec(t, s.db, `INSERT INTO tenants(id,name,slug,status,settings) VALUES(?,?,?,'active','{}')`, tenantB, "Tenant B", "tenant-b")

	userID := "user-a"
	activeA, archivedA, activeB := uuid.New(), uuid.New(), uuid.New()
	for _, fixture := range []struct {
		id, tenantID uuid.UUID
		status       string
	}{
		{id: activeA, tenantID: tenantA, status: store.TeamStatusActive},
		{id: archivedA, tenantID: tenantA, status: store.TeamStatusArchived},
		{id: activeB, tenantID: tenantB, status: store.TeamStatusActive},
	} {
		mustExec(t, s.db, `INSERT INTO agent_teams(id,name,lead_agent_id,status,settings,created_by,tenant_id)
			VALUES(?,?,?,?,'{}','owner',?)`, fixture.id, fixture.id.String(), leadID, fixture.status, fixture.tenantID)
		mustExec(t, s.db, `INSERT INTO team_user_grants(id,team_id,user_id,role,granted_by,tenant_id)
			VALUES(?,?,?,'viewer','owner',?)`, uuid.New(), fixture.id, userID, fixture.tenantID)
	}

	got, err := s.ListUserTeamIDs(ctx, userID)
	if err != nil {
		t.Fatalf("ListUserTeamIDs: %v", err)
	}
	if len(got) != 1 || got[0] != activeA {
		t.Fatalf("tenant A IDs = %v, want [%s]", got, activeA)
	}

	got, err = s.ListUserTeamIDs(store.WithTenantID(context.Background(), tenantB), userID)
	if err != nil {
		t.Fatalf("ListUserTeamIDs tenant B: %v", err)
	}
	if len(got) != 1 || got[0] != activeB {
		t.Fatalf("tenant B IDs = %v, want [%s]", got, activeB)
	}

	got, err = s.ListUserTeamIDs(context.Background(), userID)
	if err != nil {
		t.Fatalf("ListUserTeamIDs unscoped: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("unscoped IDs = %v, want none", got)
	}

	got, err = s.ListUserTeamIDs(store.WithCrossTenant(ctx), userID)
	if err != nil {
		t.Fatalf("ListUserTeamIDs cross-tenant: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("cross-tenant IDs = %v, want none", got)
	}
}
