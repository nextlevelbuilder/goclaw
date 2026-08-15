package tools

import (
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/config"
)

// WorkspaceLayer transforms a base path into a scoped path.
// Returns base unchanged if the layer is not applicable (no-op).
type WorkspaceLayer func(base string) string

// ResolveWorkspace applies layers sequentially to produce the final workspace path.
// Each layer either appends a path segment or returns base unchanged (no-op).
func ResolveWorkspace(base string, layers ...WorkspaceLayer) string {
	for _, layer := range layers {
		base = layer(base)
	}
	return base
}

// TenantLayer scopes to tenant subdirectory.
// Master tenant is a no-op (backward compat — returns base unchanged).
func TenantLayer(tenantID uuid.UUID, slug string) WorkspaceLayer {
	return func(base string) string {
		return config.TenantWorkspace(base, tenantID, slug)
	}
}

// TeamLayer scopes to team subdirectory: {base}/teams/{teamID}.
// Nil teamID is a no-op.
func TeamLayer(teamID uuid.UUID) WorkspaceLayer {
	return func(base string) string {
		if teamID == uuid.Nil {
			return base
		}
		return filepath.Join(base, "teams", teamID.String())
	}
}

// ProjectLayer scopes to project subdirectory: {base}/projects/{projectID}.
// Nil projectID is a no-op. Reserved for future use.
func ProjectLayer(projectID *uuid.UUID) WorkspaceLayer {
	return func(base string) string {
		if projectID == nil || *projectID == uuid.Nil {
			return base
		}
		return filepath.Join(base, "projects", projectID.String())
	}
}

// UserChatLayer scopes to per-user or per-chat subdirectory: {base}/{segment}.
// Empty segment or shared=true is a no-op.
// The segment should already be sanitized via SanitizePathSegment if it contains user input.
func UserChatLayer(segment string, shared bool) WorkspaceLayer {
	return func(base string) string {
		if shared || segment == "" {
			return base
		}
		return filepath.Join(base, segment)
	}
}

// SanitizePathSegment makes a string safe for use as a directory name.
// Replaces colons, spaces, and other unsafe chars with underscores.
// Used to convert userIDs and chatIDs into safe filesystem path segments.
func SanitizePathSegment(s string) string {
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
