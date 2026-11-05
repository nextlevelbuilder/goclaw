package store

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

// Secret kinds stored in the secret vault.
const (
	SecretKindAPIKey = "api_key"
	SecretKindLogin  = "login"
)

// Audit actions recorded in the secret vault audit trail.
const (
	SecretAuditCreated  = "created"
	SecretAuditUpdated  = "updated"
	SecretAuditDeleted  = "deleted"
	SecretAuditUsed     = "used"
	SecretAuditDenied   = "denied"
	SecretAuditApproved = "approved"
)

// SecretAuditOutcomeAgentPending marks the created audit row of an entry an
// agent wrote; the entry waits for user approval.
const SecretAuditOutcomeAgentPending = "agent_pending"

// Entry status. Only active entries are ever resolved for an agent.
const (
	SecretVaultStatusActive  = "active"
	SecretVaultStatusPending = "pending"
)

// Entry origin.
const (
	SecretVaultCreatedViaUser  = "user"
	SecretVaultCreatedViaAgent = "agent"
)

// SecretVaultMaxPendingPerAgent caps how many pending entries one agent may
// hold at a time, so a misbehaving agent cannot flood the approval queue.
const SecretVaultMaxPendingPerAgent = 20

var (
	// ErrSecretVaultNoKey is returned when the store has no encryption key.
	// The vault fails closed: crypto.Encrypt returns plaintext for an empty key.
	ErrSecretVaultNoKey = errors.New("secret vault: encryption key not configured")
	// ErrSecretVaultNotEncrypted is returned when a stored payload lacks the
	// encryption prefix; the vault never accepts plaintext on read.
	ErrSecretVaultNotEncrypted = errors.New("secret vault: stored payload is not encrypted")
	ErrSecretVaultNotFound     = errors.New("secret vault: secret not found")
	ErrSecretVaultNotGranted   = errors.New("secret vault: secret not granted to agent")
	ErrSecretVaultNameTaken    = errors.New("secret vault: name already exists")
	ErrSecretVaultAgentInvalid = errors.New("secret vault: agent not found in tenant")
	ErrSecretVaultNoTenant     = errors.New("secret vault: tenant required")
	ErrSecretVaultInvalid      = errors.New("secret vault: invalid input")
	ErrSecretVaultNotPending   = errors.New("secret vault: secret is not pending approval")
	ErrSecretVaultPendingLimit = errors.New("secret vault: too many secrets pending approval for this agent")
)

// SecretVaultEntry is the non-secret metadata of a stored secret.
// It never carries the secret value; the value is only returned by
// SecretVaultStore.ResolveForUse.
type SecretVaultEntry struct {
	ID              uuid.UUID `json:"id" db:"id"`
	TenantID        uuid.UUID `json:"tenant_id" db:"tenant_id"`
	Name            string    `json:"name" db:"name"`
	Kind            string    `json:"kind" db:"kind"`
	AllowedDomains  []string  `json:"allowed_domains"`
	RequireApproval bool      `json:"require_approval" db:"require_approval"`
	Hints           string    `json:"hints" db:"hints"`
	CreatedBy       string    `json:"created_by" db:"created_by"`
	// Status is active or pending. Create derives it from the origin: an
	// agent-created entry is always pending, a user-created one always active.
	Status string `json:"status" db:"status"`
	// CreatedVia is user or agent; Create sets it from CreatedByAgent.
	CreatedVia string `json:"created_via" db:"created_via"`
	// CreatedByAgent is the agent that wrote the entry, nil for user entries
	// and after that agent is deleted. Setting it on Create marks the entry
	// as agent-created.
	CreatedByAgent *uuid.UUID `json:"created_by_agent" db:"created_by_agent"`
	CreatedAt      time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at" db:"updated_at"`
}

// SecretVaultPayload is the secret material. For kind api_key only Secret is
// used; for kind login both Username and Secret (the password) are required.
// It redacts itself when formatted or logged.
type SecretVaultPayload struct {
	Username string
	Secret   string
}

func (SecretVaultPayload) String() string   { return "[REDACTED]" }
func (SecretVaultPayload) GoString() string { return "[REDACTED]" }

// LogValue keeps the payload out of structured logs.
func (SecretVaultPayload) LogValue() slog.Value { return slog.StringValue("[REDACTED]") }

