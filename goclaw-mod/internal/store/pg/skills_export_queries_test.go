package pg

import (
	"database/sql"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestExportSkillsSelectionIncludesExplicitSystemAndTenantCustom(t *testing.T) {
	db := hooksTestDB(t)
	tenantA, _ := seedTenantAndAgent(t, db)
	tenantB, _ := seedTenantAndAgent(t, db)
	customA := seedExportSkill(t, db, tenantA, "custom-a", false)
	customB := seedExportSkill(t, db, tenantB, "custom-b", false)
	system := seedExportSkill(t, db, tenantB, "system-global", true)

	ctx := store.WithTenantID(t.Context(), tenantA)
	got, err := ExportSkills(ctx, db, SkillExportSelection{
		IDs: []uuid.UUID{customA, customB, system},
	})
	if err != nil {
		t.Fatalf("ExportSkills() error = %v", err)
	}
	slugs := exportSkillSlugs(got)
	if !containsSlug(slugs, "system-global") || !containsSlug(slugs, "custom-a") {
		t.Fatalf("slugs = %v, want tenant custom + explicit system", slugs)
	}
	if containsSlug(slugs, "custom-b") {
		t.Fatalf("slugs = %v, cross-tenant custom leaked", slugs)
	}
}

func TestExportSkillsDefaultRemainsCustomOnly(t *testing.T) {
	db := hooksTestDB(t)
	tenantID, _ := seedTenantAndAgent(t, db)
	seedExportSkill(t, db, tenantID, "custom-default", false)
	seedExportSkill(t, db, tenantID, "system-default", true)

	ctx := store.WithTenantID(t.Context(), tenantID)
	got, err := ExportSkills(ctx, db, SkillExportSelection{})
	if err != nil {
		t.Fatalf("ExportSkills() error = %v", err)
	}
	slugs := exportSkillSlugs(got)
	if !containsSlug(slugs, "custom-default") {
		t.Fatalf("slugs = %v, missing custom skill", slugs)
	}
	if containsSlug(slugs, "system-default") {
		t.Fatalf("slugs = %v, default export included system skill", slugs)
	}
}

func seedExportSkill(t *testing.T, db *sql.DB, tenantID uuid.UUID, slug string, isSystem bool) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	_, err := db.ExecContext(store.WithTenantID(t.Context(), tenantID),
		`INSERT INTO skills (id, name, slug, description, owner_id, visibility, version, status, frontmatter, file_path, file_size, tags, is_system, deps, enabled, tenant_id)
		 VALUES ($1,$2,$3,$4,$5,'private',1,'active','{}',$6,0,ARRAY['export-test']::text[],$7,'{}',true,$8)`,
		id, slug, slug, slug+" desc", "owner", "/tmp/"+slug+"/1", isSystem, tenantID,
	)
	if err != nil {
		t.Fatalf("seed skill %s: %v", slug, err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec("DELETE FROM skills WHERE id=$1", id)
	})
	return id
}

func exportSkillSlugs(skills []CustomSkillExport) []string {
	out := make([]string, 0, len(skills))
	for _, skill := range skills {
		out = append(out, skill.Slug)
	}
	return out
}

func containsSlug(slugs []string, want string) bool {
	return slices.Contains(slugs, want)
}
