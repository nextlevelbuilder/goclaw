package store

import (
	"context"
	"time"
)

// User represents an authenticated user record.
type User struct {
	ID          string    `json:"id"`
	LastLoginAt time.Time `json:"last_login_at"`
}

// UserStore manages user login records.
type UserStore interface {
	// UpsertLogin creates or updates a user's last login timestamp.
	UpsertLogin(ctx context.Context, userID string) (*User, error)
	// GetUser retrieves a user by ID. Returns nil if not found.
	GetUser(ctx context.Context, userID string) (*User, error)
}
