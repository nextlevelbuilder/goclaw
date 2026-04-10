//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

func newSkillStore(t *testing.T) *pg.PGSkillStore {
	t.Helper()
	db := testDB(t)
	pg.InitSqlx(db)
	return pg.NewPGSkillStore(db, t.TempDir())
}

// seedSkill inserts a custom skill via CreateSkillManaged and returns its UUID.
func seedSkill(t *testing.T, s *pg.PGSkillStore, ctx context.Context, slug, name string) uuid.UUID {
	t.Helper()
	desc := "test skill: " + name
	id, err := s.CreateSkillManaged(ctx, store.SkillCreateParams{
		Name:        name,
		Slug:        slug,
		Description: &desc,
		OwnerID:     "test-owner",
		Visibility:  "private",
		Status:      "active",
		Version:     1,
		FilePath:    "/tmp/skills/" + slug + "/1",
		FileSize:    100,
	})
	if err != nil {
		t.Fatalf("seedSkill(%s): %v", slug, err)
	}
	return id
}

func TestStoreSkill_CreateAndGet(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	s := newSkillStore(t)

	desc := "A tool for testing"
	id, err := s.CreateSkillManaged(ctx, store.SkillCreateParams{
		Name:        "Test Skill",
		Slug:        "test-skill-" + tenantID.String()[:8],
		Description: &desc,
		OwnerID:     "test-owner",
		Visibility:  "private",
		Status:      "active",
		Version:     1,
		FilePath:    "/tmp/skills/test-skill/1",
		FileSize:    256,
		Frontmatter: map[string]string{"author": "tester"},
	})
	if err != nil {
		t.Fatalf("CreateSkillManaged: %v", err)
	}
	if id == uuid.Nil {
		t.Fatal("expected non-nil skill ID")
	}

	// GetSkillByID
	got, ok := s.GetSkillByID(ctx, id)
	if !ok {
		t.Fatal("GetSkillByID returned false")
	}
	if got.Name != "Test Skill" {
		t.Errorf("Name = %q, want %q", got.Name, "Test Skill")
	}
	if got.Description != "A tool for testing" {
		t.Errorf("Description = %q, want %q", got.Description, "A tool for testing")
	}
	if got.Visibility != "private" {
		t.Errorf("Visibility = %q, want %q", got.Visibility, "private")
	}
	if got.Status != "active" {
		t.Errorf("Status = %q, want %q", got.Status, "active")
	}

	// GetSkill (by slug)
	slug := "test-skill-" + tenantID.String()[:8]
	got2, ok := s.GetSkill(ctx, slug)
	if !ok {
		t.Fatal("GetSkill by slug returned false")
	}
	if got2.ID != id.String() {
		t.Errorf("GetSkill ID = %q, want %q", got2.ID, id.String())
	}

	// GetSkillOwnerID
	ownerID, ok := s.GetSkillOwnerID(ctx, id)
	if !ok {
		t.Fatal("GetSkillOwnerID returned false")
	}
	if ownerID != "test-owner" {
		t.Errorf("OwnerID = %q, want %q", ownerID, "test-owner")
	}

	// GetSkillOwnerIDBySlug
	ownerID2, ok := s.GetSkillOwnerIDBySlug(ctx, slug)
	if !ok {
		t.Fatal("GetSkillOwnerIDBySlug returned false")
	}
	if ownerID2 != "test-owner" {
		t.Errorf("OwnerIDBySlug = %q, want %q", ownerID2, "test-owner")
	}
}

func TestStoreSkill_Update(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	s := newSkillStore(t)

	slug := "upd-skill-" + tenantID.String()[:8]
	id := seedSkill(t, s, ctx, slug, "Update Me")

	// Update description and visibility
	err := s.UpdateSkill(ctx, id, map[string]any{
		"description": "updated description",
		"visibility":  "public",
	})
	if err != nil {
		t.Fatalf("UpdateSkill: %v", err)
	}

	got, ok := s.GetSkillByID(ctx, id)
	if !ok {
		t.Fatal("GetSkillByID after update returned false")
	}
	if got.Description != "updated description" {
		t.Errorf("Description = %q, want %q", got.Description, "updated description")
	}
	if got.Visibility != "public" {
		t.Errorf("Visibility = %q, want %q", got.Visibility, "public")
	}
}

