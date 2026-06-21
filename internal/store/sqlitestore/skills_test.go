//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestSQLiteSkillStore_StoreMissingDeps_PersistsForCustomSkills(t *testing.T) {
	ctx, skillStore := newTestSQLiteSkillStore(t)
	skillID, err := skillStore.CreateSkillManaged(ctx, store.SkillCreateParams{
		Name:       "Custom Skill",
		Slug:       "custom-skill",
		OwnerID:    "user-1",
		Visibility: "private",
		FilePath:   filepath.Join(t.TempDir(), "custom-skill", "1"),
	})
	if err != nil {
		t.Fatalf("CreateSkillManaged error: %v", err)
	}

	missing := []string{"pip:requests", "npm:tsx"}
	if err := skillStore.StoreMissingDeps(ctx, skillID, missing); err != nil {
		t.Fatalf("StoreMissingDeps error: %v", err)
	}

	info, ok := skillStore.GetSkillByID(ctx, skillID)
	if !ok {
		t.Fatal("GetSkillByID returned !ok")
	}
	if !reflect.DeepEqual(info.MissingDeps, missing) {
		t.Fatalf("MissingDeps = %v, want %v", info.MissingDeps, missing)
	}
}

func TestSQLiteSkillStore_CreateSkillManaged_PersistsArchivedDependencyState(t *testing.T) {
	ctx, skillStore := newTestSQLiteSkillStore(t)
	missing := []string{"pip:requests"}

	skillID, err := skillStore.CreateSkillManaged(ctx, store.SkillCreateParams{
		Name:        "Archived Skill",
		Slug:        "archived-skill",
		OwnerID:     "user-1",
		Visibility:  "private",
		Status:      "archived",
		MissingDeps: missing,
		FilePath:    filepath.Join(t.TempDir(), "archived-skill", "1"),
	})
	if err != nil {
		t.Fatalf("CreateSkillManaged error: %v", err)
	}

	info, ok := skillStore.GetSkillByID(ctx, skillID)
	if !ok {
		t.Fatal("GetSkillByID returned !ok")
	}
	if info.Status != "archived" {
		t.Fatalf("Status = %q, want archived", info.Status)
	}
	if !reflect.DeepEqual(info.MissingDeps, missing) {
		t.Fatalf("MissingDeps = %v, want %v", info.MissingDeps, missing)
	}
}

func TestSQLiteSkillStore_GetSkill_UUIDCanReadArchivedSlugStaysActiveOnly(t *testing.T) {
	ctx, skillStore := newTestSQLiteSkillStore(t)
	skillID, err := skillStore.CreateSkillManaged(ctx, store.SkillCreateParams{
		Name:       "Archived Detail",
		Slug:       "archived-detail",
		OwnerID:    "user-1",
		Visibility: "private",
		Status:     "archived",
		FilePath:   filepath.Join(t.TempDir(), "archived-detail", "1"),
	})
	if err != nil {
		t.Fatalf("CreateSkillManaged error: %v", err)
	}

	if _, ok := skillStore.GetSkill(ctx, "archived-detail"); ok {
		t.Fatal("GetSkill by slug returned archived skill; want active-only slug lookup")
	}
	info, ok := skillStore.GetSkill(ctx, skillID.String())
	if !ok {
		t.Fatal("GetSkill by UUID returned !ok for archived skill")
	}
	if info.Status != "archived" {
		t.Fatalf("Status = %q, want archived", info.Status)
	}
}

