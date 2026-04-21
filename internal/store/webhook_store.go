package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrIdempotencyConflict is returned when a webhook_call with the same
// (webhook_id, idempotency_key) already exists (partial unique index violation).
var ErrIdempotencyConflict = errors.New("idempotency key conflict: call already exists")

// ErrLeaseExpired is returned by UpdateStatusCAS when 0 rows were affected,
// meaning the row's lease_token no longer matches — it was reclaimed by reclaimStale
// and possibly re-claimed by another worker iteration. The caller should log and drop.
var ErrLeaseExpired = errors.New("webhook call lease expired: row reclaimed by stale sweeper")

// WebhookData represents a registered webhook.
// SecretHash is never serialized to JSON (auth token, server-side only).
// EncryptedSecret holds crypto.Encrypt(raw_secret, encKey) — decrypted at HMAC sign time.
// Existing webhooks with EncryptedSecret="" require rotation before HMAC auth is accepted.
type WebhookData struct {
	ID              uuid.UUID  `json:"id" db:"id"`
	TenantID        uuid.UUID  `json:"tenant_id" db:"tenant_id"`
	AgentID         *uuid.UUID `json:"agent_id,omitempty" db:"agent_id"`
	Name            string     `json:"name" db:"name"`
	Kind            string     `json:"kind" db:"kind"` // "llm" | "message"
	SecretPrefix    string     `json:"secret_prefix" db:"secret_prefix"`
	SecretHash      string     `json:"-" db:"secret_hash"`        // SHA-256 hex; bearer-token lookup only; never serialized
	EncryptedSecret string     `json:"-" db:"encrypted_secret"`   // AES-256-GCM of raw secret; never serialized
	Scopes          []string   `json:"scopes" db:"scopes"`
	ChannelID       *uuid.UUID `json:"channel_id,omitempty" db:"channel_id"`
	RateLimitPerMin int        `json:"rate_limit_per_min" db:"rate_limit_per_min"`
	IPAllowlist     []string   `json:"ip_allowlist" db:"ip_allowlist"`
	RequireHMAC     bool       `json:"require_hmac" db:"require_hmac"`
	LocalhostOnly   bool       `json:"localhost_only" db:"localhost_only"`
	Revoked         bool       `json:"revoked" db:"revoked"`
	CreatedBy       string     `json:"created_by" db:"created_by"`
	CreatedAt       time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at" db:"updated_at"`
	LastUsedAt      *time.Time `json:"last_used_at,omitempty" db:"last_used_at"`
}

// WebhookCallData represents a single webhook invocation (queued, in-flight, or terminal).
// DeliveryID is stable across retries — used as X-Webhook-Delivery-Id header.
// StartedAt is set on ClaimNext to detect stale-running calls.
// Attempts is incremented post-send by the worker (NOT on ClaimNext).
// LeaseToken is a random UUID set atomically by ClaimNext; UpdateStatus CAS guards with AND lease_token = $N.
// If CAS hits 0 rows, the row was reclaimed by reclaimStale — the worker logs and drops the update.
type WebhookCallData struct {
	ID             uuid.UUID  `json:"id" db:"id"`
	TenantID       uuid.UUID  `json:"tenant_id" db:"tenant_id"`
	WebhookID      uuid.UUID  `json:"webhook_id" db:"webhook_id"`
	AgentID        *uuid.UUID `json:"agent_id,omitempty" db:"agent_id"`
	DeliveryID     uuid.UUID  `json:"delivery_id" db:"delivery_id"` // stable across retries
	IdempotencyKey *string    `json:"idempotency_key,omitempty" db:"idempotency_key"`
	Mode           string     `json:"mode" db:"mode"`     // "sync" | "async"
	Status         string     `json:"status" db:"status"` // "queued"|"running"|"done"|"failed"|"dead"
	CallbackURL    *string    `json:"callback_url,omitempty" db:"callback_url"`
	Attempts       int        `json:"attempts" db:"attempts"`
	NextAttemptAt  *time.Time `json:"next_attempt_at,omitempty" db:"next_attempt_at"`
	StartedAt      *time.Time `json:"started_at,omitempty" db:"started_at"` // set on ClaimNext
	LeaseToken     *string    `json:"lease_token,omitempty" db:"lease_token"` // CAS guard; set by ClaimNext, cleared by ReclaimStale
	RequestPayload []byte     `json:"request_payload,omitempty" db:"request_payload"`
	Response       []byte     `json:"response,omitempty" db:"response"`
	LastError      *string    `json:"last_error,omitempty" db:"last_error"`
	CreatedAt      time.Time  `json:"created_at" db:"created_at"`
	CompletedAt    *time.Time `json:"completed_at,omitempty" db:"completed_at"`
}

// WebhookListFilter controls filtering for WebhookStore.List.
type WebhookListFilter struct {
	AgentID *uuid.UUID // filter by bound agent (nil = all)
	Limit   int        // 0 = default (50)
	Offset  int
}

