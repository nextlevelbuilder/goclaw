//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

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

func newTestSQLiteSkillStore(t *testing.T) (context.Context, *SQLiteSkillStore) {
	t.Helper()

	db, err := OpenDB(filepath.Join(t.TempDir(), "skills.db"))
	if err != nil {
		t.Fatalf("OpenDB error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema error: %v", err)
	}

	return store.WithTenantID(context.Background(), store.MasterTenantID), NewSQLiteSkillStore(db, t.TempDir())
}
