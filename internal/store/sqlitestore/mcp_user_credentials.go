//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// GetUserCredentials returns per-user credential overrides for an MCP server.
// Returns (nil, nil) if no per-user credentials exist.
func (s *SQLiteMCPServerStore) GetUserCredentials(ctx context.Context, serverID uuid.UUID, userID string) (*store.MCPUserCredentials, error) {
	tid := tenantIDForInsert(ctx)
	var apiKey sql.NullString
	var headersEnc, envEnc []byte

	err := s.db.QueryRowContext(ctx,
		`SELECT api_key, headers, env FROM mcp_user_credentials
		 WHERE server_id = ? AND user_id = ? AND tenant_id = ?`,
		serverID, userID, tid,
	).Scan(&apiKey, &headersEnc, &envEnc)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	creds := &store.MCPUserCredentials{}
	if apiKey.Valid && apiKey.String != "" && s.encKey != "" {
		if dec, err := crypto.Decrypt(apiKey.String, s.encKey); err == nil {
			creds.APIKey = dec
		}
	} else if apiKey.Valid {
		creds.APIKey = apiKey.String
	}
	if len(headersEnc) > 0 {
		dec := s.decryptJSON(headersEnc)
		json.Unmarshal(dec, &creds.Headers)
	}
	if len(envEnc) > 0 {
		dec := s.decryptJSON(envEnc)
		json.Unmarshal(dec, &creds.Env)
	}

	return creds, nil
}

// SetUserCredentials creates or updates per-user MCP credentials.
func (s *SQLiteMCPServerStore) SetUserCredentials(ctx context.Context, serverID uuid.UUID, userID string, creds store.MCPUserCredentials) error {
	tid := tenantIDForInsert(ctx)

	var apiKeyEnc sql.NullString
	if creds.APIKey != "" && s.encKey != "" {
		enc, err := crypto.Encrypt(creds.APIKey, s.encKey)
		if err != nil {
			return fmt.Errorf("encrypt mcp user api_key: %w", err)
		}
		apiKeyEnc = sql.NullString{String: enc, Valid: true}
	} else if creds.APIKey != "" {
		apiKeyEnc = sql.NullString{String: creds.APIKey, Valid: true}
	}

	var headersEnc, envEnc []byte
	if len(creds.Headers) > 0 {
		raw, _ := json.Marshal(creds.Headers)
		headersEnc = s.encryptJSON(raw)
	}
	if len(creds.Env) > 0 {
		raw, _ := json.Marshal(creds.Env)
		envEnc = s.encryptJSON(raw)
	}

	now := time.Now().UTC()
	id := store.GenNewID()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO mcp_user_credentials (id, server_id, user_id, api_key, headers, env, tenant_id, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (server_id, user_id, tenant_id) DO UPDATE SET
		   api_key = excluded.api_key, headers = excluded.headers, env = excluded.env, updated_at = excluded.updated_at`,
		id, serverID, userID, apiKeyEnc, headersEnc, envEnc, tid, now, now,
	)
	return err
}

// DeleteUserCredentials removes per-user MCP credentials.
func (s *SQLiteMCPServerStore) DeleteUserCredentials(ctx context.Context, serverID uuid.UUID, userID string) error {
	tid := tenantIDForInsert(ctx)
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM mcp_user_credentials WHERE server_id = ? AND user_id = ? AND tenant_id = ?`,
		serverID, userID, tid,
	)
	return err
}
