package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// PGMCPServerStore implements store.MCPServerStore backed by Postgres.
type PGMCPServerStore struct {
	db     *sql.DB
	encKey string // AES-256 encryption key for API keys
}

func NewPGMCPServerStore(db *sql.DB, encryptionKey string) *PGMCPServerStore {
	return &PGMCPServerStore{db: db, encKey: encryptionKey}
}

// --- Server CRUD ---

func (s *PGMCPServerStore) CreateServer(ctx context.Context, srv *store.MCPServerData) error {
	if err := store.ValidateUserID(srv.CreatedBy); err != nil {
		return err
	}
	if srv.ID == uuid.Nil {
		srv.ID = store.GenNewID()
	}

	apiKey := srv.APIKey
	if s.encKey != "" && apiKey != "" {
		encrypted, err := crypto.Encrypt(apiKey, s.encKey)
		if err != nil {
			return fmt.Errorf("encrypt api key: %w", err)
		}
		apiKey = encrypted
	}

	now := time.Now()
	srv.CreatedAt = now
	srv.UpdatedAt = now
	encHeaders := s.encryptJSONB(jsonOrEmpty(srv.Headers))
	encEnv := s.encryptJSONB(jsonOrEmpty(srv.Env))

	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		tenantID = store.MasterTenantID
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO mcp_servers (id, name, display_name, transport, command, args, url, headers, env,
		 api_key, tool_prefix, timeout_sec, settings, enabled, created_by, created_at, updated_at, tenant_id)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)`,
		srv.ID, srv.Name, nilStr(srv.DisplayName), srv.Transport, nilStr(srv.Command),
		jsonOrEmpty(srv.Args), nilStr(srv.URL), encHeaders, encEnv,
		nilStr(apiKey), nilStr(srv.ToolPrefix), srv.TimeoutSec,
		jsonOrEmpty(srv.Settings), srv.Enabled, srv.CreatedBy, now, now, tenantID,
	)
	return err
}

func (s *PGMCPServerStore) GetServer(ctx context.Context, id uuid.UUID) (*store.MCPServerData, error) {
	if store.IsCrossTenant(ctx) {
		return s.scanServer(s.db.QueryRowContext(ctx,
			`SELECT id, name, display_name, transport, command, args, url, headers, env,
			 api_key, tool_prefix, timeout_sec, settings, enabled, created_by, created_at, updated_at
			 FROM mcp_servers WHERE id = $1`, id))
	}
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		return nil, sql.ErrNoRows
	}
	return s.scanServer(s.db.QueryRowContext(ctx,
		`SELECT id, name, display_name, transport, command, args, url, headers, env,
		 api_key, tool_prefix, timeout_sec, settings, enabled, created_by, created_at, updated_at
		 FROM mcp_servers WHERE id = $1 AND tenant_id = $2`, id, tenantID))
}

func (s *PGMCPServerStore) GetServerByName(ctx context.Context, name string) (*store.MCPServerData, error) {
	if store.IsCrossTenant(ctx) {
		return s.scanServer(s.db.QueryRowContext(ctx,
			`SELECT id, name, display_name, transport, command, args, url, headers, env,
			 api_key, tool_prefix, timeout_sec, settings, enabled, created_by, created_at, updated_at
			 FROM mcp_servers WHERE name = $1`, name))
	}
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		return nil, sql.ErrNoRows
	}
	return s.scanServer(s.db.QueryRowContext(ctx,
		`SELECT id, name, display_name, transport, command, args, url, headers, env,
		 api_key, tool_prefix, timeout_sec, settings, enabled, created_by, created_at, updated_at
		 FROM mcp_servers WHERE name = $1 AND tenant_id = $2`, name, tenantID))
}

func (s *PGMCPServerStore) scanServer(row *sql.Row) (*store.MCPServerData, error) {
	var srv store.MCPServerData
	var displayName, command, url, apiKey, toolPrefix *string
	var args, headers, env *[]byte
	err := row.Scan(
		&srv.ID, &srv.Name, &displayName, &srv.Transport, &command,
		&args, &url, &headers, &env,
		&apiKey, &toolPrefix, &srv.TimeoutSec,
		&srv.Settings, &srv.Enabled, &srv.CreatedBy, &srv.CreatedAt, &srv.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	srv.DisplayName = derefStr(displayName)
	srv.Command = derefStr(command)
	srv.URL = derefStr(url)
	srv.ToolPrefix = derefStr(toolPrefix)
	srv.Args = derefBytes(args)
	srv.Headers = s.decryptJSONB(derefBytes(headers))
	srv.Env = s.decryptJSONB(derefBytes(env))
	if apiKey != nil && *apiKey != "" && s.encKey != "" {
		decrypted, err := crypto.Decrypt(*apiKey, s.encKey)
		if err != nil {
			slog.Warn("mcp: failed to decrypt api key", "server", srv.Name, "error", err)
		} else {
			srv.APIKey = decrypted
		}
	} else {
		srv.APIKey = derefStr(apiKey)
	}
	return &srv, nil
}

func (s *PGMCPServerStore) ListServers(ctx context.Context) ([]store.MCPServerData, error) {
	query := `SELECT id, name, display_name, transport, command, args, url, headers, env,
		 api_key, tool_prefix, timeout_sec, settings, enabled, created_by, created_at, updated_at
		 FROM mcp_servers`
	var qArgs []any
	if !store.IsCrossTenant(ctx) {
		tenantID := store.TenantIDFromContext(ctx)
		if tenantID == uuid.Nil {
			return []store.MCPServerData{}, nil
		}
		query += ` WHERE tenant_id = $1`
		qArgs = append(qArgs, tenantID)
	}
	query += ` ORDER BY name`
	rows, err := s.db.QueryContext(ctx, query, qArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]store.MCPServerData, 0)
	for rows.Next() {
		var srv store.MCPServerData
		var displayName, command, url, apiKey, toolPrefix *string
		var args, headers, env *[]byte
		if err := rows.Scan(
			&srv.ID, &srv.Name, &displayName, &srv.Transport, &command,
			&args, &url, &headers, &env,
			&apiKey, &toolPrefix, &srv.TimeoutSec,
			&srv.Settings, &srv.Enabled, &srv.CreatedBy, &srv.CreatedAt, &srv.UpdatedAt,
		); err != nil {
			continue
		}
		srv.DisplayName = derefStr(displayName)
		srv.Command = derefStr(command)
		srv.URL = derefStr(url)
		srv.ToolPrefix = derefStr(toolPrefix)
		srv.Args = derefBytes(args)
		srv.Headers = s.decryptJSONB(derefBytes(headers))
		srv.Env = s.decryptJSONB(derefBytes(env))
		if apiKey != nil && *apiKey != "" && s.encKey != "" {
			if decrypted, err := crypto.Decrypt(*apiKey, s.encKey); err == nil {
				srv.APIKey = decrypted
			}
		} else {
			srv.APIKey = derefStr(apiKey)
		}
		result = append(result, srv)
	}
	return result, nil
}

func (s *PGMCPServerStore) UpdateServer(ctx context.Context, id uuid.UUID, updates map[string]any) error {
	// Encrypt api_key if present
	if key, ok := updates["api_key"]; ok {
		if keyStr, isStr := key.(string); isStr && keyStr != "" && s.encKey != "" {
			encrypted, err := crypto.Encrypt(keyStr, s.encKey)
			if err != nil {
				return fmt.Errorf("encrypt api key: %w", err)
			}
			updates["api_key"] = encrypted
		}
	}
	// Encrypt env/headers JSONB fields.
	// json.Decoder into map[string]interface{} produces map[string]interface{}
	// for nested objects, not json.RawMessage — so we must marshal any type.
	for _, field := range []string{"env", "headers"} {
		if v, ok := updates[field]; ok {
			var raw []byte
			switch val := v.(type) {
			case json.RawMessage:
				raw = []byte(val)
			default:
				raw, _ = json.Marshal(val)
			}
			if len(raw) > 0 {
				updates[field] = json.RawMessage(s.encryptJSONB(raw))
			}
		}
	}
	updates["updated_at"] = time.Now()
	if store.IsCrossTenant(ctx) {
		return execMapUpdate(ctx, s.db, "mcp_servers", id, updates)
	}
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required for update")
	}
	return execMapUpdateWhereTenant(ctx, s.db, "mcp_servers", updates, id, tid)
}

func (s *PGMCPServerStore) DeleteServer(ctx context.Context, id uuid.UUID) error {
	if store.IsCrossTenant(ctx) {
		_, err := s.db.ExecContext(ctx, "DELETE FROM mcp_servers WHERE id = $1", id)
		return err
	}
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required")
	}
	_, err := s.db.ExecContext(ctx, "DELETE FROM mcp_servers WHERE id = $1 AND tenant_id = $2", id, tid)
	return err
}

// encryptJSONB encrypts a JSONB blob (env, headers) by converting it to a JSON string literal.
// Unencrypted: {"key":"val"} (JSONB object). Encrypted: "aes-gcm:..." (JSONB string).
func (s *PGMCPServerStore) encryptJSONB(data []byte) []byte {
	if s.encKey == "" || len(data) == 0 || string(data) == "{}" || string(data) == "null" {
		return data
	}
	enc, err := crypto.Encrypt(string(data), s.encKey)
	if err != nil {
		slog.Warn("mcp: failed to encrypt jsonb", "error", err)
		return data
	}
	// Wrap as JSON string so it's valid JSONB
	wrapped, _ := json.Marshal(enc)
	return wrapped
}

// decryptJSONB decrypts a JSONB blob if it's an encrypted JSON string.
// Returns the original bytes if unencrypted (JSON object) or on error.
func (s *PGMCPServerStore) decryptJSONB(data []byte) []byte {
	if s.encKey == "" || len(data) == 0 || data[0] != '"' {
		return data // not a JSON string → unencrypted JSONB object
	}
	var encStr string
	if json.Unmarshal(data, &encStr) != nil {
		return data
	}
	dec, err := crypto.Decrypt(encStr, s.encKey)
	if err != nil {
		slog.Warn("mcp: failed to decrypt jsonb", "error", err)
		return data
	}
	return []byte(dec)
}