// SecretVaultMetaUpdate changes metadata only; nil fields are left unchanged.
type SecretVaultMetaUpdate struct {
	AllowedDomains  *[]string
	RequireApproval *bool
	Hints           *string
}

// SecretVaultAuditEntry is one row of the vault audit trail. It never holds
// the secret value or any derivative of it.
type SecretVaultAuditEntry struct {
	ID         uuid.UUID  `json:"id"`
	TenantID   uuid.UUID  `json:"tenant_id"`
	EntryID    uuid.UUID  `json:"entry_id"`
	EntryName  string     `json:"entry_name"`
	AgentID    *uuid.UUID `json:"agent_id,omitempty"`
	UserID     string     `json:"user_id"`
	Action     string     `json:"action"`
	TargetHost string     `json:"target_host"`
	Outcome    string     `json:"outcome"`
	CreatedAt  time.Time  `json:"created_at"`
}

// SecretVaultResolved pairs an entry's metadata with its decrypted payload.
type SecretVaultResolved struct {
	Entry   SecretVaultEntry
	Payload SecretVaultPayload
}

// SecretVaultStore persists encrypted secrets scoped to a tenant, with
// per-agent grants and an audit trail. All methods are tenant-scoped from the
// context tenant; a context without a tenant is rejected (no cross-tenant or
// master fallback). Values are write-only: only ResolveForUse returns them.
type SecretVaultStore interface {
	// Create validates and stores a new entry. e.ID is generated when nil.
	// It never replaces an existing entry: a taken name returns
	// ErrSecretVaultNameTaken. When e.CreatedByAgent is set the entry is
	// agent-created: the agent must belong to the tenant
	// (ErrSecretVaultAgentInvalid), the entry is stored pending, the agent's
	// pending cap is enforced (ErrSecretVaultPendingLimit), and the created
	// audit row (agent id, outcome agent_pending) is written in the same
	// transaction. User-created entries are stored active; their caller audits.
	Create(ctx context.Context, e *SecretVaultEntry, p SecretVaultPayload) error
	// Approve activates a pending entry with the user-confirmed allowed
	// domains and writes the approved audit row with the context user id.
	// Returns ErrSecretVaultNotPending when the entry is already active.
	Approve(ctx context.Context, id uuid.UUID, domains []string) error
	// CountPendingByAgent counts the pending entries the agent created.
	CountPendingByAgent(ctx context.Context, agentID uuid.UUID) (int, error)
	// ReplaceValue overwrites the stored secret value.
	ReplaceValue(ctx context.Context, id uuid.UUID, p SecretVaultPayload) error
	// UpdateMeta changes allowed domains, approval flag or hints.
	UpdateMeta(ctx context.Context, id uuid.UUID, u SecretVaultMetaUpdate) error
	Get(ctx context.Context, id uuid.UUID) (*SecretVaultEntry, error)
	List(ctx context.Context) ([]SecretVaultEntry, error)
	Delete(ctx context.Context, id uuid.UUID) error

	// SetGrants replaces the set of agents allowed to use the entry.
	SetGrants(ctx context.Context, entryID uuid.UUID, agentIDs []uuid.UUID) error
	ListGrants(ctx context.Context, entryID uuid.UUID) ([]uuid.UUID, error)
	// ListGrantedMeta returns metadata of the active entries granted to an agent.
	ListGrantedMeta(ctx context.Context, agentID uuid.UUID) ([]SecretVaultEntry, error)

	// ResolveForUse returns metadata and the decrypted value for an active
	// entry granted to the agent. A pending entry returns
	// ErrSecretVaultNotGranted, like an ungranted one. Runtime use only;
	// never expose through an API.
	ResolveForUse(ctx context.Context, agentID uuid.UUID, name string) (*SecretVaultEntry, SecretVaultPayload, error)

	// ListGrantedSecrets returns every active entry granted to the agent with its
	// decrypted value, in one query. Runtime use only: the agent loop preloads
	// the redactor with it. Never expose through an API.
	ListGrantedSecrets(ctx context.Context, agentID uuid.UUID) ([]SecretVaultResolved, error)

	AppendAudit(ctx context.Context, a *SecretVaultAuditEntry) error
	ListAudit(ctx context.Context, entryID uuid.UUID, limit int) ([]SecretVaultAuditEntry, error)
}
