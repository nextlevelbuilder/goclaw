//go:build integration

package integration

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

func strPtr(s string) *string { return &s }

func TestStorePermission_GrantAndCheck(t *testing.T) {
	db := testDB(t)
	pg.InitSqlx(db)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	s := pg.NewPGConfigPermissionStore(db)

	perm := &store.ConfigPermission{
		AgentID:    agentID,
		Scope:      "group:telegram:-100123",
		ConfigType: "file_writer",
		UserID:     "user-42",
		Permission: "allow",
		GrantedBy:  strPtr("test-admin"),
	}
	if err := s.Grant(ctx, perm); err != nil {
		t.Fatalf("Grant: %v", err)
	}

	// CheckPermission — should be allowed.
	allowed, err := s.CheckPermission(ctx, agentID, "group:telegram:-100123", "file_writer", "user-42")
	if err != nil {
		t.Fatalf("CheckPermission: %v", err)
	}
	if !allowed {
		t.Error("expected allowed=true after grant")
	}

	// Revoke the permission.
	if err := s.Revoke(ctx, agentID, "group:telegram:-100123", "file_writer", "user-42"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	// CheckPermission — should now be denied (cache invalidated by Revoke/Grant).
	allowed2, err := s.CheckPermission(ctx, agentID, "group:telegram:-100123", "file_writer", "user-42")
	if err != nil {
		t.Fatalf("CheckPermission after revoke: %v", err)
	}
	if allowed2 {
		t.Error("expected allowed=false after revoke")
	}
}

func TestStorePermission_List(t *testing.T) {
	db := testDB(t)
	pg.InitSqlx(db)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	s := pg.NewPGConfigPermissionStore(db)

	perms := []store.ConfigPermission{
		{
			AgentID:    agentID,
			Scope:      "group:telegram:-100111",
			ConfigType: "file_writer",
			UserID:     "user-1",
			Permission: "allow",
			GrantedBy:  strPtr("test-admin"),
		},
		{
			AgentID:    agentID,
			Scope:      "group:telegram:-100222",
			ConfigType: "file_writer",
			UserID:     "user-2",
			Permission: "deny",
			GrantedBy:  strPtr("test-admin"),
		},
	}
	for i, p := range perms {
		cp := p
		if err := s.Grant(ctx, &cp); err != nil {
			t.Fatalf("Grant[%d]: %v", i, err)
		}
	}

	list, err := s.List(ctx, agentID, "file_writer", "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) < 2 {
		t.Errorf("expected at least 2 permissions, got %d", len(list))
	}
}
