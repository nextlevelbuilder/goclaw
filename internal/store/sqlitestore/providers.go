//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SQLiteProviderStore implements store.ProviderStore backed by SQLite.
type SQLiteProviderStore struct {
	db     *sql.DB
	encKey string // AES-256 encryption key for API keys (empty = plain text)
}

func NewSQLiteProviderStore(db *sql.DB, encryptionKey string) *SQLiteProviderStore {
	if encryptionKey != "" {
		slog.Info("provider store: API key encryption enabled")
	} else {
		slog.Warn("provider store: API key encryption disabled (plain text storage)")
	}
	return &SQLiteProviderStore{db: db, encKey: encryptionKey}
}

func (s *SQLiteProviderStore) CreateProvider(ctx context.Context, p *store.LLMProviderData) error {
	if p.ID == uuid.Nil {
		p.ID = store.GenNewID()
	}

	apiKey := p.APIKey
	if s.encKey != "" && apiKey != "" {
		encrypted, err := crypto.Encrypt(apiKey, s.encKey)
		if err != nil {
			return fmt.Errorf("encrypt api key: %w", err)
		}
		apiKey = encrypted
	}

	settings := p.Settings
	if len(settings) == 0 {
		settings = []byte("{}")
	}

	now := time.Now()
	p.CreatedAt = now
	p.UpdatedAt = now
	tid := tenantIDForInsert(ctx)
	p.TenantID = tid
	// UPSERT: if provider with same (tenant_id, name) exists, update it and return its ID.
	// This handles orphaned providers left after agent deletion (#295).
	var actualID string
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO llm_providers (id, name, display_name, provider_type, api_base, api_key, enabled, settings, created_at, updated_at, tenant_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(tenant_id, name) DO UPDATE SET
			display_name = excluded.display_name, provider_type = excluded.provider_type,
			api_base = excluded.api_base, api_key = excluded.api_key,
			enabled = excluded.enabled, settings = excluded.settings, updated_at = excluded.updated_at
		 RETURNING id`,
		p.ID, p.Name, p.DisplayName, p.ProviderType, p.APIBase, apiKey, p.Enabled, settings, now, now, tid,
	).Scan(&actualID)
	if err == nil {
		if parsed, parseErr := uuid.Parse(actualID); parseErr == nil {
			p.ID = parsed // sync in-memory ID with actual DB row
		}
	}
	return err
}

func (s *SQLiteProviderStore) GetProvider(ctx context.Context, id uuid.UUID) (*store.LLMProviderData, error) {
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return nil, err
	}
	var p store.LLMProviderData
	var apiKey string
	createdAt, updatedAt := scanTimePair()
	args := append([]any{id}, tArgs...)
	err = s.db.QueryRowContext(ctx,
		`SELECT id, name, display_name, provider_type, api_base, api_key, enabled, settings, created_at, updated_at, tenant_id
		 FROM llm_providers WHERE id = ?`+tClause,
		args...,
	).Scan(&p.ID, &p.Name, &p.DisplayName, &p.ProviderType, &p.APIBase, &apiKey, &p.Enabled, &p.Settings, createdAt, updatedAt, &p.TenantID)
	if err != nil {
		return nil, fmt.Errorf("provider not found: %s", id)
	}
	p.CreatedAt = createdAt.Time
	p.UpdatedAt = updatedAt.Time
	p.APIKey = s.decryptKey(apiKey, p.Name)
	return &p, nil
}

func (s *SQLiteProviderStore) GetProviderByName(ctx context.Context, name string) (*store.LLMProviderData, error) {
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return nil, err
	}
	var p store.LLMProviderData
	var apiKey string
	createdAt, updatedAt := scanTimePair()
	args := append([]any{name}, tArgs...)
	err = s.db.QueryRowContext(ctx,
		`SELECT id, name, display_name, provider_type, api_base, api_key, enabled, settings, created_at, updated_at, tenant_id
		 FROM llm_providers WHERE name = ?`+tClause,
		args...,
	).Scan(&p.ID, &p.Name, &p.DisplayName, &p.ProviderType, &p.APIBase, &apiKey, &p.Enabled, &p.Settings, createdAt, updatedAt, &p.TenantID)
	if err != nil {
		return nil, fmt.Errorf("provider not found: %s", name)
	}
	p.CreatedAt = createdAt.Time
	p.UpdatedAt = updatedAt.Time
	p.APIKey = s.decryptKey(apiKey, p.Name)
	return &p, nil
}

func (s *SQLiteProviderStore) ListProviders(ctx context.Context) ([]store.LLMProviderData, error) {
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return nil, err
	}
	q := `SELECT id, name, display_name, provider_type, api_base, api_key, enabled, settings, created_at, updated_at, tenant_id
		 FROM llm_providers WHERE true` + tClause + ` ORDER BY name`
	rows, err := s.db.QueryContext(ctx, q, tArgs...)
	if err != nil {
		return nil, err
	}
	return s.scanProviders(rows)
}

// ListAllProviders returns all providers across all tenants. Server-internal only.
func (s *SQLiteProviderStore) ListAllProviders(ctx context.Context) ([]store.LLMProviderData, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, display_name, provider_type, api_base, api_key, enabled, settings, created_at, updated_at, tenant_id
		 FROM llm_providers ORDER BY name`)
	if err != nil {
		return nil, err
	}
	return s.scanProviders(rows)
}

func (s *SQLiteProviderStore) UpdateProvider(ctx context.Context, id uuid.UUID, updates map[string]any) error {
	if apiKey, ok := updates["api_key"]; ok && s.encKey != "" {
		if keyStr, ok := apiKey.(string); ok && keyStr != "" {
			encrypted, err := crypto.Encrypt(keyStr, s.encKey)
			if err != nil {
				return fmt.Errorf("encrypt api key: %w", err)
			}
			updates["api_key"] = encrypted
		}
	}
	if store.IsCrossTenant(ctx) {
		return execMapUpdate(ctx, s.db, "llm_providers", id, updates)
	}
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required")
	}
	return execMapUpdateWhereTenant(ctx, s.db, "llm_providers", updates, id, tid)
}

func (s *SQLiteProviderStore) DeleteProvider(ctx context.Context, id uuid.UUID) error {
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return err
	}
	args := append([]any{id}, tArgs...)
	_, err = s.db.ExecContext(ctx,
		"DELETE FROM llm_providers WHERE id = ?"+tClause,
		args...,
	)
	return err
}

func (s *SQLiteProviderStore) decryptKey(apiKey, providerName string) string {
	if s.encKey != "" && apiKey != "" {
		decrypted, err := crypto.Decrypt(apiKey, s.encKey)
		if err != nil {
			slog.Warn("failed to decrypt provider API key", "provider", providerName, "error", err)
			return apiKey
		}
		return decrypted
	}
	return apiKey
}

func (s *SQLiteProviderStore) scanProviders(rows *sql.Rows) ([]store.LLMProviderData, error) {
	defer rows.Close()
	var result []store.LLMProviderData
	for rows.Next() {
		var p store.LLMProviderData
		var apiKey string
		createdAt, updatedAt := scanTimePair()
		if err := rows.Scan(&p.ID, &p.Name, &p.DisplayName, &p.ProviderType, &p.APIBase, &apiKey, &p.Enabled, &p.Settings, createdAt, updatedAt, &p.TenantID); err != nil {
			slog.Error("providers.scan", "error", err)
			continue
		}
		p.CreatedAt = createdAt.Time
		p.UpdatedAt = updatedAt.Time
		p.APIKey = s.decryptKey(apiKey, p.Name)
		result = append(result, p)
	}
	return result, rows.Err()
}

