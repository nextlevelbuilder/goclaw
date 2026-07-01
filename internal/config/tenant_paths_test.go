package config

import (
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

func TestTenantScopedDir_MasterTenant(t *testing.T) {
	got := TenantScopedDir("/data", masterTenantID.String(), "master")
	if got != "/data" {
		t.Errorf("master tenant should be no-op, got %s", got)
	}
}

func TestTenantScopedDir_EmptyTenantID(t *testing.T) {
	got := TenantScopedDir("/data", "", "acme")
	if got != "/data" {
		t.Errorf("empty tenantID should be no-op, got %s", got)
	}
}

func TestTenantScopedDir_UsesSlug(t *testing.T) {
	tid := uuid.MustParse("0193b000-0000-7000-8000-000000000002")
	got := TenantScopedDir("/data", tid.String(), "acme")
	want := filepath.Join("/data", "tenants", "acme")
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestTenantScopedDir_EmptySlugFallsBackToID(t *testing.T) {
	tid := uuid.MustParse("0193b000-0000-7000-8000-000000000002")
	got := TenantScopedDir("/data", tid.String(), "")
	want := filepath.Join("/data", "tenants", tid.String())
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestTenantScopedDir_TraversalDefense(t *testing.T) {
	tid := uuid.MustParse("0193b000-0000-7000-8000-000000000002")
	got := TenantScopedDir("/data", tid.String(), "../../etc")
	want := filepath.Join("/data", "tenants", tid.String())
	if got != want {
		t.Errorf("malicious slug should fall back to ID-based path, got %s, want %s", got, want)
	}
}

// TestTenantDataDir_TenantWorkspace_DelegateToCanonical pins that both
// exported wrappers produce identical output shapes via the single
// canonical TenantScopedDir implementation — no independent join logic.
func TestTenantDataDir_TenantWorkspace_DelegateToCanonical(t *testing.T) {
	tid := uuid.MustParse("0193b000-0000-7000-8000-000000000002")

	dataDir := TenantDataDir("/data", tid, "acme")
	wantDataDir := filepath.Join("/data", "tenants", "acme")
	if dataDir != wantDataDir {
		t.Errorf("TenantDataDir = %s, want %s", dataDir, wantDataDir)
	}

	workspace := TenantWorkspace("/ws", tid, "acme")
	wantWorkspace := filepath.Join("/ws", "tenants", "acme")
	if workspace != wantWorkspace {
		t.Errorf("TenantWorkspace = %s, want %s", workspace, wantWorkspace)
	}
}

func TestTenantTeamDir(t *testing.T) {
	tid := uuid.MustParse("0193b000-0000-7000-8000-000000000002")
	teamID := uuid.MustParse("0193c000-0000-7000-8000-000000000003")
	got := TenantTeamDir("/data", tid, "acme", teamID)
	want := filepath.Join("/data", "tenants", "acme", "teams", teamID.String())
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}
