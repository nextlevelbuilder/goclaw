package skills

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

type customSlugConflictStore struct {
	customID       uuid.UUID
	nextVersion    int
	conflictErr    error
	updatedID      uuid.UUID
	updatedCtx     context.Context
	updates        map[string]any
	updateErr      error
	bumps          int
	updateAttempts int
}

func (s *customSlugConflictStore) UpsertSystemSkill(context.Context, store.SkillCreateParams) (uuid.UUID, bool, string, error) {
	return s.customID, false, "", s.conflictErr
}

func (s *customSlugConflictStore) GetNextVersion(context.Context, string) int { return s.nextVersion }
func (s *customSlugConflictStore) BumpVersion()                               { s.bumps++ }

func (s *customSlugConflictStore) UpdateSkill(ctx context.Context, id uuid.UUID, updates map[string]any) error {
	s.updateAttempts++
	if s.updateErr != nil {
		return s.updateErr
	}
	s.updatedID = id
	s.updatedCtx = ctx
	s.updates = updates
	return s.updateErr
}

func TestSeeder_RestoresCustomMetadataWhenBundledSlugConflicts(t *testing.T) {
	bundledDir := filepath.Join(t.TempDir(), "bundled")
	managedDir := filepath.Join(t.TempDir(), "managed")
	slug := "lark-pm"
	bundledContent := "---\nname: lark-pm\ndescription: Bundled workflow\nlicense: Proprietary. Part of GoClaw bundled skills.\n---\n"
	customContent := "---\nname: Lark PM\ndescription: Custom Lark workflow\nversion: 1.0.0\n---\n\n# Custom Lark PM\n"

	for path, content := range map[string]string{
		filepath.Join(bundledDir, slug, "SKILL.md"):      bundledContent,
		filepath.Join(managedDir, slug, "4", "SKILL.md"): customContent,
		filepath.Join(managedDir, slug, "5", "SKILL.md"): bundledContent,
	} {
		writeSeederSkillFile(t, path, content)
	}

	customID := uuid.New()
	skillStore := &customSlugConflictStore{customID: customID, nextVersion: 5, conflictErr: store.ErrMisclassifiedCustomSkill}
	seeder := NewSeeder(bundledDir, managedDir, skillStore)
	seeded, skipped, _, err := seeder.Seed(context.Background())
	if err != nil {
		t.Fatalf("Seed error: %v", err)
	}
	if seeded != 0 || skipped != 1 {
		t.Fatalf("Seed = (%d seeded, %d skipped), want (0, 1)", seeded, skipped)
	}
	if skillStore.updatedID != customID {
		t.Fatalf("updated ID = %s, want %s", skillStore.updatedID, customID)
	}
	if tenantID := store.TenantIDFromContext(skillStore.updatedCtx); tenantID != store.MasterTenantID {
		t.Fatalf("update tenant = %s, want master tenant %s", tenantID, store.MasterTenantID)
	}
	if got := skillStore.updates["name"]; got != "Lark PM" {
		t.Fatalf("restored name = %q, want custom name", got)
	}
	if got := skillStore.updates["description"]; got != "Custom Lark workflow" {
		t.Fatalf("restored description = %q, want custom description", got)
	}
	if got := skillStore.updates["file_path"]; got != filepath.Join(managedDir, slug, "4") {
		t.Fatalf("restored file path = %q, want custom version directory", got)
	}
	if got := skillStore.updates["visibility"]; got != "private" {
		t.Fatalf("restored visibility = %q, want private", got)
	}
	if got := skillStore.updates["status"]; got != "archived" {
		t.Fatalf("restored status = %q, want archived", got)
	}
	if got := skillStore.updates["version"]; got != 4 {
		t.Fatalf("restored version = %v, want 4", got)
	}

	var frontmatter map[string]string
	if err := json.Unmarshal(skillStore.updates["frontmatter"].([]byte), &frontmatter); err != nil {
		t.Fatalf("unmarshal restored frontmatter: %v", err)
	}
	if frontmatter["name"] != "Lark PM" || frontmatter["description"] != "Custom Lark workflow" {
		t.Fatalf("restored frontmatter = %#v", frontmatter)
	}
	if _, err := os.Stat(filepath.Join(managedDir, slug, "5")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("overwritten numeric directory still exists: %v", err)
	}
	quarantineDir := filepath.Join(managedDir, slug, ".goclaw-bundled-5-"+hashSeederContent(bundledContent)[:12])
	if _, err := os.Stat(filepath.Join(quarantineDir, "SKILL.md")); err != nil {
		t.Fatalf("quarantined bundled skill missing: %v", err)
	}
	loader := NewLoader("", "", "")
	if version, dir := loader.findLatestVersion(managedDir, slug); version != 4 || dir != filepath.Join(managedDir, slug, "4") {
		t.Fatalf("latest managed version = (%d, %q), want restored custom version 4", version, dir)
	}
}

