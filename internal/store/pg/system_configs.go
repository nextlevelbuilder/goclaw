package pg

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// PGSystemConfigStore implements store.SystemConfigStore backed by Postgres.
// Strict tenant isolation — all operations require tenant_id in context (no fallback).
type PGSystemConfigStore struct {
	db *sql.DB
}

func NewPGSystemConfigStore(db *sql.DB) *PGSystemConfigStore {
	return &PGSystemConfigStore{db: db}
}

// resolveTenantID returns the tenant ID from context.
// Returns uuid.Nil if no tenant in context — callers must handle this explicitly.
func resolveTenantID(ctx context.Context) uuid.UUID {
	return store.TenantIDFromContext(ctx)
}

func (s *PGSystemConfigStore) Get(ctx context.Context, key string) (string, error) {
	tid := resolveTenantID(ctx)
	if tid == uuid.Nil {
		return "", fmt.Errorf("system config get: tenant_id required")
	}

	var val string
	err := s.db.QueryRowContext(ctx,
		"SELECT value FROM system_configs WHERE key = $1 AND tenant_id = $2",
		key, tid,
	).Scan(&val)
	if err == nil {
		return val, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("system config get: %w", err)
	}

	return "", fmt.Errorf("system config not found: %s", key)
}

func (s *PGSystemConfigStore) Set(ctx context.Context, key, value string) error {
	tid := resolveTenantID(ctx)
	if tid == uuid.Nil {
		slog.Warn("system_config.set: no tenant in context", "key", key)
		return fmt.Errorf("system config set: tenant_id required")
	}
	return s.upsert(ctx, key, value, tid)
}

func (s *PGSystemConfigStore) upsert(ctx context.Context, key, value string, tenantID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO system_configs (key, value, tenant_id, updated_at)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (key, tenant_id) DO UPDATE SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at`,
		key, value, tenantID, time.Now(),
	)
	return err
}

func (s *PGSystemConfigStore) Delete(ctx context.Context, key string) error {
	tid := resolveTenantID(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("system config delete: tenant_id required")
	}
	_, err := s.db.ExecContext(ctx,
		"DELETE FROM system_configs WHERE key = $1 AND tenant_id = $2",
		key, tid,
	)
	return err
}

func (s *PGSystemConfigStore) List(ctx context.Context) (map[string]string, error) {
	tid := resolveTenantID(ctx)
	if tid == uuid.Nil {
		return nil, fmt.Errorf("system config list: tenant_id required")
	}

	rows, err := s.db.QueryContext(ctx,
		"SELECT key, value FROM system_configs WHERE tenant_id = $1 ORDER BY key",
		tid,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("system config scan: %w", err)
		}
		result[k] = v
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("system config list: %w", err)
	}
	return result, nil
}
