package pg

import (
	"context"
	"database/sql"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type pgScreencastSessionStore struct {
	db *sql.DB
}

func NewScreencastSessionStore(db *sql.DB) *pgScreencastSessionStore {
	return &pgScreencastSessionStore{db: db}
}

func (s *pgScreencastSessionStore) Create(ctx context.Context, ss *store.ScreencastSession) error {
	if ss.TenantID == "" {
		ss.TenantID = tenantIDForInsert(ctx).String()
	}
	return s.db.QueryRowContext(ctx,
		`INSERT INTO screencast_sessions (tenant_id, token, target_id, mode, created_by, expires_at)
		 VALUES ($1,$2,$3,$4,$5,$6)
		 RETURNING id, created_at`,
		ss.TenantID, ss.Token, ss.TargetID, ss.Mode, nilIfEmpty(ss.CreatedBy), ss.ExpiresAt,
	).Scan(&ss.ID, &ss.CreatedAt)
}

func (s *pgScreencastSessionStore) GetByToken(ctx context.Context, token string) (*store.ScreencastSession, error) {
	var ss store.ScreencastSession
	var createdBy sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT id, tenant_id, token, target_id, mode, created_by, expires_at, created_at
		 FROM screencast_sessions WHERE token = $1`, token,
	).Scan(&ss.ID, &ss.TenantID, &ss.Token, &ss.TargetID, &ss.Mode,
		&createdBy, &ss.ExpiresAt, &ss.CreatedAt)
	if err != nil {
		return nil, err
	}
	if createdBy.Valid {
		ss.CreatedBy = createdBy.String
	}
	return &ss, nil
}

func (s *pgScreencastSessionStore) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM screencast_sessions WHERE id = $1`, id)
	return err
}

func (s *pgScreencastSessionStore) DeleteExpired(ctx context.Context) (int, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM screencast_sessions WHERE expires_at < NOW()`)
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
