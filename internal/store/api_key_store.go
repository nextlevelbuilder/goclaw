package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// APIKeyData represents a gateway API key with scoped permissions.
type APIKeyData struct {
	ID         uuid.UUID  `json:"id" db:"id"`
	TenantID   uuid.UUID  `json:"tenant_id" db:"tenant_id"` // uuid.Nil when NULL in DB
	Name       string     `json:"name" db:"name"`
	Prefix     string     `json:"prefix" db:"prefix"`               // first 8 chars for display
	KeyHash    string     `json:"-" db:"key_hash"`                  // SHA-256 hex, never serialized
	Scopes     []string   `json:"scopes" db:"scopes"`               // e.g. ["operator.admin","operator.read"]
	OwnerID    string     `json:"owner_id,omitempty" db:"owner_id"` // bound user; when set, auth forces user_id = owner_id
	ExpiresAt  *time.Time `json:"expires_at" db:"expires_at"`       // nil = never
	LastUsedAt *time.Time `json:"last_used_at" db:"last_used_at"`
	Revoked    bool       `json:"revoked" db:"revoked"`
	CreatedBy  string     `json:"created_by" db:"created_by"`
	CreatedAt  time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at" db:"updated_at"`
}

// APIKeyStore manages gateway API keys.
type APIKeyStore interface {
	// Create inserts a new API key.
	Create(ctx context.Context, key *APIKeyData) error

	// GetByHash looks up an active (non-revoked, non-expired) key by its SHA-256 hash.
	GetByHash(ctx context.Context, keyHash string) (*APIKeyData, error)

	// List returns API keys. If ownerID is non-empty, filters to keys owned by that user.
	List(ctx context.Context, ownerID string) ([]APIKeyData, error)

	// Revoke marks a key as revoked. If ownerID is non-empty, also enforces owner_id = ownerID.
	Revoke(ctx context.Context, id uuid.UUID, ownerID string) error

	// Delete permanently removes a key. If ownerID is non-empty, also enforces owner_id = ownerID.
	Delete(ctx context.Context, id uuid.UUID, ownerID string) error

	// TouchLastUsed updates the last_used_at timestamp.
	TouchLastUsed(ctx context.Context, id uuid.UUID) error
}
