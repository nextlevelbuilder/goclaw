package pg

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
)

// PGConfigSecretsStore implements store.ConfigSecretsStore backed by Postgres.
type PGConfigSecretsStore struct {
	db     *sql.DB
	encKey string
}

func NewPGConfigSecretsStore(db *sql.DB, encryptionKey string) *PGConfigSecretsStore {
	return &PGConfigSecretsStore{db: db, encKey: encryptionKey}
}

func (s *PGConfigSecretsStore) Get(ctx context.Context, key string) (string, error) {
	tid := tenantIDForInsert(ctx) // fallback to master
	var value []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT value FROM config_secrets WHERE key = $1 AND tenant_id = $2`, key, tid).Scan(&value)
	if err != nil {
		return "", err
	}

	if len(value) > 0 && s.encKey != "" {
		decrypted, err := crypto.Decrypt(string(value), s.encKey)
		if err != nil {
			return "", fmt.Errorf("decrypt secret %q: %w", key, err)
		}
		return decrypted, nil
	}
	return string(value), nil
}

func (s *PGConfigSecretsStore) Set(ctx context.Context, key, value string) error {
	var stored []byte
	if s.encKey != "" {
		encrypted, err := crypto.Encrypt(value, s.encKey)
		if err != nil {
			return fmt.Errorf("encrypt secret %q: %w", key, err)
		}
		stored = []byte(encrypted)
	} else {
		stored = []byte(value)
	}

	tid := tenantIDForInsert(ctx)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO config_secrets (key, value, updated_at, tenant_id) VALUES ($1, $2, $3, $4)
		 ON CONFLICT (key, tenant_id) DO UPDATE SET value = $2, updated_at = $3`,
		key, stored, time.Now(), tid,
	)
	return err
}

func (s *PGConfigSecretsStore) Delete(ctx context.Context, key string) error {
	tid := tenantIDForInsert(ctx)
	_, err := s.db.ExecContext(ctx, `DELETE FROM config_secrets WHERE key = $1 AND tenant_id = $2`, key, tid)
	return err
}

func (s *PGConfigSecretsStore) GetAll(ctx context.Context) (map[string]string, error) {
	tid := tenantIDForInsert(ctx)
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM config_secrets WHERE tenant_id = $1`, tid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var key string
		var value []byte
		if err := rows.Scan(&key, &value); err != nil {
			continue
		}

		if len(value) > 0 && s.encKey != "" {
			decrypted, err := crypto.Decrypt(string(value), s.encKey)
			if err != nil {
				slog.Warn("config_secrets: failed to decrypt", "key", key, "error", err)
				continue
			}
			result[key] = decrypted
		} else {
			result[key] = string(value)
		}
	}
	return result, nil
}
