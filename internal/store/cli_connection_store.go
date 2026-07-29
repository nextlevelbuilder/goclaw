package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// CLIConnection is a connected external agent (Claude Code, Codex, Aider, Gemini
// CLI, …) owned by a TENANT rather than by one agent. Every agent in the tenant
// may delegate to any enabled connection — there is deliberately no per-agent
// grant table, so connecting a CLI once makes it available everywhere.
//
// Supersedes the per-agent `agents.connected_agents` JSONB (migration 000082).
// That column is still readable this release (expand/contract) but is no longer
// the source of truth.
type CLIConnection struct {
	ID uuid.UUID
	// TenantID nil means a GLOBAL connection: visible to every tenant, mutable
	// only by a platform admin. Mirrors the mcp_servers scoping model.
	TenantID *uuid.UUID
	Name     string // display name, e.g. "Claude Code"
	Kind     string // external_cli | mcp | a2a | http
	Provider string // claude_code | codex | aider | gemini_cli | generic
	Mode     string // delegate | engine
	Endpoint string
	Enabled  bool
	// Config carries provider-specific settings. For provider="generic" it holds
	// the whole command contract ({binary, task_args, cred_env, output}) so an
	// arbitrary CLI can be wired without a code change.
	Config    json.RawMessage
	CreatedBy string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// SharedCredentialUserID is the sentinel user_id for a credential that belongs to
// the whole tenant rather than one user. An explicit empty string (not NULL) keeps
// it inside the (connection_id, user_id) primary key.
const SharedCredentialUserID = ""

// CLIConnectionCredential is a secret for a shared connection, scoped to ONE
// USER (BYOK) so a user's own subscription token or API key stays theirs even
// though the connection itself is tenant-wide. UserID == SharedCredentialUserID
// is a tenant-shared fallback that any user resolves to when they have none of
// their own. Stored encrypted at rest; never returned to clients.
type CLIConnectionCredential struct {
	ConnectionID uuid.UUID
	UserID       string
	TenantID     *uuid.UUID
	// Type is the credential kind: "api_key" | "oauth".
	Type string
	// Inject tells the runtime how to deliver the secret into the sandbox exec:
	//   "env:VAR"   → set env var VAR to Secret (e.g. env:CLAUDE_CODE_OAUTH_TOKEN)
	//   "file:PATH" → write Secret to PATH under the sandbox home before running
	Inject    string
	Secret    string // plaintext in memory only; encrypted at rest
	UpdatedAt time.Time
}

// CLIConnectionStore persists tenant-level CLI connections and their per-user
// credentials. Implementations must scope every read to the caller's tenant plus
// global (tenant_id IS NULL) rows.
type CLIConnectionStore interface {
	// ListForTenant returns the tenant's connections plus all global ones. When
	// enabledOnly is true, disabled connections are omitted — that is what the
	// delegation path uses, since a disabled connection must not be reachable.
	ListForTenant(ctx context.Context, tenantID *uuid.UUID, enabledOnly bool) ([]CLIConnection, error)
	// GetByID returns one connection if it is visible to the tenant (its own or
	// global), else (nil, nil).
	GetByID(ctx context.Context, tenantID *uuid.UUID, id uuid.UUID) (*CLIConnection, error)
	// Upsert creates or updates a connection. A zero ID means create.
	Upsert(ctx context.Context, conn *CLIConnection) error
	// Delete removes a connection and, by cascade, its credentials.
	Delete(ctx context.Context, tenantID *uuid.UUID, id uuid.UUID) error

	// PutCredential upserts a credential, encrypting Secret at rest.
	PutCredential(ctx context.Context, cred CLIConnectionCredential) error
	// GetCredential resolves the secret for (connectionID, userID) with Secret
	// decrypted. It MUST fall back to the tenant-shared row
	// (UserID == SharedCredentialUserID) when the user has none of their own, and
	// return (nil, nil) when neither exists.
	GetCredential(ctx context.Context, connectionID uuid.UUID, userID string) (*CLIConnectionCredential, error)
	// DeleteCredential removes one user's credential; no-op if absent.
	DeleteCredential(ctx context.Context, connectionID uuid.UUID, userID string) error
}
