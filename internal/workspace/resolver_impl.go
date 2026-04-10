package workspace

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// masterTenantID is the sentinel UUID for the master/default tenant.
// Master tenant workspaces use base dir directly (no tenants/ prefix).
// Must match config.masterTenantID in internal/config/tenant_paths.go.
const masterTenantID = "0193a5b0-7000-7000-8000-000000000001"

// defaultResolver implements Resolver for all 6 workspace scenarios.
// Stateless — all inputs come via ResolveParams. No DB queries.
// Does NOT import tools package (avoids circular dependency).
type defaultResolver struct{}

// NewResolver creates a workspace Resolver.
func NewResolver() Resolver { return &defaultResolver{} }

func (r *defaultResolver) Resolve(_ context.Context, params ResolveParams) (*WorkspaceContext, error) {
	if params.BaseDir == "" {
		return nil, fmt.Errorf("workspace: base dir is required")
	}

	// Priority: delegation > team > personal/predefined
	switch {
	case params.DelegateCtx != nil:
		return r.resolveDelegate(params)
	case params.TeamID != nil && *params.TeamID != "":
		return r.resolveTeam(params), nil
	default:
		return r.resolvePersonal(params), nil
	}
}

// resolveDelegate handles delegated task workspace.
// ActivePath = delegate's shared path, read-only exports from delegator.
// Validates SharedPath is under BaseDir to prevent directory traversal.
func (r *defaultResolver) resolveDelegate(p ResolveParams) (*WorkspaceContext, error) {
	shared := filepath.Clean(p.DelegateCtx.SharedPath)
	base := filepath.Clean(p.BaseDir)
	if !strings.HasPrefix(shared+string(filepath.Separator), base+string(filepath.Separator)) {
		return nil, fmt.Errorf("workspace: delegate shared path escapes base dir")
	}

	wc := &WorkspaceContext{
		ActivePath:       shared,
		Scope:            ScopeDelegate,
		ReadOnlyPaths:    p.DelegateCtx.ExportPaths,
		SharedPath:       &p.DelegateCtx.SharedPath,
		OwnerID:          p.UserID,
		MemoryScope:      "user",
		KGScope:          "user",
		EnforcementLabel: DefaultEnforcementLabel(ScopeDelegate, false),
	}
	ensureDir(wc.ActivePath)
	return wc, nil
}

// resolveTeam handles team workspace (shared or isolated).
func (r *defaultResolver) resolveTeam(p ResolveParams) *WorkspaceContext {
	base := tenantPath(p.BaseDir, p.TenantID, p.TenantSlug)
	teamRoot := filepath.Join(base, "teams", sanitizeSegment(*p.TeamID))

	shared := p.TeamConfig.IsShared()
	activePath := teamRoot
	if !shared {
		// Isolated: add chat/user segment
		segment := sanitizeSegment(p.ChatID)
		if segment == "" {
			segment = sanitizeSegment(p.UserID)
		}
		if segment != "" {
			activePath = filepath.Join(teamRoot, segment)
		}
	}

	scope := sharingScope(p)
	wc := &WorkspaceContext{
		ActivePath:       activePath,
		Scope:            ScopeTeam,
		TeamPath:         &teamRoot,
		OwnerID:          ownerID(p),
		MemoryScope:      scope,
		KGScope:          scope,
		EnforcementLabel: DefaultEnforcementLabel(ScopeTeam, shared),
	}
	ensureDirTeam(wc.ActivePath)
	return wc
}

// resolvePersonal handles open agent (per-user) and predefined agent (shared) workspaces.
func (r *defaultResolver) resolvePersonal(p ResolveParams) *WorkspaceContext {
	base := tenantPath(p.BaseDir, p.TenantID, p.TenantSlug)
	agentDir := filepath.Join(base, sanitizeSegment(p.AgentID))

	activePath := agentDir
	shared := p.AgentType == "predefined"
	if !shared {
		segment := userChatSegment(p)
		if segment != "" {
			activePath = filepath.Join(agentDir, segment)
		}
	}

	scope := sharingScope(p)
	wc := &WorkspaceContext{
		ActivePath:       activePath,
		Scope:            ScopePersonal,
		OwnerID:          ownerID(p),
		MemoryScope:      scope,
		KGScope:          scope,
		EnforcementLabel: DefaultEnforcementLabel(ScopePersonal, shared),
	}
	ensureDir(wc.ActivePath)
	return wc
}

// tenantPath returns tenant-scoped directory.
// Master tenant returns base dir directly (backward compat with v2).
// Uses slug when available (matches config.TenantWorkspace), falls back to UUID.
func tenantPath(base, tenantID, tenantSlug string) string {
	if tenantID == "" || tenantID == masterTenantID {
		return base
	}
	segment := tenantSlug
	if segment == "" {
		segment = tenantID
	}
	result := filepath.Join(base, "tenants", sanitizeSegment(segment))
	// Path traversal defense: ensure result stays under tenants/ base
	tenantsBase := filepath.Join(base, "tenants") + string(filepath.Separator)
	if !strings.HasPrefix(result+string(filepath.Separator), tenantsBase) {
		return filepath.Join(base, "tenants", sanitizeSegment(tenantID))
	}
	return result
}

// userChatSegment returns the isolation segment: chatID for group, userID for direct.
func userChatSegment(p ResolveParams) string {
	if p.PeerKind == "group" && p.ChatID != "" {
		return sanitizeSegment(p.ChatID)
	}
	return sanitizeSegment(p.UserID)
}

// ownerID picks the identifying owner: userID or chatID.
func ownerID(p ResolveParams) string {
	if p.UserID != "" {
		return p.UserID
	}
	return p.ChatID
}

// sharingScope returns "shared" or "user" based on team config.
func sharingScope(p ResolveParams) string {
	if p.TeamConfig.IsShared() {
		return "shared"
	}
	return "user"
}

// sanitizeSegment makes a string safe for filesystem path use.
// Mirrors tools.SanitizePathSegment without importing tools package.
func sanitizeSegment(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

// ensureDir creates workspace directory (0755 for personal/delegate).
func ensureDir(path string) {
	if err := os.MkdirAll(path, 0755); err != nil {
		slog.Warn("workspace: failed to create directory", "path", path, "err", err)
	}
}

// ensureDirTeam creates team workspace directory (0750 — more restrictive).
func ensureDirTeam(path string) {
	if err := os.MkdirAll(path, 0750); err != nil {
		slog.Warn("workspace: failed to create team directory", "path", path, "err", err)
	}
}
