package store

import (
	"context"
	"time"
)

// ScreencastSession represents a live view session token.
type ScreencastSession struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenantId"`
	Token     string    `json:"token"`
	TargetID  string    `json:"targetId"` // browser tab target ID
	Mode      string    `json:"mode"`     // "view" or "takeover"
	CreatedBy string    `json:"createdBy,omitempty"`
	ExpiresAt time.Time `json:"expiresAt"`
	CreatedAt time.Time `json:"createdAt"`
}

// ScreencastSessionStore manages live view sessions.
type ScreencastSessionStore interface {
	Create(ctx context.Context, s *ScreencastSession) error
	GetByToken(ctx context.Context, token string) (*ScreencastSession, error)
	Delete(ctx context.Context, id string) error
	DeleteExpired(ctx context.Context) (int, error)
}
