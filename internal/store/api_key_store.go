package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// APIKeyData represents a gateway API key with scoped permissions.
type APIKeyData struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`      // first 8 chars for display
	KeyHash    string     `json:"-"`            // SHA-256 hex, never serialized
	Scopes     []string   `json:"scopes"`       // e.g. ["operator.admin","operator.read"]
	ExpiresAt  *time.Time `json:"expires_at"`   // nil = never
	LastUsedAt *time.Time `json:"last_used_at"`
	Revoked    bool       `json:"revoked"`
	CreatedBy  string     `json:"created_by"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// APIKeyStore manages gateway API keys.
type APIKeyStore interface {
	// Create inserts a new API key.
	Create(ctx context.Context, key *APIKeyData) error

	// GetByHash looks up an active (non-revoked, non-expired) key by its SHA-256 hash.
	GetByHash(ctx context.Context, keyHash string) (*APIKeyData, error)

	// List returns all API keys (for admin display; key hashes are not included).
	List(ctx context.Context) ([]APIKeyData, error)

	// Revoke marks a key as revoked.
	Revoke(ctx context.Context, id uuid.UUID) error

	// Delete permanently removes a key.
	Delete(ctx context.Context, id uuid.UUID) error

	// TouchLastUsed updates the last_used_at timestamp.
	TouchLastUsed(ctx context.Context, id uuid.UUID) error
}
