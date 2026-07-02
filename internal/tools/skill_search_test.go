package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// TestSkillSearchTool_TenantIsolation mirrors
// TestLoader_ManagedSkills_TenantIsolation (internal/skills/loader_test.go):
// two tenants each have a managed skill with the same slug but different,
// tenant-secret content. Execute() called under tenant A's context must
// never return tenant B's content, and vice versa, including after tenant
// B has already been searched (proving the per-tenant index cache doesn't
// get clobbered by a later, different-tenant call).
func TestSkillSearchTool_TenantIsolation(t *testing.T) {
	dataDir := t.TempDir()

	tenantA := uuid.MustParse("00000000-0000-0000-0000-0000000000aa")
	tenantB := uuid.MustParse("00000000-0000-0000-0000-0000000000bb")

	dirA := filepath.Join(dataDir, "tenants", tenantA.String(), "skills-store", "shared-slug", "1")
	if err := os.MkdirAll(dirA, 0755); err != nil {
		t.Fatalf("mkdir tenant A: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dirA, "SKILL.md"),
		[]byte("---\nname: Tenant A Skill\ndescription: alpha rendering skill\n---\nTENANT A SECRET"), 0644); err != nil {
		t.Fatalf("write tenant A skill: %v", err)
	}

	dirB := filepath.Join(dataDir, "tenants", tenantB.String(), "skills-store", "shared-slug", "1")
	if err := os.MkdirAll(dirB, 0755); err != nil {
		t.Fatalf("mkdir tenant B: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dirB, "SKILL.md"),
		[]byte("---\nname: Tenant B Skill\ndescription: alpha rendering skill\n---\nTENANT B SECRET"), 0644); err != nil {
		t.Fatalf("write tenant B skill: %v", err)
	}

	loader := skills.NewLoader("", "", "")
	loader.SetManagedDir(dataDir)

	tool := NewSkillSearchTool(loader)

	ctxA := store.WithTenantID(context.Background(), tenantA)
	ctxB := store.WithTenantID(context.Background(), tenantB)

	execAndDump := func(ctx context.Context) string {
		res := tool.Execute(ctx, map[string]any{"query": "alpha rendering"})
		if res == nil {
			t.Fatal("Execute returned nil result")
		}
		return res.ForLLM
	}

	// First search under tenant A.
	outA := execAndDump(ctxA)
	if !strings.Contains(outA, "Tenant A Skill") || strings.Contains(outA, "Tenant B Skill") {
		t.Fatalf("tenant A search leaked or missing tenant A content: %q", outA)
	}

	// Now search under tenant B — must never see tenant A's content.
	outB := execAndDump(ctxB)
	if !strings.Contains(outB, "Tenant B Skill") || strings.Contains(outB, "Tenant A Skill") {
		t.Fatalf("tenant B search leaked or missing tenant B content: %q", outB)
	}

	// Re-check tenant A AFTER tenant B was searched, proving the per-tenant
	// cache entry was not clobbered by the intervening tenant B lookup.
	outA2 := execAndDump(ctxA)
	if !strings.Contains(outA2, "Tenant A Skill") || strings.Contains(outA2, "Tenant B Skill") {
		t.Fatalf("tenant A re-check leaked or missing tenant A content: %q", outA2)
	}
}