func TestStoreSkill_Delete(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	s := newSkillStore(t)

	slug := "del-skill-" + tenantID.String()[:8]
	id := seedSkill(t, s, ctx, slug, "Delete Me")

	// Verify it exists in list
	list := s.ListSkills(ctx)
	found := false
	for _, sk := range list {
		if sk.Slug == slug {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("skill not found in ListSkills before delete")
	}

	// Delete (soft-delete)
	if err := s.DeleteSkill(ctx, id); err != nil {
		t.Fatalf("DeleteSkill: %v", err)
	}

	// Verify soft-deleted: GetSkillByID still works (returns any status)
	got, ok := s.GetSkillByID(ctx, id)
	if !ok {
		t.Fatal("GetSkillByID after delete returned false")
	}
	if got.Status != "deleted" {
		t.Errorf("Status = %q, want %q", got.Status, "deleted")
	}

	// Bump version to invalidate cache, then verify not in ListSkills
	s.BumpVersion()
	list2 := s.ListSkills(ctx)
	for _, sk := range list2 {
		if sk.Slug == slug {
			t.Error("deleted skill still appears in ListSkills")
		}
	}
}

func TestStoreSkill_ListSkills(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	s := newSkillStore(t)

	slug1 := "list-a-" + tenantID.String()[:8]
	slug2 := "list-b-" + tenantID.String()[:8]
	seedSkill(t, s, ctx, slug1, "List A")
	seedSkill(t, s, ctx, slug2, "List B")

	// Bump version to invalidate cache
	s.BumpVersion()
	list := s.ListSkills(ctx)

	found1, found2 := false, false
	for _, sk := range list {
		if sk.Slug == slug1 {
			found1 = true
		}
		if sk.Slug == slug2 {
			found2 = true
		}
	}
	if !found1 {
		t.Errorf("skill %q not found in ListSkills", slug1)
	}
	if !found2 {
		t.Errorf("skill %q not found in ListSkills", slug2)
	}

	// ListAllSkills should also include them
	all := s.ListAllSkills(ctx)
	found1 = false
	for _, sk := range all {
		if sk.Slug == slug1 {
			found1 = true
			break
		}
	}
	if !found1 {
		t.Errorf("skill %q not found in ListAllSkills", slug1)
	}

	// Toggle disabled — should disappear from ListAllSkills (which filters enabled=true)
	id := seedSkill(t, s, ctx, "list-toggle-"+tenantID.String()[:8], "Toggle Me")
	if err := s.ToggleSkill(ctx, id, false); err != nil {
		t.Fatalf("ToggleSkill: %v", err)
	}
	s.BumpVersion()
	allAfter := s.ListAllSkills(ctx)
	for _, sk := range allAfter {
		if sk.Slug == "list-toggle-"+tenantID.String()[:8] {
			t.Error("disabled skill still in ListAllSkills")
		}
	}
}

func TestStoreSkill_GrantToAgent(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	s := newSkillStore(t)

	slug := "grant-skill-" + tenantID.String()[:8]
	skillID := seedSkill(t, s, ctx, slug, "Grant Skill")

	// Grant to agent
	if err := s.GrantToAgent(ctx, skillID, agentID, 1, "test-owner"); err != nil {
		t.Fatalf("GrantToAgent: %v", err)
	}

	// Verify via ListWithGrantStatus
	grantList, err := s.ListWithGrantStatus(ctx, agentID)
	if err != nil {
		t.Fatalf("ListWithGrantStatus: %v", err)
	}
	found := false
	for _, g := range grantList {
		if g.ID == skillID {
			if !g.Granted {
				t.Error("expected Granted=true for granted skill")
			}
			found = true
			break
		}
	}
	if !found {
		t.Error("granted skill not found in ListWithGrantStatus")
	}

	// Verify via ListAccessible
	accessible, err := s.ListAccessible(ctx, agentID, "test-owner")
	if err != nil {
		t.Fatalf("ListAccessible: %v", err)
	}
	foundAccessible := false
	for _, sk := range accessible {
		if sk.Slug == slug {
			foundAccessible = true
			break
		}
	}
	if !foundAccessible {
		t.Error("granted skill not found in ListAccessible")
	}

	// Revoke
	if err := s.RevokeFromAgent(ctx, skillID, agentID); err != nil {
		t.Fatalf("RevokeFromAgent: %v", err)
	}

	// Verify revoked
	grantList2, err := s.ListWithGrantStatus(ctx, agentID)
	if err != nil {
		t.Fatalf("ListWithGrantStatus after revoke: %v", err)
	}
	for _, g := range grantList2 {
		if g.ID == skillID && g.Granted {
			t.Error("expected Granted=false after revoke")
		}
	}
}

func TestStoreSkill_TenantIsolation(t *testing.T) {
	db := testDB(t)
	tenantA, _ := seedTenantAgent(t, db)
	tenantB, _ := seedTenantAgent(t, db)
	ctxA := tenantCtx(tenantA)
	ctxB := tenantCtx(tenantB)
	s := newSkillStore(t)

	// Create skill in tenant A
	slugA := "iso-skill-" + tenantA.String()[:8]
	seedSkill(t, s, ctxA, slugA, "Tenant A Skill")

	// Bump version to ensure fresh queries
	s.BumpVersion()

	// Tenant B should NOT see tenant A's skill
	listB := s.ListSkills(ctxB)
	for _, sk := range listB {
		if sk.Slug == slugA {
			t.Errorf("tenant B can see tenant A's skill %q — isolation broken", slugA)
		}
	}

	// Tenant A should see it
	listA := s.ListSkills(ctxA)
	found := false
	for _, sk := range listA {
		if sk.Slug == slugA {
			found = true
			break
		}
	}
	if !found {
		t.Error("tenant A cannot see its own skill")
	}

	// GetSkill from tenant B context should fail
	_, ok := s.GetSkill(ctxB, slugA)
	if ok {
		t.Error("GetSkill from tenant B returned true for tenant A's skill")
	}
}
