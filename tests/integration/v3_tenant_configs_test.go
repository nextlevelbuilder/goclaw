//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

func TestStoreTenantConfig_DisableAndListTools(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	s := pg.NewPGBuiltinToolTenantConfigStore(db)
	pg.InitSqlx(db)
	ctx := context.Background()

	// Seed builtin tools for FK
	for _, tool := range []string{"web_search", "exec_cmd", "read_file"} {
		db.Exec(`INSERT INTO builtin_tools (name, display_name) VALUES ($1, $1) ON CONFLICT DO NOTHING`, tool)
	}

	t.Cleanup(func() {
		db.Exec("DELETE FROM builtin_tool_tenant_configs WHERE tenant_id = $1", tenantID)
	})

	// Disable two tools
	if err := s.Set(ctx, tenantID, "web_search", false); err != nil {
		t.Fatalf("Set web_search: %v", err)
	}
	if err := s.Set(ctx, tenantID, "exec_cmd", false); err != nil {
		t.Fatalf("Set exec_cmd: %v", err)
	}
	// Enable one tool explicitly
	if err := s.Set(ctx, tenantID, "read_file", true); err != nil {
		t.Fatalf("Set read_file: %v", err)
	}

	// ListDisabled — should return 2
	disabled, err := s.ListDisabled(ctx, tenantID)
	if err != nil {
		t.Fatalf("ListDisabled: %v", err)
	}
	if len(disabled) != 2 {
		t.Errorf("ListDisabled len = %d, want 2", len(disabled))
	}
	disabledSet := make(map[string]bool)
	for _, d := range disabled {
		disabledSet[d] = true
	}
	if !disabledSet["web_search"] || !disabledSet["exec_cmd"] {
		t.Errorf("expected web_search and exec_cmd in disabled, got %v", disabled)
	}

	// ListAll — should return 3 entries
	all, err := s.ListAll(ctx, tenantID)
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("ListAll len = %d, want 3", len(all))
	}
	if all["read_file"] != true {
		t.Error("expected read_file=true")
	}
	if all["web_search"] != false {
		t.Error("expected web_search=false")
	}

	// Delete and verify
	if err := s.Delete(ctx, tenantID, "web_search"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	disabled2, _ := s.ListDisabled(ctx, tenantID)
	if len(disabled2) != 1 {
		t.Errorf("after delete, ListDisabled len = %d, want 1", len(disabled2))
	}
}

func TestStoreTenantConfig_DisableAndListSkills(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	skillStore := pg.NewPGSkillStore(db, t.TempDir())
	pg.InitSqlx(db)

	// Create two skills to disable
	desc := "test skill"
	slug1 := "cfg-skill-1-" + tenantID.String()[:8]
	skillID1, err := skillStore.CreateSkillManaged(ctx, store.SkillCreateParams{
		Name: slug1, Slug: slug1, Description: &desc, OwnerID: "test-owner",
		Visibility: "private", Status: "active", Version: 1, FilePath: "/tmp/" + slug1,
	})
	if err != nil {
		t.Fatalf("CreateSkill 1: %v", err)
	}
	slug2 := "cfg-skill-2-" + tenantID.String()[:8]
	skillID2, err := skillStore.CreateSkillManaged(ctx, store.SkillCreateParams{
		Name: slug2, Slug: slug2, Description: &desc, OwnerID: "test-owner",
		Visibility: "private", Status: "active", Version: 1, FilePath: "/tmp/" + slug2,
	})
	if err != nil {
		t.Fatalf("CreateSkill 2: %v", err)
	}

	s := pg.NewPGSkillTenantConfigStore(db)
	bgCtx := context.Background()

	t.Cleanup(func() {
		db.Exec("DELETE FROM skill_tenant_configs WHERE tenant_id = $1", tenantID)
	})

	// Disable both skills
	if err := s.Set(bgCtx, tenantID, skillID1, false); err != nil {
		t.Fatalf("Set skill1: %v", err)
	}
	if err := s.Set(bgCtx, tenantID, skillID2, false); err != nil {
		t.Fatalf("Set skill2: %v", err)
	}

	// ListDisabledSkillIDs
	disabled, err := s.ListDisabledSkillIDs(bgCtx, tenantID)
	if err != nil {
		t.Fatalf("ListDisabledSkillIDs: %v", err)
	}
	if len(disabled) != 2 {
		t.Errorf("ListDisabledSkillIDs len = %d, want 2", len(disabled))
	}

	// ListAll
	all, err := s.ListAll(bgCtx, tenantID)
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("ListAll len = %d, want 2", len(all))
	}
	if all[skillID1] != false {
		t.Error("expected skillID1=false")
	}
}

func TestStoreTenantConfig_TenantIsolation(t *testing.T) {
	db := testDB(t)
	tenantA, _ := seedTenantAgent(t, db)
	tenantB, _ := seedTenantAgent(t, db)
	s := pg.NewPGBuiltinToolTenantConfigStore(db)
	pg.InitSqlx(db)
	ctx := context.Background()

	t.Cleanup(func() {
		db.Exec("DELETE FROM builtin_tool_tenant_configs WHERE tenant_id = $1", tenantA)
		db.Exec("DELETE FROM builtin_tool_tenant_configs WHERE tenant_id = $1", tenantB)
	})

	// Seed builtin tool for FK
	db.Exec(`INSERT INTO builtin_tools (name, display_name) VALUES ('dangerous_tool', 'dangerous_tool') ON CONFLICT DO NOTHING`)

	// Disable tool in tenant A
	if err := s.Set(ctx, tenantA, "dangerous_tool", false); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Tenant B should not see it
	disabledB, err := s.ListDisabled(ctx, tenantB)
	if err != nil {
		t.Fatalf("ListDisabled B: %v", err)
	}
	if len(disabledB) != 0 {
		t.Errorf("tenant B sees %d disabled tools — isolation broken", len(disabledB))
	}

	// Tenant A should see it
	disabledA, err := s.ListDisabled(ctx, tenantA)
	if err != nil {
		t.Fatalf("ListDisabled A: %v", err)
	}
	if len(disabledA) != 1 {
		t.Errorf("tenant A sees %d disabled tools, want 1", len(disabledA))
	}
}
