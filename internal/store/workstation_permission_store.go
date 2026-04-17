package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// WorkstationPermission is a single allowlist entry for a workstation.
// Pattern matches against argv[0] binary name only (not the full command string).
// Examples: "git", "npm", "python*" (prefix-glob).
// Default-deny: if no enabled pattern matches, exec is rejected.
type WorkstationPermission struct {
	ID            uuid.UUID `json:"id"`
	WorkstationID uuid.UUID `json:"workstationId"`
	TenantID      uuid.UUID `json:"tenantId"`
	// Pattern is the binary name or prefix-glob (e.g. "git", "python*").
	// Wildcard "*" alone is intentionally NOT supported — too permissive.
	Pattern   string    `json:"pattern"`
	Enabled   bool      `json:"enabled"`
	CreatedBy string    `json:"createdBy"`
	CreatedAt time.Time `json:"createdAt"`
}

// WorkstationPermissionStore manages per-workstation binary allowlist entries.
// All queries are tenant-scoped; never cross-tenant reads/writes.
type WorkstationPermissionStore interface {
	// ListForWorkstation returns all entries for the given workstation (any enabled state).
	// Caller must filter by enabled if needed.
	ListForWorkstation(ctx context.Context, workstationID uuid.UUID) ([]WorkstationPermission, error)

	// Add inserts a new allowlist entry. Idempotent on (workstation_id, pattern).
	Add(ctx context.Context, perm *WorkstationPermission) error

	// Remove deletes an allowlist entry by ID (tenant-scoped).
	Remove(ctx context.Context, id uuid.UUID) error

	// SetEnabled enables or disables an entry by ID (tenant-scoped).
	SetEnabled(ctx context.Context, id uuid.UUID, enabled bool) error

	// SeedDefaults inserts the default safe binary names for a new workstation.
	// Uses INSERT OR IGNORE / ON CONFLICT DO NOTHING — safe to call multiple times.
	// Intended to be called inside the workstation Create transaction (H5 fix).
	SeedDefaults(ctx context.Context, workstationID, tenantID uuid.UUID) error
}

// DefaultAllowedBinaries is the set of binary names seeded when a workstation is created.
// These are safe, read-only or low-risk commands. Admin must add anything else.
// NOTE: shells (bash, sh, zsh) are intentionally excluded — adding a shell binary
// bypasses all protection by allowing arbitrary commands as arguments.
var DefaultAllowedBinaries = []string{
	"echo", "pwd", "ls", "cat", "git", "env",
	"whoami", "hostname", "date", "uname", "claude",
}