// WebhookCallListFilter controls filtering for WebhookCallStore.List.
type WebhookCallListFilter struct {
	WebhookID *uuid.UUID // filter by parent webhook (nil = all in tenant)
	Status    string     // "" = all statuses
	Limit     int        // 0 = default (50)
	Offset    int
}

// WebhookStore manages webhook registry entries.
// All methods are tenant-scoped via context (store.TenantIDFromContext).
type WebhookStore interface {
	// Create inserts a new webhook. ID + CreatedAt + UpdatedAt should be
	// pre-filled by the caller.
	Create(ctx context.Context, w *WebhookData) error

	// GetByID returns a webhook by its UUID.
	// Returns sql.ErrNoRows if not found or tenant mismatch.
	GetByID(ctx context.Context, id uuid.UUID) (*WebhookData, error)

	// GetByHash returns an active (non-revoked) webhook by its secret_hash.
	// Returns sql.ErrNoRows if not found.
	GetByHash(ctx context.Context, secretHash string) (*WebhookData, error)

	// GetByHashUnscoped looks up a webhook by secret_hash WITHOUT requiring tenant
	// in context. Used exclusively in WebhookAuthMiddleware for pre-auth resolution;
	// downstream queries remain tenant-scoped after WithTenantID injection.
	// security_hash is globally unique (uq_webhooks_secret) so no tenant filter needed.
	GetByHashUnscoped(ctx context.Context, secretHash string) (*WebhookData, error)

	// GetByIDUnscoped looks up a webhook by UUID WITHOUT requiring tenant in context.
	// Used exclusively in WebhookAuthMiddleware for HMAC pre-auth resolution.
	GetByIDUnscoped(ctx context.Context, id uuid.UUID) (*WebhookData, error)

	// List returns webhooks for the context tenant, with optional agent filter.
	List(ctx context.Context, f WebhookListFilter) ([]WebhookData, error)

	// Update applies a partial update via column→value map.
	// Caller validates keys; store validates against allowlist.
	Update(ctx context.Context, id uuid.UUID, updates map[string]any) error

	// RotateSecret replaces the secret_hash, secret_prefix, and encrypted_secret.
	// Callers (webhooks_admin.go) generate hash + prefix + encrypted form above the store layer.
	RotateSecret(ctx context.Context, id uuid.UUID, newSecretHash, newPrefix, newEncryptedSecret string) error

	// Revoke marks a webhook as revoked. Returns sql.ErrNoRows if not found.
	Revoke(ctx context.Context, id uuid.UUID) error

	// TouchLastUsed updates last_used_at. Best-effort — failures are not fatal.
	TouchLastUsed(ctx context.Context, id uuid.UUID) error
}

// WebhookCallStore manages webhook call state (queued → running → terminal).
// All methods are tenant-scoped via context.
type WebhookCallStore interface {
	// Create inserts a new call record (status = "queued").
	// Returns ErrIdempotencyConflict if (webhook_id, idempotency_key) already exists.
	Create(ctx context.Context, call *WebhookCallData) error

	// GetByID returns a call by its UUID.
	// Returns sql.ErrNoRows if not found or tenant mismatch.
	GetByID(ctx context.Context, id uuid.UUID) (*WebhookCallData, error)

	// GetByIdempotency returns the existing call for a given (webhookID, key).
	// Returns sql.ErrNoRows if no match.
	GetByIdempotency(ctx context.Context, webhookID uuid.UUID, key string) (*WebhookCallData, error)

	// UpdateStatus updates mutable fields after a send attempt.
	// Callers may set status, attempts, next_attempt_at, response, last_error, completed_at.
	UpdateStatus(ctx context.Context, id uuid.UUID, updates map[string]any) error

	// UpdateStatusCAS is like UpdateStatus but guards with AND lease_token = lease.
	// Returns ErrLeaseExpired if 0 rows affected (row was reclaimed by reclaimStale).
	// Worker callers must use this instead of UpdateStatus for all post-ClaimNext updates.
	UpdateStatusCAS(ctx context.Context, id uuid.UUID, lease string, updates map[string]any) error

	// ClaimNext atomically claims the next queued call due for processing.
	// Sets status="running", started_at=now, and lease_token=new UUID.
	// Does NOT increment attempts — the worker does that on terminal UpdateStatus.
	// Returns sql.ErrNoRows if the queue is empty.
	ClaimNext(ctx context.Context, tenantID uuid.UUID, now time.Time) (*WebhookCallData, error)

	// List returns calls for the context tenant with optional filters.
	List(ctx context.Context, f WebhookCallListFilter) ([]WebhookCallData, error)

	// DeleteOlderThan deletes terminal calls (done/failed/dead) older than ts.
	// If tenantID is uuid.Nil, deletes across all tenants (retention worker).
	DeleteOlderThan(ctx context.Context, tenantID uuid.UUID, ts time.Time) (int64, error)

	// ReclaimStale resets rows stuck in status='running' with started_at older than
	// staleThreshold back to status='queued'. Called on worker startup and periodically
	// (every 60s) to recover from crashes between ClaimNext and UpdateStatus.
	// Returns the number of rows reclaimed.
	ReclaimStale(ctx context.Context, staleThreshold time.Time) (int64, error)
}