func TestSeeder_ReportsAmbiguousCustomRecovery(t *testing.T) {
	bundledDir := filepath.Join(t.TempDir(), "bundled")
	managedDir := filepath.Join(t.TempDir(), "managed")
	slug := "lark-pm"
	bundledContent := "---\nname: lark-pm\ndescription: Bundled workflow\nlicense: Proprietary. Part of GoClaw bundled skills.\n---\n"
	priorBundledContent := bundledContent + "\n# Previous bundled body\n"
	writeSeederSkillFile(t, filepath.Join(bundledDir, slug, "SKILL.md"), bundledContent)
	writeSeederSkillFile(t, filepath.Join(managedDir, slug, "4", "SKILL.md"), priorBundledContent)
	writeSeederSkillFile(t, filepath.Join(managedDir, slug, "5", "SKILL.md"), bundledContent)

	skillStore := &customSlugConflictStore{customID: uuid.New(), nextVersion: 5, conflictErr: store.ErrMisclassifiedCustomSkill}
	seeder := NewSeeder(bundledDir, managedDir, skillStore)
	_, skipped, _, err := seeder.Seed(context.Background())
	if err == nil || !strings.Contains(err.Error(), "custom version is ambiguous") {
		t.Fatalf("Seed error = %v, want ambiguous recovery error", err)
	}
	if skipped != 1 {
		t.Fatalf("skipped = %d, want 1", skipped)
	}
	if skillStore.updatedID != uuid.Nil {
		t.Fatalf("ambiguous recovery updated skill %s", skillStore.updatedID)
	}
	if _, err := os.Stat(filepath.Join(managedDir, slug, "5", "SKILL.md")); err != nil {
		t.Fatalf("ambiguous bundled version was moved: %v", err)
	}
}

func TestSeeder_SkipsLegitimateCustomSlugConflictWithoutRecovery(t *testing.T) {
	bundledDir := filepath.Join(t.TempDir(), "bundled")
	managedDir := filepath.Join(t.TempDir(), "managed")
	slug := "lark-pm"
	writeSeederSkillFile(t, filepath.Join(bundledDir, slug, "SKILL.md"), "---\nname: lark-pm\ndescription: Bundled workflow\n---\n")

	skillStore := &customSlugConflictStore{customID: uuid.New(), nextVersion: 2, conflictErr: store.ErrSystemSkillSlugConflict}
	seeder := NewSeeder(bundledDir, managedDir, skillStore)
	_, skipped, _, err := seeder.Seed(context.Background())
	if err != nil {
		t.Fatalf("Seed error: %v", err)
	}
	if skipped != 1 {
		t.Fatalf("skipped = %d, want 1", skipped)
	}
	if skillStore.updatedID != uuid.Nil {
		t.Fatalf("legitimate custom collision updated skill %s", skillStore.updatedID)
	}
}

func TestSeeder_ReportsMissingMarkedRecoveryFiles(t *testing.T) {
	bundledDir := filepath.Join(t.TempDir(), "bundled")
	managedDir := filepath.Join(t.TempDir(), "managed")
	slug := "lark-pm"
	writeSeederSkillFile(t, filepath.Join(bundledDir, slug, "SKILL.md"), "---\nname: lark-pm\ndescription: Bundled workflow\n---\n")

	skillStore := &customSlugConflictStore{
		customID:    uuid.New(),
		nextVersion: 5,
		conflictErr: store.ErrMisclassifiedCustomSkill,
	}
	seeder := NewSeeder(bundledDir, managedDir, skillStore)
	_, skipped, _, err := seeder.Seed(context.Background())
	if err == nil || !strings.Contains(err.Error(), "read overwritten bundled skill") {
		t.Fatalf("Seed error = %v, want missing recovery file error", err)
	}
	if skipped != 1 {
		t.Fatalf("skipped = %d, want 1", skipped)
	}
}

