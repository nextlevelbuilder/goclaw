package pg

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestPGListUserTeamIDsTenantAndActiveIsolation(t *testing.T) {
	db := hooksTestDB(t)
	tenantA, leadID := seedTenantAndAgent(t, db)
	tenantB := uuid.New()
	if _, err := db.Exec(`INSERT INTO tenants(id,name,slug,status,settings) VALUES($1,$2,$3,'active','{}')`, tenantB, "Tenant B", "tenant-b-"+tenantB.String()); err != nil {
		t.Fatal(err)
	}

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
		if _, err := db.Exec(`INSERT INTO agent_teams(id,name,lead_agent_id,status,settings,created_by,tenant_id)
			VALUES($1,$2,$3,$4,'{}','owner',$5)`, fixture.id, fixture.id.String(), leadID, fixture.status, fixture.tenantID); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO team_user_grants(id,team_id,user_id,role,granted_by,tenant_id)
			VALUES($1,$2,$3,'viewer','owner',$4)`, uuid.New(), fixture.id, userID, fixture.tenantID); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM agent_teams WHERE id = ANY($1)`, []uuid.UUID{activeA, archivedA, activeB})
		_, _ = db.Exec(`DELETE FROM tenants WHERE id=$1`, tenantB)
	})

	s := NewPGTeamStore(db)
	got, err := s.ListUserTeamIDs(store.WithTenantID(context.Background(), tenantA), userID)
	if err != nil {
		t.Fatalf("ListUserTeamIDs tenant A: %v", err)
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

	got, err = s.ListUserTeamIDs(store.WithCrossTenant(store.WithTenantID(context.Background(), tenantA)), userID)
	if err != nil {
		t.Fatalf("ListUserTeamIDs cross-tenant: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("cross-tenant IDs = %v, want none", got)
	}
}