func TestSQLiteSkillStore_ListSkills_ResolvesCreatorAgentWithinTenant(t *testing.T) {
	_, skillStore, db := newTestSQLiteSkillStoreWithDB(t)
	tenantID, agentID := seedSQLiteTenantAgent(t, db)
	if _, err := db.Exec(`UPDATE agents SET display_name = ? WHERE id = ?`, "Creator Agent", agentID.String()); err != nil {
		t.Fatalf("update agent display_name: %v", err)
	}
	ctx := store.WithTenantID(context.Background(), tenantID)
	if _, err := skillStore.CreateSkillManaged(ctx, store.SkillCreateParams{
		Name:       "Verified Creator",
		Slug:       "verified-creator",
		OwnerID:    "user-1",
		Visibility: "private",
		FilePath:   filepath.Join(t.TempDir(), "verified-creator", "1"),
		Frontmatter: map[string]string{
			"created_by_agent_id": agentID.String(),
			"created_by_agent":    "Spoofed Name",
		},
	}); err != nil {
		t.Fatalf("CreateSkillManaged error: %v", err)
	}
	if _, err := skillStore.CreateSkillManaged(ctx, store.SkillCreateParams{
		Name:       "Spoofed Creator",
		Slug:       "spoofed-creator",
		OwnerID:    "user-1",
		Visibility: "private",
		FilePath:   filepath.Join(t.TempDir(), "spoofed-creator", "1"),
		Frontmatter: map[string]string{
			"created_by_agent": "Only A String",
		},
	}); err != nil {
		t.Fatalf("CreateSkillManaged error: %v", err)
	}

	list := skillStore.ListSkills(ctx)
	bySlug := map[string]store.SkillInfo{}
	for _, info := range list {
		bySlug[info.Slug] = info
	}
	verified := bySlug["verified-creator"].CreatorAgent
	if verified == nil {
		t.Fatal("CreatorAgent = nil, want verified creator")
	}
	if verified.ID != agentID.String() || verified.DisplayName != "Creator Agent" {
		t.Fatalf("CreatorAgent = %+v, want resolved DB agent", verified)
	}
	if got := bySlug["spoofed-creator"].CreatorAgent; got != nil {
		t.Fatalf("CreatorAgent = %+v, want nil for display-only spoof", got)
	}
}

func TestSQLiteSkillStore_GrantToAgentRejectsCrossTenantSkill(t *testing.T) {
	_, skillStore, db := newTestSQLiteSkillStoreWithDB(t)
	tenantA, agentA := seedSQLiteTenantAgent(t, db)
	tenantB, _ := seedSQLiteTenantAgent(t, db)
	ctxA := store.WithTenantID(context.Background(), tenantA)
	ctxB := store.WithTenantID(context.Background(), tenantB)

	skillID, err := skillStore.CreateSkillManaged(ctxB, store.SkillCreateParams{
		Name:       "Tenant B Skill",
		Slug:       "tenant-b-skill-" + tenantB.String()[:8],
		OwnerID:    "user-1",
		Visibility: "private",
		FilePath:   filepath.Join(t.TempDir(), "tenant-b-skill", "1"),
	})
	if err != nil {
		t.Fatalf("CreateSkillManaged error: %v", err)
	}

	if err := skillStore.GrantToAgent(ctxA, skillID, agentA, 1, "user-1", true); err == nil {
		t.Fatal("GrantToAgent allowed tenant A to grant tenant B skill")
	}

	grants, err := skillStore.ListAgentGrantsForSkill(ctxB, skillID)
	if err != nil {
		t.Fatalf("ListAgentGrantsForSkill error: %v", err)
	}
	if len(grants) != 0 {
		t.Fatalf("cross-tenant grant was inserted: %+v", grants)
	}

	got, ok := skillStore.GetSkillByID(ctxB, skillID)
	if !ok {
		t.Fatal("GetSkillByID returned !ok")
	}
	if got.Visibility != "private" {
		t.Fatalf("cross-tenant grant changed visibility to %q, want private", got.Visibility)
	}
}

