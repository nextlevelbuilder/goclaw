package pg

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// PGGatewayUserStore implements store.GatewayUserStore using PostgreSQL.
type PGGatewayUserStore struct {
	db *sql.DB
}

// NewPGGatewayUserStore creates a new PostgreSQL-backed gateway user store.
func NewPGGatewayUserStore(db *sql.DB) *PGGatewayUserStore {
	return &PGGatewayUserStore{db: db}
}

// gatewayUserAllowedFields is a defense-in-depth allowlist for UPDATE.
var gatewayUserAllowedFields = map[string]bool{
	"user_id":       true,
	"gateway_token": true,
	"role":          true,
}

func (s *PGGatewayUserStore) Create(ctx context.Context, user *store.GatewayUserData) error {
	if user.ID == uuid.Nil {
		user.ID = store.GenNewID()
	}
	if user.CreatedAt.IsZero() {
		user.CreatedAt = time.Now()
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO gateway_users (id, user_id, gateway_token, role, created_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		user.ID, user.UserID, user.GatewayToken, user.Role, user.CreatedAt,
	)
	return err
}

func (s *PGGatewayUserStore) GetByID(ctx context.Context, id uuid.UUID) (*store.GatewayUserData, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, gateway_token, role, created_at
		 FROM gateway_users WHERE id = $1`, id,
	)
	return scanGatewayUser(row)
}

func (s *PGGatewayUserStore) GetByToken(ctx context.Context, token string) (*store.GatewayUserData, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, gateway_token, role, created_at
		 FROM gateway_users WHERE gateway_token = $1`, token,
	)
	return scanGatewayUser(row)
}

func (s *PGGatewayUserStore) GetByUserID(ctx context.Context, userID string) (*store.GatewayUserData, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, gateway_token, role, created_at
		 FROM gateway_users WHERE user_id = $1`, userID,
	)
	return scanGatewayUser(row)
}

func (s *PGGatewayUserStore) List(ctx context.Context) ([]store.GatewayUserData, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, gateway_token, role, created_at
		 FROM gateway_users ORDER BY created_at ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []store.GatewayUserData
	for rows.Next() {
		var u store.GatewayUserData
		if err := rows.Scan(&u.ID, &u.UserID, &u.GatewayToken, &u.Role, &u.CreatedAt); err != nil {
			return nil, err
		}
		u.TokenHint = store.MaskToken(u.GatewayToken)
		users = append(users, u)
	}
	return users, rows.Err()
}

func (s *PGGatewayUserStore) Update(ctx context.Context, id uuid.UUID, fields map[string]any) error {
	var sets []string
	var args []any
	idx := 1

	for k, v := range fields {
		if !gatewayUserAllowedFields[k] {
			return fmt.Errorf("field %q not allowed for update", k)
		}
		sets = append(sets, fmt.Sprintf("%s = $%d", k, idx))
		args = append(args, v)
		idx++
	}
	if len(sets) == 0 {
		return nil
	}

	query := fmt.Sprintf("UPDATE gateway_users SET %s WHERE id = $%d",
		strings.Join(sets, ", "), idx)
	args = append(args, id)

	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *PGGatewayUserStore) Delete(ctx context.Context, id uuid.UUID) error {
	// Prevent deleting root users
	var role string
	err := s.db.QueryRowContext(ctx,
		`SELECT role FROM gateway_users WHERE id = $1`, id,
	).Scan(&role)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return sql.ErrNoRows
		}
		return err
	}
	if role == "root" {
		return fmt.Errorf("cannot delete root user")
	}

	res, err := s.db.ExecContext(ctx,
		`DELETE FROM gateway_users WHERE id = $1`, id,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *PGGatewayUserStore) EnsureRoot(ctx context.Context, gatewayToken string) error {
	if gatewayToken == "" {
		return nil
	}

	// Upsert: insert root user or update its token if already exists.
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO gateway_users (id, user_id, gateway_token, role, created_at)
		 VALUES ($1, 'root', $2, 'root', $3)
		 ON CONFLICT (user_id) DO UPDATE SET gateway_token = EXCLUDED.gateway_token`,
		store.GenNewID(), gatewayToken, time.Now(),
	)
	return err
}

// scanGatewayUser scans a single row into GatewayUserData.
func scanGatewayUser(row *sql.Row) (*store.GatewayUserData, error) {
	var u store.GatewayUserData
	err := row.Scan(&u.ID, &u.UserID, &u.GatewayToken, &u.Role, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