func TestSeeder_ContinuesRecoveryFromQuarantinedBundledVersion(t *testing.T) {
	bundledDir := filepath.Join(t.TempDir(), "bundled")
	managedDir := filepath.Join(t.TempDir(), "managed")
	slug := "lark-pm"
	bundledContent := "---\nname: lark-pm\ndescription: Bundled workflow\n---\n"
	writeSeederSkillFile(t, filepath.Join(bundledDir, slug, "SKILL.md"), bundledContent)
	writeSeederSkillFile(t, filepath.Join(managedDir, slug, "4", "SKILL.md"), "---\nname: Lark PM\ndescription: Custom workflow\nversion: 1.0.0\n---\n")
	quarantineDir := filepath.Join(managedDir, slug, ".goclaw-bundled-5-"+hashSeederContent(bundledContent)[:12])
	writeSeederSkillFile(t, filepath.Join(quarantineDir, "SKILL.md"), bundledContent)

	skillStore := &customSlugConflictStore{
		customID:    uuid.New(),
		nextVersion: 5,
		conflictErr: store.ErrMisclassifiedCustomSkill,
	}
	seeder := NewSeeder(bundledDir, managedDir, skillStore)
	_, _, _, err := seeder.Seed(context.Background())
	if err != nil {
		t.Fatalf("Seed error: %v", err)
	}
	if skillStore.updatedID == uuid.Nil {
		t.Fatal("quarantined recovery did not update custom metadata")
	}
	if _, err := os.Stat(filepath.Join(quarantineDir, "SKILL.md")); err != nil {
		t.Fatalf("quarantine missing after recovery: %v", err)
	}
}

func TestSeeder_PropagatesCustomRecoveryUpdateFailure(t *testing.T) {
	bundledDir := filepath.Join(t.TempDir(), "bundled")
	managedDir := filepath.Join(t.TempDir(), "managed")
	slug := "lark-pm"
	bundledContent := "---\nname: lark-pm\ndescription: Bundled workflow\n---\n"
	writeSeederSkillFile(t, filepath.Join(bundledDir, slug, "SKILL.md"), bundledContent)
	writeSeederSkillFile(t, filepath.Join(managedDir, slug, "4", "SKILL.md"), "---\nname: Lark PM\ndescription: Custom workflow\nversion: 1.0.0\n---\n")
	writeSeederSkillFile(t, filepath.Join(managedDir, slug, "5", "SKILL.md"), bundledContent)

	skillStore := &customSlugConflictStore{
		customID:    uuid.New(),
		nextVersion: 5,
		conflictErr: store.ErrMisclassifiedCustomSkill,
		updateErr:   errors.New("database unavailable"),
	}
	seeder := NewSeeder(bundledDir, managedDir, skillStore)
	_, _, _, err := seeder.Seed(context.Background())
	if err == nil || !strings.Contains(err.Error(), "database unavailable") {
		t.Fatalf("Seed error = %v, want update failure", err)
	}
	if _, err := os.Stat(filepath.Join(managedDir, slug, "5", "SKILL.md")); err != nil {
		t.Fatalf("bundled version moved despite failed update: %v", err)
	}
}

func TestSeeder_CheckDepsRestoresArchivedSkillOnKnownSuccess(t *testing.T) {
	slug := "no-deps"
	skillDir := filepath.Join(t.TempDir(), slug)
	writeSeederSkillFile(t, filepath.Join(skillDir, "SKILL.md"), "---\nname: no-deps\ndescription: No deps\n---\n")

	st := &customSlugConflictStore{}
	seeder := NewSeeder("", "", st)
	seeder.checkDeps([]seededSkill{{id: uuid.New(), slug: slug, baseDir: skillDir}}, nil)

	if got := st.updates["status"]; got != "active" {
		t.Fatalf("persisted status = %v, want active (recovery from archived)", got)
	}
	if st.bumps != 1 {
		t.Fatalf("BumpVersion calls = %d, want 1", st.bumps)
	}
	var deps struct {
		Missing []string `json:"missing"`
	}
	if err := json.Unmarshal([]byte(st.updates["deps"].(string)), &deps); err != nil || len(deps.Missing) != 0 {
		t.Fatalf("atomic dependency recovery = %v, err=%v", st.updates, err)
	}
}