func TestSQLiteSkillStore_RevokeFromAgentDoesNotDemoteCrossTenantSkill(t *testing.T) {
	_, skillStore, db := newTestSQLiteSkillStoreWithDB(t)
	tenantA, agentA := seedSQLiteTenantAgent(t, db)
	tenantB, _ := seedSQLiteTenantAgent(t, db)
	ctxA := store.WithTenantID(context.Background(), tenantA)
	ctxB := store.WithTenantID(context.Background(), tenantB)

	skillID, err := skillStore.CreateSkillManaged(ctxB, store.SkillCreateParams{
		Name:       "Tenant B Skill",
		Slug:       "tenant-b-revoke-skill-" + tenantB.String()[:8],
		OwnerID:    "user-1",
		Visibility: "internal",
		FilePath:   filepath.Join(t.TempDir(), "tenant-b-revoke-skill", "1"),
	})
	if err != nil {
		t.Fatalf("CreateSkillManaged error: %v", err)
	}

	if err := skillStore.RevokeFromAgent(ctxA, skillID, agentA); err == nil {
		t.Fatal("RevokeFromAgent allowed tenant A to revoke tenant B skill")
	}

	got, ok := skillStore.GetSkillByID(ctxB, skillID)
	if !ok {
		t.Fatal("GetSkillByID returned !ok")
	}
	if got.Visibility != "internal" {
		t.Fatalf("cross-tenant revoke demoted visibility to %q, want internal", got.Visibility)
	}
}

func TestSQLiteSkillStore_RevokeFromAgentKeepsInternalWhenUserGrantRemains(t *testing.T) {
	_, skillStore, db := newTestSQLiteSkillStoreWithDB(t)
	tenantID, agentID := seedSQLiteTenantAgent(t, db)
	ctx := store.WithTenantID(context.Background(), tenantID)

	skillID, err := skillStore.CreateSkillManaged(ctx, store.SkillCreateParams{
		Name:       "Shared Internal Skill",
		Slug:       "shared-internal-skill-" + tenantID.String()[:8],
		OwnerID:    "owner-user",
		Visibility: "internal",
		FilePath:   filepath.Join(t.TempDir(), "shared-internal-skill", "1"),
	})
	if err != nil {
		t.Fatalf("CreateSkillManaged error: %v", err)
	}
	if err := skillStore.GrantToAgent(ctx, skillID, agentID, 1, "owner-user", true); err != nil {
		t.Fatalf("GrantToAgent error: %v", err)
	}
	if err := skillStore.GrantToUser(ctx, skillID, "granted-user", "owner-user"); err != nil {
		t.Fatalf("GrantToUser error: %v", err)
	}
	if err := skillStore.RevokeFromAgent(ctx, skillID, agentID); err != nil {
		t.Fatalf("RevokeFromAgent error: %v", err)
	}

	got, ok := skillStore.GetSkillByID(ctx, skillID)
	if !ok {
		t.Fatal("GetSkillByID returned !ok")
	}
	if got.Visibility != "internal" {
		t.Fatalf("visibility after revoking last agent grant = %q, want internal because a user grant remains", got.Visibility)
	}
}

func TestSQLiteSkillStore_UserGrantListPromotesAndDemotesVisibility(t *testing.T) {
	_, skillStore, db := newTestSQLiteSkillStoreWithDB(t)
	tenantID, _ := seedSQLiteTenantAgent(t, db)
	ctx := store.WithTenantID(context.Background(), tenantID)

	skillID, err := skillStore.CreateSkillManaged(ctx, store.SkillCreateParams{
		Name:       "User Shared Skill",
		Slug:       "user-shared-skill-" + tenantID.String()[:8],
		OwnerID:    "owner-user",
		Visibility: "private",
		FilePath:   filepath.Join(t.TempDir(), "user-shared-skill", "1"),
	})
	if err != nil {
		t.Fatalf("CreateSkillManaged error: %v", err)
	}
	if err := skillStore.GrantToUser(ctx, skillID, "granted-user", "owner-user"); err != nil {
		t.Fatalf("GrantToUser error: %v", err)
	}

	grants, err := skillStore.ListUserGrantsForSkill(ctx, skillID)
	if err != nil {
		t.Fatalf("ListUserGrantsForSkill error: %v", err)
	}
	if len(grants) != 1 || grants[0].UserID != "granted-user" || grants[0].GrantedBy != "owner-user" {
		t.Fatalf("user grants = %+v", grants)
	}
	got, ok := skillStore.GetSkillByID(ctx, skillID)
	if !ok {
		t.Fatal("GetSkillByID returned !ok")
	}
	if got.Visibility != "internal" {
		t.Fatalf("visibility after user grant = %q, want internal", got.Visibility)
	}
	if err := skillStore.RevokeFromUser(ctx, skillID, "granted-user"); err != nil {
		t.Fatalf("RevokeFromUser error: %v", err)
	}
	got, ok = skillStore.GetSkillByID(ctx, skillID)
	if !ok {
		t.Fatal("GetSkillByID returned !ok after revoke")
	}
	if got.Visibility != "private" {
		t.Fatalf("visibility after last user grant revoke = %q, want private", got.Visibility)
	}
}

