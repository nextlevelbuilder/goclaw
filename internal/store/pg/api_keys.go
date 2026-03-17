package pg

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// PGAPIKeyStore implements store.APIKeyStore using PostgreSQL.
type PGAPIKeyStore struct {
	db *sql.DB
}

// NewPGAPIKeyStore creates a new PostgreSQL-backed API key store.
func NewPGAPIKeyStore(db *sql.DB) *PGAPIKeyStore {
	return &PGAPIKeyStore{db: db}
}

func (s *PGAPIKeyStore) Create(ctx context.Context, key *store.APIKeyData) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO api_keys (id, name, prefix, key_hash, scopes, expires_at, created_by, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		key.ID, key.Name, key.Prefix, key.KeyHash, pq.Array(key.Scopes),
		key.ExpiresAt, nilStr(key.CreatedBy), key.CreatedAt, key.UpdatedAt,
	)
	return err
}

func (s *PGAPIKeyStore) GetByHash(ctx context.Context, keyHash string) (*store.APIKeyData, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, prefix, key_hash, scopes, expires_at, last_used_at, revoked, created_by, created_at, updated_at
		 FROM api_keys
		 WHERE key_hash = $1 AND NOT revoked AND (expires_at IS NULL OR expires_at > now())`,
		keyHash,
	)

	var k store.APIKeyData
	var createdBy *string
	err := row.Scan(
		&k.ID, &k.Name, &k.Prefix, &k.KeyHash, pq.Array(&k.Scopes),
		&k.ExpiresAt, &k.LastUsedAt, &k.Revoked, &createdBy,
		&k.CreatedAt, &k.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if createdBy != nil {
		k.CreatedBy = *createdBy
	}

	return &k, nil
}

func (s *PGAPIKeyStore) List(ctx context.Context) ([]store.APIKeyData, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, prefix, scopes, expires_at, last_used_at, revoked, created_by, created_at, updated_at
		 FROM api_keys
		 ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []store.APIKeyData
	for rows.Next() {
		var k store.APIKeyData
		var createdBy *string
		if err := rows.Scan(
			&k.ID, &k.Name, &k.Prefix, pq.Array(&k.Scopes),
			&k.ExpiresAt, &k.LastUsedAt, &k.Revoked, &createdBy,
			&k.CreatedAt, &k.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if createdBy != nil {
			k.CreatedBy = *createdBy
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

func (s *PGAPIKeyStore) Revoke(ctx context.Context, id uuid.UUID) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE api_keys SET revoked = true, updated_at = $2 WHERE id = $1`,
		id, time.Now(),
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

func (s *PGAPIKeyStore) Delete(ctx context.Context, id uuid.UUID) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM api_keys WHERE id = $1`, id,
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

func (s *PGAPIKeyStore) TouchLastUsed(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE api_keys SET last_used_at = $2 WHERE id = $1`,
		id, time.Now(),
	)
	return err
}
