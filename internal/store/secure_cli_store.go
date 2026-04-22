package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// SecureCLIBinary represents a CLI binary with auto-injected credentials.
// Credentials are encrypted at rest and injected into child processes via Direct Exec Mode.
type SecureCLIBinary struct {
	BaseModel
	BinaryName     string          `json:"binary_name" db:"binary_name"`
	BinaryPath     *string         `json:"binary_path,omitempty" db:"binary_path"`
	Description    string          `json:"description" db:"description"`
	EncryptedEnv   []byte          `json:"-" db:"encrypted_env"`               // AES-256-GCM encrypted JSON — never serialized to API
	DenyArgs       json.RawMessage `json:"deny_args" db:"deny_args"`       // regex patterns for blocked subcommands
	DenyVerbose    json.RawMessage `json:"deny_verbose" db:"deny_verbose"`    // blocked verbose/debug flags
	TimeoutSeconds int             `json:"timeout_seconds" db:"timeout_seconds"`
	Tips           string          `json:"tips" db:"tips"`            // hint injected into TOOLS.md context
	IsGlobal       bool            `json:"is_global" db:"is_global"`
	Enabled        bool            `json:"enabled" db:"enabled"`
	CreatedBy      string          `json:"created_by" db:"created_by"`
	UserEnv        []byte          `json:"-" db:"-"` // per-user encrypted env (populated by LookupByBinary LEFT JOIN)
	// EnvKeys is set by HTTP handlers only (names from decrypted env, no values); not a DB column.
	EnvKeys []string `json:"env_keys,omitempty" db:"-"`
}

// MergeGrantOverrides applies agent grant overrides onto a binary config.
// Non-nil grant fields replace binary defaults; nil fields keep binary values.
func (b *SecureCLIBinary) MergeGrantOverrides(g *SecureCLIAgentGrant) {
	if g == nil {
		return
	}
	if g.DenyArgs != nil {
		b.DenyArgs = *g.DenyArgs
	}
	if g.DenyVerbose != nil {
		b.DenyVerbose = *g.DenyVerbose
	}
	if g.TimeoutSeconds != nil {
		b.TimeoutSeconds = *g.TimeoutSeconds
	}
	if g.Tips != nil {
		b.Tips = *g.Tips
	}
}

// SecureCLIUserCredential holds per-user encrypted env overrides for a binary.
type SecureCLIUserCredential struct {
	ID           uuid.UUID       `json:"id" db:"id"`
	BinaryID     uuid.UUID       `json:"binary_id" db:"binary_id"`
	UserID       string          `json:"user_id" db:"user_id"`
	Metadata     json.RawMessage `json:"metadata,omitempty" db:"metadata"`
	CreatedAt    string          `json:"created_at" db:"created_at"`
	UpdatedAt    string          `json:"updated_at" db:"updated_at"`
	// EncryptedEnv is decrypted JSON — never serialized to API.
	EncryptedEnv []byte `json:"-" db:"encrypted_env"`
}

// SecureCLIAgentGrant represents a per-agent grant with optional setting overrides.
type SecureCLIAgentGrant struct {
	BaseModel
	BinaryID       uuid.UUID        `json:"binary_id" db:"binary_id"`
	AgentID        uuid.UUID        `json:"agent_id" db:"agent_id"`
	DenyArgs       *json.RawMessage `json:"deny_args,omitempty" db:"deny_args"`
	DenyVerbose    *json.RawMessage `json:"deny_verbose,omitempty" db:"deny_verbose"`
	TimeoutSeconds *int             `json:"timeout_seconds,omitempty" db:"timeout_seconds"`
	Tips           *string          `json:"tips,omitempty" db:"tips"`
	Enabled        bool             `json:"enabled" db:"enabled"`
	CreatedAt      time.Time        `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time        `json:"updated_at" db:"updated_at"`
}

// SecureCLIStore manages secure CLI binary credential configurations.
type SecureCLIStore interface {
	Create(ctx context.Context, b *SecureCLIBinary) error
	Get(ctx context.Context, id uuid.UUID) (*SecureCLIBinary, error)
	Update(ctx context.Context, id uuid.UUID, updates map[string]any) error
	Delete(ctx context.Context, id uuid.UUID) error
	List(ctx context.Context) ([]SecureCLIBinary, error)

	// LookupByBinary finds the credential config for a binary name.
	// If agentID is provided, checks grant authorization and merges overrides.
	// If userID is non-empty, also fetches per-user env overrides via LEFT JOIN.
	LookupByBinary(ctx context.Context, binaryName string, agentID *uuid.UUID, userID string) (*SecureCLIBinary, error)

	// ListEnabled returns all enabled configs (for TOOLS.md context generation).
	ListEnabled(ctx context.Context) ([]SecureCLIBinary, error)

	// ListForAgent returns all CLIs accessible by an agent (global + granted),
	// with grant overrides merged into the returned configs.
	ListForAgent(ctx context.Context, agentID uuid.UUID) ([]SecureCLIBinary, error)

	// IsRegisteredBinary reports whether a binary with the given name is
	// registered and enabled for the tenant in ctx AND requires a grant
	// (is_global = false). Used by the shell exec gate to hard-deny
	// execution of credentialed binaries when the calling agent has no
	// grant. is_global = true binaries are open to all agents and MUST
	// NOT be reported as gate-needing. Tenant-scoped unless IsCrossTenant(ctx).
	// Returns (false, nil) when name is empty or tenant context is missing.
	IsRegisteredBinary(ctx context.Context, binaryName string) (bool, error)

	// --- Per-user credential management ---

	GetUserCredentials(ctx context.Context, binaryID uuid.UUID, userID string) (*SecureCLIUserCredential, error)
	SetUserCredentials(ctx context.Context, binaryID uuid.UUID, userID string, encryptedEnv []byte) error
	DeleteUserCredentials(ctx context.Context, binaryID uuid.UUID, userID string) error
	ListUserCredentials(ctx context.Context, binaryID uuid.UUID) ([]SecureCLIUserCredential, error)
}

// SecureCLIAgentGrantStore manages per-agent grants for secure CLI binaries.
type SecureCLIAgentGrantStore interface {
	Create(ctx context.Context, g *SecureCLIAgentGrant) error
	Get(ctx context.Context, id uuid.UUID) (*SecureCLIAgentGrant, error)
	Update(ctx context.Context, id uuid.UUID, updates map[string]any) error
	Delete(ctx context.Context, id uuid.UUID) error
	ListByBinary(ctx context.Context, binaryID uuid.UUID) ([]SecureCLIAgentGrant, error)
	ListByAgent(ctx context.Context, agentID uuid.UUID) ([]SecureCLIAgentGrant, error)
}
