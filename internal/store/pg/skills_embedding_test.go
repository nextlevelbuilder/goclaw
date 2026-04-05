package pg

import (
	"testing"
)

func TestBuildSkillEmbeddingTenantCond_Empty(t *testing.T) {
	got := buildSkillEmbeddingTenantCond("")
	if got != "" {
		t.Fatalf("expected empty tenant condition, got %q", got)
	}
}

func TestBuildSkillEmbeddingTenantCond_WithTenant(t *testing.T) {
	got := buildSkillEmbeddingTenantCond(" AND tenant_id = $2")
	want := " AND (is_system = true OR (tenant_id = $2))"
	if got != want {
		t.Fatalf("unexpected tenant condition\nwant: %q\n got: %q", want, got)
	}
}

func TestBuildSkillEmbeddingTenantCond_WithTenantAndProject(t *testing.T) {
	got := buildSkillEmbeddingTenantCond(" AND tenant_id = $2 AND project_id = $3")
	want := " AND (is_system = true OR (tenant_id = $2 AND project_id = $3))"
	if got != want {
		t.Fatalf("unexpected tenant/project condition\nwant: %q\n got: %q", want, got)
	}
}
