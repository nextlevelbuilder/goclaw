package config

import (
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// masterTenantID mirrors store.MasterTenantID.
// Duplicated here to avoid an import cycle (store imports config).
var masterTenantID = uuid.MustParse("0193a5b0-7000-7000-8000-000000000001")

// TenantDataDir returns the data directory root for a tenant.
// Master tenant returns dataDir unchanged (backward compat).
// Other tenants return dataDir/tenants/{slug}/.
func TenantDataDir(dataDir string, tenantID uuid.UUID, tenantSlug string) string {
	if tenantID == masterTenantID {
		return dataDir
	}
	// Empty slug (transient DB lookup failure) would resolve to the tenants/ parent dir,
	// granting cross-tenant access. Fall back to UUID-based path instead.
	if tenantSlug == "" {
		return filepath.Join(dataDir, "tenants", tenantID.String())
	}
	result := filepath.Join(dataDir, "tenants", tenantSlug)
	// Defense-in-depth: prevent path traversal via malicious slug.
	tenantsBase := filepath.Join(dataDir, "tenants") + string(filepath.Separator)
	if !strings.HasPrefix(result+string(filepath.Separator), tenantsBase) {
		return filepath.Join(dataDir, "tenants", tenantID.String())
	}
	return result
}

// TenantWorkspace returns the workspace root for a tenant.
// Master tenant returns workspace unchanged (backward compat).
// Other tenants return workspace/tenants/{slug}/.
func TenantWorkspace(workspace string, tenantID uuid.UUID, tenantSlug string) string {
	if tenantID == masterTenantID {
		return workspace
	}
	// Empty slug (transient DB lookup failure) would resolve to the tenants/ parent dir,
	// granting cross-tenant access. Fall back to UUID-based path instead.
	if tenantSlug == "" {
		return filepath.Join(workspace, "tenants", tenantID.String())
	}
	result := filepath.Join(workspace, "tenants", tenantSlug)
	tenantsBase := filepath.Join(workspace, "tenants") + string(filepath.Separator)
	if !strings.HasPrefix(result+string(filepath.Separator), tenantsBase) {
		return filepath.Join(workspace, "tenants", tenantID.String())
	}
	return result
}

// TenantTeamDir returns the team workspace directory for a tenant.
func TenantTeamDir(dataDir string, tenantID uuid.UUID, tenantSlug string, teamID uuid.UUID) string {
	return filepath.Join(TenantDataDir(dataDir, tenantID, tenantSlug), "teams", teamID.String())
}

// TenantSkillsStoreDir returns the managed skills directory for a tenant.
func TenantSkillsStoreDir(dataDir string, tenantID uuid.UUID, tenantSlug string) string {
	return filepath.Join(TenantDataDir(dataDir, tenantID, tenantSlug), "skills-store")
}

// TenantMediaDir returns the media storage directory for a tenant.
func TenantMediaDir(dataDir string, tenantID uuid.UUID, tenantSlug string) string {
	return filepath.Join(TenantDataDir(dataDir, tenantID, tenantSlug), "media")
}