func TestSQLiteSkillStore_UserGrantsAreTenantScopedForSystemSkill(t *testing.T) {
	_, skillStore, db := newTestSQLiteSkillStoreWithDB(t)
	tenantA, _ := seedSQLiteTenantAgent(t, db)
	tenantB, _ := seedSQLiteTenantAgent(t, db)
	ctxA := store.WithTenantID(context.Background(), tenantA)
	ctxB := store.WithTenantID(context.Background(), tenantB)
	skillID := uuid.New()
	slug := "system-user-grant-" + skillID.String()[:8]
	if _, err := db.Exec(
		`INSERT INTO skills (id, name, slug, owner_id, visibility, version, status, file_path, is_system, tenant_id)
		 VALUES (?, 'System User Grant Skill', ?, 'system', 'private', 1, 'active', ?, 1, ?)`,
		skillID.String(), slug, filepath.Join(t.TempDir(), "system-user-grant", "1"), store.MasterTenantID.String(),
	); err != nil {
		t.Fatalf("insert system skill: %v", err)
	}

	if err := skillStore.GrantToUser(ctxA, skillID, "same-user", "tenant-a-admin"); err != nil {
		t.Fatalf("GrantToUser tenant A error: %v", err)
	}
	if err := skillStore.GrantToUser(ctxB, skillID, "same-user", "tenant-b-admin"); err != nil {
		t.Fatalf("GrantToUser tenant B error: %v", err)
	}
	grantsA, err := skillStore.ListUserGrantsForSkill(ctxA, skillID)
	if err != nil {
		t.Fatalf("ListUserGrantsForSkill tenant A error: %v", err)
	}
	grantsB, err := skillStore.ListUserGrantsForSkill(ctxB, skillID)
	if err != nil {
		t.Fatalf("ListUserGrantsForSkill tenant B error: %v", err)
	}
	if len(grantsA) != 1 || grantsA[0].GrantedBy != "tenant-a-admin" {
		t.Fatalf("tenant A grants = %+v", grantsA)
	}
	if len(grantsB) != 1 || grantsB[0].GrantedBy != "tenant-b-admin" {
		t.Fatalf("tenant B grants = %+v", grantsB)
	}
}

