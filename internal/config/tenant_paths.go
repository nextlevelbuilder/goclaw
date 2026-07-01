package config

import (
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// masterTenantID mirrors store.MasterTenantID.
// Duplicated here to avoid an import cycle (store imports config).
var masterTenantID = uuid.MustParse("0193a5b0-7000-7000-8000-000000000001")

// TenantScopedDir is the single canonical implementation for joining a base
// directory with a tenant-scoped "tenants/{slug-or-id}" subdirectory.
// It is the sole source of truth for this join: TenantDataDir and
// TenantWorkspace (uuid.UUID-typed call sites) delegate to it, and
// internal/workspace's team/personal workspace resolution (string-typed
// tenant identifiers, not always parseable as uuid.UUID) delegates to it too.
//
// Master tenant (tenantIDStr == "" or the master sentinel) returns base
// unchanged (backward compat).
// Empty tenantSlug (transient DB lookup failure) would otherwise resolve to
// the tenants/ parent dir, granting cross-tenant access — falls back to an
// ID-based path instead.
// Includes defense-in-depth path traversal protection against a malicious slug.
func TenantScopedDir(base, tenantIDStr, tenantSlug string) string {
	if tenantIDStr == "" || tenantIDStr == masterTenantID.String() {
		return base
	}
	if tenantSlug == "" {
		return filepath.Join(base, "tenants", tenantIDStr)
	}
	result := filepath.Join(base, "tenants", tenantSlug)
	tenantsBase := filepath.Join(base, "tenants") + string(filepath.Separator)
	if !strings.HasPrefix(result+string(filepath.Separator), tenantsBase) {
		return filepath.Join(base, "tenants", tenantIDStr)
	}
	return result
}

// TenantDataDir returns the data directory root for a tenant.
// Master tenant returns dataDir unchanged (backward compat).
// Other tenants return dataDir/tenants/{slug}/.
func TenantDataDir(dataDir string, tenantID uuid.UUID, tenantSlug string) string {
	return TenantScopedDir(dataDir, tenantID.String(), tenantSlug)
}

// TenantWorkspace returns the workspace root for a tenant.
// Master tenant returns workspace unchanged (backward compat).
// Other tenants return workspace/tenants/{slug}/.
func TenantWorkspace(workspace string, tenantID uuid.UUID, tenantSlug string) string {
	return TenantScopedDir(workspace, tenantID.String(), tenantSlug)
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
