package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// GatewayUserData represents a gateway user with role-based access.
type GatewayUserData struct {
	ID           uuid.UUID `json:"id"`
	UserID       string    `json:"user_id"`
	GatewayToken string    `json:"gateway_token,omitempty"` // omitted in list responses
	TokenHint    string    `json:"token_hint,omitempty"`    // masked preview: first4...last4
	Role         string    `json:"role"`                    // "root" or "admin"
	CreatedAt    time.Time `json:"created_at"`
}

// MaskToken returns a masked preview of a gateway token (first 4 + last 4 chars).
func MaskToken(token string) string {
	if len(token) <= 8 {
		return "****"
	}
	return token[:4] + "..." + token[len(token)-4:]
}

// GatewayUserStore manages gateway users.
type GatewayUserStore interface {
	// Create inserts a new gateway user.
	Create(ctx context.Context, user *GatewayUserData) error

	// GetByID returns a user by primary key.
	GetByID(ctx context.Context, id uuid.UUID) (*GatewayUserData, error)

	// GetByToken looks up a user by gateway token.
	GetByToken(ctx context.Context, token string) (*GatewayUserData, error)

	// GetByUserID looks up a user by user_id.
	GetByUserID(ctx context.Context, userID string) (*GatewayUserData, error)

	// List returns all gateway users (tokens are excluded).
	List(ctx context.Context) ([]GatewayUserData, error)

	// Update updates a gateway user's mutable fields.
	Update(ctx context.Context, id uuid.UUID, fields map[string]any) error

	// Delete permanently removes a gateway user. Root users cannot be deleted.
	Delete(ctx context.Context, id uuid.UUID) error

	// EnsureRoot creates the root user if it does not exist, or updates its token.
	EnsureRoot(ctx context.Context, gatewayToken string) error
}
