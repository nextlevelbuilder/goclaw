package pg

import (
	"context"
	"database/sql"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// PGUserStore implements store.UserStore backed by PostgreSQL.
type PGUserStore struct {
	db *sql.DB
}

// NewPGUserStore creates a new PGUserStore.
func NewPGUserStore(db *sql.DB) *PGUserStore {
	return &PGUserStore{db: db}
}

// UpsertLogin creates or updates a user's last login timestamp.
// Returns the user with the updated timestamp.
func (s *PGUserStore) UpsertLogin(ctx context.Context, userID string) (*store.User, error) {
	var user store.User
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO users (id, last_login_at)
		 VALUES ($1, NOW())
		 ON CONFLICT (id) DO UPDATE SET last_login_at = NOW()
		 RETURNING id, last_login_at`,
		userID,
	).Scan(&user.ID, &user.LastLoginAt)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// GetUser retrieves a user by ID. Returns nil if not found.
func (s *PGUserStore) GetUser(ctx context.Context, userID string) (*store.User, error) {
	var user store.User
	err := s.db.QueryRowContext(ctx,
		`SELECT id, last_login_at FROM users WHERE id = $1`,
		userID,
	).Scan(&user.ID, &user.LastLoginAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
}
