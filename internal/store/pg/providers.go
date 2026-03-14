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

	// Set created_by from context if not provided by caller
	if p.CreatedBy == "" {
		p.CreatedBy = store.UserIDFromContext(ctx)
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
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO llm_providers (id, name, display_name, provider_type, api_base, api_key, enabled, settings, created_by, updated_by, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		p.ID, p.Name, p.DisplayName, p.ProviderType, p.APIBase, apiKey, p.Enabled, settings,
		nilStr(p.CreatedBy), nilStr(p.CreatedBy), now, now,
	)
	return err
}

func (s *PGProviderStore) GetProvider(ctx context.Context, id uuid.UUID) (*store.LLMProviderData, error) {
	var p store.LLMProviderData
	var apiKey string
	var createdBy, updatedBy *string
	q := `SELECT id, name, display_name, provider_type, api_base, api_key, enabled, settings, created_by, updated_by, created_at, updated_at
		 FROM llm_providers WHERE id = $1`
	args := []any{id}
	if clause, arg, active := ownerFilter(ctx, "created_by", 2); active {
		q += " " + clause
		args = append(args, arg)
	}
	err := s.db.QueryRowContext(ctx, q, args...).Scan(&p.ID, &p.Name, &p.DisplayName, &p.ProviderType, &p.APIBase, &apiKey, &p.Enabled, &p.Settings, &createdBy, &updatedBy, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("provider not found: %s", id)
	}
	p.APIKey = s.decryptKey(apiKey, p.Name)
	p.CreatedBy = derefStr(createdBy)
	p.UpdatedBy = derefStr(updatedBy)
	return &p, nil
}

func (s *PGProviderStore) GetProviderByName(ctx context.Context, name string) (*store.LLMProviderData, error) {
	var p store.LLMProviderData
	var apiKey string
	var createdBy, updatedBy *string
	q := `SELECT id, name, display_name, provider_type, api_base, api_key, enabled, settings, created_by, updated_by, created_at, updated_at
		 FROM llm_providers WHERE name = $1`
	args := []any{name}
	if clause, arg, active := ownerFilter(ctx, "created_by", 2); active {
		q += " " + clause
		args = append(args, arg)
	}
	err := s.db.QueryRowContext(ctx, q, args...).Scan(&p.ID, &p.Name, &p.DisplayName, &p.ProviderType, &p.APIBase, &apiKey, &p.Enabled, &p.Settings, &createdBy, &updatedBy, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("provider not found: %s", name)
	}
	p.APIKey = s.decryptKey(apiKey, p.Name)
	p.CreatedBy = derefStr(createdBy)
	p.UpdatedBy = derefStr(updatedBy)
	return &p, nil
}

func (s *PGProviderStore) ListProviders(ctx context.Context) ([]store.LLMProviderData, error) {
	q := `SELECT id, name, display_name, provider_type, api_base, api_key, enabled, settings, created_by, updated_by, created_at, updated_at
		 FROM llm_providers WHERE 1=1`
	var args []any
	if clause, arg, active := ownerFilter(ctx, "created_by", 1); active {
		q += " " + clause
		args = append(args, arg)
	}
	q += " ORDER BY name"
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []store.LLMProviderData
	for rows.Next() {
		var p store.LLMProviderData
		var apiKey string
		var createdBy, updatedBy *string
		if err := rows.Scan(&p.ID, &p.Name, &p.DisplayName, &p.ProviderType, &p.APIBase, &apiKey, &p.Enabled, &p.Settings, &createdBy, &updatedBy, &p.CreatedAt, &p.UpdatedAt); err != nil {
			continue
		}
		p.APIKey = s.decryptKey(apiKey, p.Name)
		p.CreatedBy = derefStr(createdBy)
		p.UpdatedBy = derefStr(updatedBy)
		result = append(result, p)
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
	return execMapUpdate(ctx, s.db, "llm_providers", id, updates)
}

func (s *PGProviderStore) DeleteProvider(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM llm_providers WHERE id = $1", id)
	return err
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
