package pg

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

// PGProviderStore implements store.ProviderStore backed by Postgres.
type PGProviderStore struct {
	db     *sql.DB
	encKey string // AES-256 encryption key for API keys (empty = plain text)
}

func NewPGProviderStore(db *sql.DB, encryptionKey string) *PGProviderStore {
	if encryptionKey != "" {
		slog.Info("provider store: API key encryption enabled")
	} else {
		slog.Warn("provider store: API key encryption disabled (plain text storage)")
	}
	return &PGProviderStore{db: db, encKey: encryptionKey}
}

func (s *PGProviderStore) CreateProvider(ctx context.Context, p *store.LLMProviderData) error {
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
	var actualID uuid.UUID
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO llm_providers (id, name, display_name, provider_type, api_base, api_key, enabled, settings, created_at, updated_at, tenant_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		 ON CONFLICT (tenant_id, name) DO UPDATE SET
			display_name = EXCLUDED.display_name, provider_type = EXCLUDED.provider_type,
			api_base = EXCLUDED.api_base, api_key = EXCLUDED.api_key,
			enabled = EXCLUDED.enabled, settings = EXCLUDED.settings, updated_at = EXCLUDED.updated_at
		 RETURNING id`,
		p.ID, p.Name, p.DisplayName, p.ProviderType, p.APIBase, apiKey, p.Enabled, settings, now, now, tid,
	).Scan(&actualID)
	if err == nil {
		p.ID = actualID // sync in-memory ID with actual DB row
	}
	return err
}

func (s *PGProviderStore) GetProvider(ctx context.Context, id uuid.UUID) (*store.LLMProviderData, error) {
	tClause, tArgs, _, err := scopeClause(ctx, 2)
	if err != nil {
		return nil, err
	}
	var p store.LLMProviderData
	err = pkgSqlxDB.GetContext(ctx, &p,
		`SELECT id, name, display_name, provider_type, api_base, api_key, enabled, settings, created_at, updated_at, tenant_id
		 FROM llm_providers WHERE id = $1`+tClause,
		append([]any{id}, tArgs...)...,
	)
	if err != nil {
		return nil, fmt.Errorf("provider not found: %s", id)
	}
	p.APIKey = s.decryptKey(p.APIKey, p.Name)
	return &p, nil
}

func (s *PGProviderStore) GetProviderByName(ctx context.Context, name string) (*store.LLMProviderData, error) {
	tClause, tArgs, _, err := scopeClause(ctx, 2)
	if err != nil {
		return nil, err
	}
	var p store.LLMProviderData
	err = pkgSqlxDB.GetContext(ctx, &p,
		`SELECT id, name, display_name, provider_type, api_base, api_key, enabled, settings, created_at, updated_at, tenant_id
		 FROM llm_providers WHERE name = $1`+tClause,
		append([]any{name}, tArgs...)...,
	)
	if err != nil {
		return nil, fmt.Errorf("provider not found: %s", name)
	}
	p.APIKey = s.decryptKey(p.APIKey, p.Name)
	return &p, nil
}

func (s *PGProviderStore) ListProviders(ctx context.Context) ([]store.LLMProviderData, error) {
	tClause, tArgs, _, err := scopeClause(ctx, 1)
	if err != nil {
		return nil, err
	}
	var result []store.LLMProviderData
	err = pkgSqlxDB.SelectContext(ctx, &result,
		`SELECT id, name, display_name, provider_type, api_base, api_key, enabled, settings, created_at, updated_at, tenant_id
		 FROM llm_providers WHERE true`+tClause+` ORDER BY name`, tArgs...)
	if err != nil {
		return nil, err
	}
	for i := range result {
		result[i].APIKey = s.decryptKey(result[i].APIKey, result[i].Name)
	}
	return result, nil
}

// ListAllProviders returns all providers across all tenants. Server-internal only.
func (s *PGProviderStore) ListAllProviders(ctx context.Context) ([]store.LLMProviderData, error) {
	var result []store.LLMProviderData
	err := pkgSqlxDB.SelectContext(ctx, &result,
		`SELECT id, name, display_name, provider_type, api_base, api_key, enabled, settings, created_at, updated_at, tenant_id
		 FROM llm_providers WHERE true ORDER BY name`)
	if err != nil {
		return nil, err
	}
	for i := range result {
		result[i].APIKey = s.decryptKey(result[i].APIKey, result[i].Name)
	}
	return result, nil
}

func (s *PGProviderStore) UpdateProvider(ctx context.Context, id uuid.UUID, updates map[string]any) error {
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

func (s *PGProviderStore) DeleteProvider(ctx context.Context, id uuid.UUID) error {
	tClause, tArgs, _, err := scopeClause(ctx, 2)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	// Safe no-op after Commit (returns sql.ErrTxDone, ignored).
	defer tx.Rollback()

	// Defensive: disable heartbeats so the next scheduler tick after delete
	// cannot fire stale config. FK ON DELETE SET NULL clears provider_id auto.
	// Tenant-scope the UPDATE through agents to prevent cross-tenant side effects:
	// even though provider IDs are UUIDs (globally unique), an attacker who guessed
	// or leaked one could otherwise disable another tenant's heartbeats.
	// IsCrossTenant (master scope) bypasses scoping for legitimate cross-tenant admin.
	var updateQuery string
	var updateArgs []any
	if store.IsCrossTenant(ctx) {
		updateQuery = "UPDATE agent_heartbeats SET enabled = false WHERE provider_id = $1"
		updateArgs = []any{id}
	} else {
		tid := store.TenantIDFromContext(ctx)
		updateQuery = `UPDATE agent_heartbeats SET enabled = false
		               WHERE provider_id = $1
		                 AND agent_id IN (SELECT id FROM agents WHERE tenant_id = $2)`
		updateArgs = []any{id, tid}
	}
	res, err := tx.ExecContext(ctx, updateQuery, updateArgs...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		slog.Warn("heartbeat.provider_cleared",
			"provider_id", id, "heartbeats_disabled", n)
	}

	if _, err := tx.ExecContext(ctx,
		"DELETE FROM llm_providers WHERE id = $1"+tClause,
		append([]any{id}, tArgs...)...,
	); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *PGProviderStore) decryptKey(apiKey, providerName string) string {
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
