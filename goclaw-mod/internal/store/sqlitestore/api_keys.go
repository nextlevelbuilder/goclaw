//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SQLiteAPIKeyStore implements store.APIKeyStore backed by SQLite.
type SQLiteAPIKeyStore struct {
	db *sql.DB
}

// NewSQLiteAPIKeyStore creates a new SQLite-backed API key store.
func NewSQLiteAPIKeyStore(db *sql.DB) *SQLiteAPIKeyStore {
	return &SQLiteAPIKeyStore{db: db}
}

func (s *SQLiteAPIKeyStore) Create(ctx context.Context, key *store.APIKeyData) error {
	var ownerID *string
	if key.OwnerID != "" {
		ownerID = &key.OwnerID
	}
	var tenantID *uuid.UUID
	if key.TenantID != uuid.Nil {
		tenantID = &key.TenantID
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO api_keys (id, name, prefix, key_hash, scopes, owner_id, tenant_id, expires_at, created_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		key.ID, key.Name, key.Prefix, key.KeyHash, jsonStringArray(key.Scopes),
		ownerID, tenantID, key.ExpiresAt, nilStr(key.CreatedBy), key.CreatedAt, key.UpdatedAt,
	)
	return err
}

// Get fetches a key by ID without revoked/expired filtering. No tenant scoping
// at store layer — callers must enforce their own ownership rules.
func (s *SQLiteAPIKeyStore) Get(ctx context.Context, id uuid.UUID) (*store.APIKeyData, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, prefix, key_hash, scopes, owner_id, tenant_id, expires_at, last_used_at, revoked, created_by, created_at, updated_at
		 FROM api_keys
		 WHERE id = ?`,
		id,
	)

	var k store.APIKeyData
	var createdBy *string
	var ownerID *string
	var tenantID *uuid.UUID
	var scopesRaw []byte
	var expiresAt, lastUsedAt nullSqliteTime
	createdAt, updatedAt := scanTimePair()
	err := row.Scan(
		&k.ID, &k.Name, &k.Prefix, &k.KeyHash, &scopesRaw,
		&ownerID, &tenantID, &expiresAt, &lastUsedAt, &k.Revoked, &createdBy,
		createdAt, updatedAt,
	)
	if err != nil {
		return nil, err
	}
	k.CreatedAt = createdAt.Time
	k.UpdatedAt = updatedAt.Time
	if expiresAt.Valid {
		k.ExpiresAt = &expiresAt.Time
	}
	if lastUsedAt.Valid {
		k.LastUsedAt = &lastUsedAt.Time
	}
	scanJSONStringArray(scopesRaw, &k.Scopes)
	if createdBy != nil {
		k.CreatedBy = *createdBy
	}
	if ownerID != nil {
		k.OwnerID = *ownerID
	}
	if tenantID != nil {
		k.TenantID = *tenantID
	}
	return &k, nil
}

func (s *SQLiteAPIKeyStore) GetByHash(ctx context.Context, keyHash string) (*store.APIKeyData, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, prefix, key_hash, scopes, owner_id, tenant_id, expires_at, last_used_at, revoked, created_by, created_at, updated_at
		 FROM api_keys
		 WHERE key_hash = ? AND NOT revoked AND (expires_at IS NULL OR expires_at > datetime('now'))`,
		keyHash,
	)

	var k store.APIKeyData
	var createdBy *string
	var ownerID *string
	var tenantID *uuid.UUID
	var scopesRaw []byte
	var expiresAt, lastUsedAt nullSqliteTime
	createdAt, updatedAt := scanTimePair()
	err := row.Scan(
		&k.ID, &k.Name, &k.Prefix, &k.KeyHash, &scopesRaw,
		&ownerID, &tenantID, &expiresAt, &lastUsedAt, &k.Revoked, &createdBy,
		createdAt, updatedAt,
	)
	if err != nil {
		return nil, err
	}
	k.CreatedAt = createdAt.Time
	k.UpdatedAt = updatedAt.Time
	if expiresAt.Valid {
		k.ExpiresAt = &expiresAt.Time
	}
	if lastUsedAt.Valid {
		k.LastUsedAt = &lastUsedAt.Time
	}
	scanJSONStringArray(scopesRaw, &k.Scopes)
	if createdBy != nil {
		k.CreatedBy = *createdBy
	}
	if ownerID != nil {
		k.OwnerID = *ownerID
	}
	if tenantID != nil {
		k.TenantID = *tenantID
	}
	return &k, nil
}

func (s *SQLiteAPIKeyStore) List(ctx context.Context, ownerID string) ([]store.APIKeyData, error) {
	var conditions []string
	var args []any

	if ownerID != "" {
		conditions = append(conditions, "owner_id = ?")
		args = append(args, ownerID)
	}

	// Tenant filter: include tenant-scoped keys + system keys (NULL tenant_id).
	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid != uuid.Nil {
			conditions = append(conditions, "(tenant_id = ? OR tenant_id IS NULL)")
			args = append(args, tid)
		}
	}

	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, prefix, scopes, owner_id, tenant_id, expires_at, last_used_at, revoked, created_by, created_at, updated_at
		 FROM api_keys`+where+`
		 ORDER BY created_at DESC`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []store.APIKeyData
	for rows.Next() {
		var k store.APIKeyData
		var createdBy *string
		var oID *string
		var tID *uuid.UUID
		var scopesRaw []byte
		var expiresAt, lastUsedAt nullSqliteTime
		createdAt, updatedAt := scanTimePair()
		if err := rows.Scan(
			&k.ID, &k.Name, &k.Prefix, &scopesRaw,
			&oID, &tID, &expiresAt, &lastUsedAt, &k.Revoked, &createdBy,
			createdAt, updatedAt,
		); err != nil {
			return nil, err
		}
		k.CreatedAt = createdAt.Time
		k.UpdatedAt = updatedAt.Time
		if expiresAt.Valid {
			k.ExpiresAt = &expiresAt.Time
		}
		if lastUsedAt.Valid {
			k.LastUsedAt = &lastUsedAt.Time
		}
		scanJSONStringArray(scopesRaw, &k.Scopes)
		if createdBy != nil {
			k.CreatedBy = *createdBy
		}
		if oID != nil {
			k.OwnerID = *oID
		}
		if tID != nil {
			k.TenantID = *tID
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

func (s *SQLiteAPIKeyStore) Revoke(ctx context.Context, id uuid.UUID, ownerID string) error {
	q := "UPDATE api_keys SET revoked = 1, updated_at = ? WHERE id = ?"
	args := []any{time.Now(), id}

	if ownerID != "" {
		q += " AND owner_id = ?"
		args = append(args, ownerID)
	}
	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid != uuid.Nil {
			q += " AND (tenant_id = ? OR tenant_id IS NULL)"
			args = append(args, tid)
		}
	}

	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *SQLiteAPIKeyStore) TouchLastUsed(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE api_keys SET last_used_at = ? WHERE id = ?`,
		time.Now(), id,
	)
	return err
}

// ensure interface is satisfied at compile time
var _ store.APIKeyStore = (*SQLiteAPIKeyStore)(nil)
