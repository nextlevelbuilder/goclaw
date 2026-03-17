package store

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
)

// SecureCLIBinary represents a CLI binary with auto-injected credentials.
// Credentials are encrypted at rest and injected into child processes via Direct Exec Mode.
type SecureCLIBinary struct {
	BaseModel
	BinaryName     string          `json:"binary_name"`
	BinaryPath     *string         `json:"binary_path,omitempty"`
	Description    string          `json:"description"`
	EncryptedEnv   []byte          `json:"-"`               // AES-256-GCM encrypted JSON — never serialized to API
	DenyArgs       json.RawMessage `json:"deny_args"`       // regex patterns for blocked subcommands
	DenyVerbose    json.RawMessage `json:"deny_verbose"`    // blocked verbose/debug flags
	TimeoutSeconds int             `json:"timeout_seconds"`
	Tips           string          `json:"tips"`            // hint injected into TOOLS.md context
	AgentID        *uuid.UUID      `json:"agent_id,omitempty"`
	Enabled        bool            `json:"enabled"`
	CreatedBy      string          `json:"created_by"`
}

// SecureCLIStore manages secure CLI binary credential configurations.
type SecureCLIStore interface {
	Create(ctx context.Context, b *SecureCLIBinary) error
	Get(ctx context.Context, id uuid.UUID) (*SecureCLIBinary, error)
	Update(ctx context.Context, id uuid.UUID, updates map[string]any) error
	Delete(ctx context.Context, id uuid.UUID) error
	List(ctx context.Context) ([]SecureCLIBinary, error)
	ListByAgent(ctx context.Context, agentID uuid.UUID) ([]SecureCLIBinary, error)

	// LookupByBinary finds the best-matching credential config for a binary name.
	// Priority: agent-specific > global (agent_id IS NULL). Returns nil if not found.
	LookupByBinary(ctx context.Context, binaryName string, agentID *uuid.UUID) (*SecureCLIBinary, error)

	// ListEnabled returns all enabled configs (for TOOLS.md context generation).
	ListEnabled(ctx context.Context) ([]SecureCLIBinary, error)
}