func TestSeeder_CheckDepsLeavesArchivedSkillOnUnknown(t *testing.T) {
	sleep := sleepPath(t)
	writeFakeBin(t, "python3", "#!/bin/sh\nexec "+sleep+" 7\n")
	t.Setenv("GOCLAW_DEP_CHECK_TIMEOUT", "1s")

	slug := "slow-import"
	skillDir := filepath.Join(t.TempDir(), slug)
	writeSeederSkillFile(t, filepath.Join(skillDir, "SKILL.md"), "---\nname: slow-import\ndeps:\n  - pip:anthropic\n---\n")

	st := &customSlugConflictStore{}
	seeder := NewSeeder("", "", st)
	seeder.checkDeps([]seededSkill{{id: uuid.New(), slug: slug, baseDir: skillDir}}, nil)

	if st.updatedID != uuid.Nil {
		t.Fatalf("unknown check updated status: %v", st.updates)
	}
	if st.updateAttempts != 0 {
		t.Fatalf("unknown check attempted %d writes", st.updateAttempts)
	}
	if st.bumps != 0 {
		t.Fatalf("BumpVersion calls = %d, want 0", st.bumps)
	}
}

// TestSeeder_CheckDepsKeepsStateWhenMissingAndUnknownMix pins the mixed case:
// one dependency is verified missing while another probe times out. The
// missing list is incomplete, so the seeder keeps the stored state.
func TestSeeder_CheckDepsKeepsStateWhenMissingAndUnknownMix(t *testing.T) {
	sleep := sleepPath(t)
	writeFakeBin(t, "python3", "#!/bin/sh\nexec "+sleep+" 7\n")
	t.Setenv("GOCLAW_DEP_CHECK_TIMEOUT", "1s")

	slug := "missing-and-slow"
	skillDir := filepath.Join(t.TempDir(), slug)
	writeSeederSkillFile(t, filepath.Join(skillDir, "SKILL.md"),
		"---\nname: missing-and-slow\ndeps:\n  - system:missing_goclaw_test_binary_xyz\n  - pip:anthropic\n---\n")

	result := CheckSkillDepsContext(context.Background(), ScanSkillDeps(skillDir))
	if result.Status != DepCheckMissing || !result.Incomplete {
		t.Fatalf("precondition: result = %+v, want Missing and Incomplete", result)
	}

	st := &customSlugConflictStore{}
	seeder := NewSeeder("", "", st)
	seeder.checkDeps([]seededSkill{{id: uuid.New(), slug: slug, baseDir: skillDir}}, nil)

	if st.updateAttempts != 0 {
		t.Fatalf("incomplete missing result attempted %d writes: %v", st.updateAttempts, st.updates)
	}
	if st.bumps != 0 {
		t.Fatalf("BumpVersion calls = %d, want 0", st.bumps)
	}
}

func writeSeederSkillFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("create skill directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
}

func hashSeederContent(content string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
}

func TestSeeder_FailedDependencyStateWriteDoesNotPublishSuccess(t *testing.T) {
	for _, tc := range []struct{ name, deps string }{
		{"recover", ""},
		{"archive", "deps:\n  - system:missing_goclaw_test_binary_xyz\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeSeederSkillFile(t, filepath.Join(dir, "SKILL.md"), "---\nname: failure\n"+tc.deps+"---\n")
			st := &customSlugConflictStore{updateErr: errors.New("dependency data write failed")}
			events := []bus.Event{}
			messages := bus.New()
			messages.Subscribe("state-write-test", func(e bus.Event) { events = append(events, e) })
			seeder := NewSeeder("", "", st)
			seeder.checkDeps([]seededSkill{{id: uuid.New(), slug: "failure", baseDir: dir}}, messages)
			if st.updateAttempts != 1 || st.updatedID != uuid.Nil || st.updates != nil || st.bumps != 0 {
				t.Fatalf("failed write published local state: %+v", st)
			}
			for _, e := range events {
				if e.Name == protocol.EventSkillDepsChecked {
					t.Fatalf("published successful skill state after write failure: %+v", e)
				}
			}
		})
	}
}