func TestSQLiteSkillStore_ListWithGrantStatusIgnoresForeignTenantGrant(t *testing.T) {
	_, skillStore, db := newTestSQLiteSkillStoreWithDB(t)
	tenantA, _ := seedSQLiteTenantAgent(t, db)
	tenantB, agentB := seedSQLiteTenantAgent(t, db)
	ctxA := store.WithTenantID(context.Background(), tenantA)

	skillID := uuid.New()
	if _, err := db.Exec(
		`INSERT INTO skills (id, name, slug, owner_id, visibility, version, status, file_path, is_system, tenant_id)
		 VALUES (?, 'System Skill', ?, 'system', 'internal', 1, 'active', ?, 1, ?)`,
		skillID.String(), "system-grant-status-"+skillID.String()[:8], filepath.Join(t.TempDir(), "system-skill", "1"), store.MasterTenantID.String(),
	); err != nil {
		t.Fatalf("insert system skill: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO skill_agent_grants (id, skill_id, agent_id, pinned_version, granted_by, can_manage, tenant_id)
		 VALUES (?, ?, ?, 1, 'tenant-b-admin', 1, ?)`,
		uuid.New().String(), skillID.String(), agentB.String(), tenantB.String(),
	); err != nil {
		t.Fatalf("insert foreign tenant grant: %v", err)
	}

	skills, err := skillStore.ListWithGrantStatus(ctxA, agentB)
	if err != nil {
		t.Fatalf("ListWithGrantStatus error: %v", err)
	}
	for _, skill := range skills {
		if skill.ID == skillID {
			if skill.Granted || skill.CanManage {
				t.Fatalf("foreign tenant grant leaked into tenant A status: granted=%v canManage=%v", skill.Granted, skill.CanManage)
			}
			return
		}
	}
	t.Fatalf("system skill %s not returned for tenant A", skillID)
}

func TestSQLiteSkillStore_ListAccessibleHonorsAccessModes(t *testing.T) {
	_, skillStore, db := newTestSQLiteSkillStoreWithDB(t)
	tenantID, agentA := seedSQLiteTenantAgent(t, db)
	agentB := uuid.New()
	if _, err := db.Exec(
		`INSERT INTO agents (id, tenant_id, agent_key, agent_type, status, provider, model, owner_id)
		 VALUES (?, ?, ?, 'predefined', 'active', 'test', 'test-model', 'user-2')`,
		agentB.String(), tenantID.String(), "agent-"+agentB.String()[:8],
	); err != nil {
		t.Fatalf("insert second agent: %v", err)
	}
	ctx := store.WithTenantID(context.Background(), tenantID)

	privateID, err := skillStore.CreateSkillManaged(ctx, store.SkillCreateParams{
		Name:       "Owner Only Skill",
		Slug:       "owner-only-skill-" + tenantID.String()[:8],
		OwnerID:    "owner-user",
		Visibility: "private",
		FilePath:   filepath.Join(t.TempDir(), "owner-only", "1"),
	})
	if err != nil {
		t.Fatalf("CreateSkillManaged private error: %v", err)
	}
	internalID, err := skillStore.CreateSkillManaged(ctx, store.SkillCreateParams{
		Name:       "Granted Agents Skill",
		Slug:       "granted-agents-skill-" + tenantID.String()[:8],
		OwnerID:    "owner-user",
		Visibility: "internal",
		FilePath:   filepath.Join(t.TempDir(), "granted-agents", "1"),
	})
	if err != nil {
		t.Fatalf("CreateSkillManaged internal error: %v", err)
	}
	publicID, err := skillStore.CreateSkillManaged(ctx, store.SkillCreateParams{
		Name:       "Tenant Wide Skill",
		Slug:       "tenant-wide-skill-" + tenantID.String()[:8],
		OwnerID:    "owner-user",
		Visibility: "public",
		FilePath:   filepath.Join(t.TempDir(), "tenant-wide", "1"),
	})
	if err != nil {
		t.Fatalf("CreateSkillManaged public error: %v", err)
	}
	if err := skillStore.GrantToAgent(ctx, internalID, agentA, 1, "owner-user", true); err != nil {
		t.Fatalf("GrantToAgent internal error: %v", err)
	}

	viewerA := listAccessibleSlugs(t, skillStore, ctx, agentA, "viewer-user")
	if !viewerA["granted-agents-skill-"+tenantID.String()[:8]] {
		t.Fatalf("granted agent did not receive internal skill; got %v", viewerA)
	}
	if !viewerA["tenant-wide-skill-"+tenantID.String()[:8]] {
		t.Fatalf("agent did not receive public skill; got %v", viewerA)
	}
	if viewerA["owner-only-skill-"+tenantID.String()[:8]] {
		t.Fatalf("non-owner received private skill; got %v", viewerA)
	}

	viewerB := listAccessibleSlugs(t, skillStore, ctx, agentB, "viewer-user")
	if viewerB["granted-agents-skill-"+tenantID.String()[:8]] {
		t.Fatalf("ungranted agent received internal skill; got %v", viewerB)
	}
	if !viewerB["tenant-wide-skill-"+tenantID.String()[:8]] {
		t.Fatalf("ungranted agent did not receive public skill; got %v", viewerB)
	}

	owner := listAccessibleSlugs(t, skillStore, ctx, agentB, "owner-user")
	if !owner["owner-only-skill-"+tenantID.String()[:8]] {
		t.Fatalf("owner did not receive private skill %s; got %v", privateID, owner)
	}
	if !owner["tenant-wide-skill-"+tenantID.String()[:8]] {
		t.Fatalf("owner did not receive public skill %s; got %v", publicID, owner)
	}
	if owner["granted-agents-skill-"+tenantID.String()[:8]] {
		t.Fatalf("owner received internal skill without grant; got %v", owner)
	}
}

func TestSQLiteSkillStore_ListAllSkillsIncludesOwnerID(t *testing.T) {
	ctx, skillStore := newTestSQLiteSkillStore(t)
	skillID, err := skillStore.CreateSkillManaged(ctx, store.SkillCreateParams{
		Name:       "Owner Projection Skill",
		Slug:       "owner-projection-skill",
		OwnerID:    "owner-user",
		Visibility: "private",
		FilePath:   filepath.Join(t.TempDir(), "owner-projection", "1"),
	})
	if err != nil {
		t.Fatalf("CreateSkillManaged error: %v", err)
	}

	for _, skill := range skillStore.ListAllSkills(ctx) {
		if skill.ID == skillID.String() {
			if skill.OwnerID != "owner-user" {
				t.Fatalf("OwnerID = %q, want owner-user", skill.OwnerID)
			}
			return
		}
	}
	t.Fatalf("created skill %s not found", skillID)
}

func listAccessibleSlugs(t *testing.T, skillStore *SQLiteSkillStore, ctx context.Context, agentID uuid.UUID, userID string) map[string]bool {
	t.Helper()
	skills, err := skillStore.ListAccessible(ctx, agentID, userID)
	if err != nil {
		t.Fatalf("ListAccessible error: %v", err)
	}
	slugs := make(map[string]bool, len(skills))
	for _, skill := range skills {
		slugs[skill.Slug] = true
	}
	return slugs
}

func newTestSQLiteSkillStore(t *testing.T) (context.Context, *SQLiteSkillStore) {
	ctx, skillStore, _ := newTestSQLiteSkillStoreWithDB(t)
	return ctx, skillStore
}

func newTestSQLiteSkillStoreWithDB(t *testing.T) (context.Context, *SQLiteSkillStore, *sql.DB) {
	t.Helper()

	db, err := OpenDB(filepath.Join(t.TempDir(), "skills.db"))
	if err != nil {
		t.Fatalf("OpenDB error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema error: %v", err)
	}

	return store.WithTenantID(context.Background(), store.MasterTenantID), NewSQLiteSkillStore(db, t.TempDir()), db
}

func seedSQLiteTenantAgent(t *testing.T, db *sql.DB) (uuid.UUID, uuid.UUID) {
	t.Helper()

	tenantID := uuid.New()
	agentID := uuid.New()
	if _, err := db.Exec(
		`INSERT INTO tenants (id, name, slug, status) VALUES (?, ?, ?, 'active')`,
		tenantID.String(), "tenant-"+tenantID.String()[:8], "t"+tenantID.String()[:8],
	); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO agents (id, tenant_id, agent_key, agent_type, status, provider, model, owner_id)
		 VALUES (?, ?, ?, 'predefined', 'active', 'test', 'test-model', 'user-1')`,
		agentID.String(), tenantID.String(), "agent-"+agentID.String()[:8],
	); err != nil {
		t.Fatalf("insert agent: %v", err)
	}
	return tenantID, agentID
}
