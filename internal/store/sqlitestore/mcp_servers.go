//go:build sqlite || sqliteonly

package sqlitestore

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

// SQLiteMCPServerStore implements store.MCPServerStore backed by SQLite.
type SQLiteMCPServerStore struct {
	db     *sql.DB
	encKey string
}

func NewSQLiteMCPServerStore(db *sql.DB, encryptionKey string) *SQLiteMCPServerStore {
	return &SQLiteMCPServerStore{db: db, encKey: encryptionKey}
}

func (s *SQLiteMCPServerStore) CreateServer(ctx context.Context, srv *store.MCPServerData) error {
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

	now := time.Now().UTC()
	srv.CreatedAt = now
	srv.UpdatedAt = now
	encHeaders := s.encryptJSON(jsonOrEmpty(srv.Headers))
	encEnv := s.encryptJSON(jsonOrEmpty(srv.Env))

	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		tenantID = store.MasterTenantID
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO mcp_servers (id, name, display_name, transport, command, args, url, headers, env,
		 api_key, tool_prefix, timeout_sec, settings, enabled, created_by, created_at, updated_at, tenant_id)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		srv.ID, srv.Name, nilStr(srv.DisplayName), srv.Transport, nilStr(srv.Command),
		jsonOrEmpty(srv.Args), nilStr(srv.URL), encHeaders, encEnv,
		nilStr(apiKey), nilStr(srv.ToolPrefix), srv.TimeoutSec,
		jsonOrEmpty(srv.Settings), srv.Enabled, srv.CreatedBy, now, now, tenantID,
	)
	return err
}

func (s *SQLiteMCPServerStore) GetServer(ctx context.Context, id uuid.UUID) (*store.MCPServerData, error) {
	if store.IsCrossTenant(ctx) {
		return s.scanServer(s.db.QueryRowContext(ctx,
			`SELECT id, name, display_name, transport, command, args, url, headers, env,
			 api_key, tool_prefix, timeout_sec, settings, enabled, created_by, created_at, updated_at
			 FROM mcp_servers WHERE id = ?`, id))
	}
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		return nil, sql.ErrNoRows
	}
	return s.scanServer(s.db.QueryRowContext(ctx,
		`SELECT id, name, display_name, transport, command, args, url, headers, env,
		 api_key, tool_prefix, timeout_sec, settings, enabled, created_by, created_at, updated_at
		 FROM mcp_servers WHERE id = ? AND tenant_id = ?`, id, tenantID))
}

func (s *SQLiteMCPServerStore) GetServerByName(ctx context.Context, name string) (*store.MCPServerData, error) {
	if store.IsCrossTenant(ctx) {
		return s.scanServer(s.db.QueryRowContext(ctx,
			`SELECT id, name, display_name, transport, command, args, url, headers, env,
			 api_key, tool_prefix, timeout_sec, settings, enabled, created_by, created_at, updated_at
			 FROM mcp_servers WHERE name = ?`, name))
	}
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		return nil, sql.ErrNoRows
	}
	return s.scanServer(s.db.QueryRowContext(ctx,
		`SELECT id, name, display_name, transport, command, args, url, headers, env,
		 api_key, tool_prefix, timeout_sec, settings, enabled, created_by, created_at, updated_at
		 FROM mcp_servers WHERE name = ? AND tenant_id = ?`, name, tenantID))
}

func (s *SQLiteMCPServerStore) scanServer(row *sql.Row) (*store.MCPServerData, error) {
	var srv store.MCPServerData
	var displayName, command, url, apiKey, toolPrefix *string
	var args, headers, env *[]byte
	createdAt, updatedAt := scanTimePair()
	err := row.Scan(
		&srv.ID, &srv.Name, &displayName, &srv.Transport, &command,
		&args, &url, &headers, &env,
		&apiKey, &toolPrefix, &srv.TimeoutSec,
		&srv.Settings, &srv.Enabled, &srv.CreatedBy, createdAt, updatedAt,
	)
	if err != nil {
		return nil, err
	}
	srv.CreatedAt = createdAt.Time
	srv.UpdatedAt = updatedAt.Time
	srv.DisplayName = derefStr(displayName)
	srv.Command = derefStr(command)
	srv.URL = derefStr(url)
	srv.ToolPrefix = derefStr(toolPrefix)
	srv.Args = derefBytes(args)
	srv.Headers = s.decryptJSON(derefBytes(headers))
	srv.Env = s.decryptJSON(derefBytes(env))
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

func (s *SQLiteMCPServerStore) ListServers(ctx context.Context) ([]store.MCPServerData, error) {
	query := `SELECT id, name, display_name, transport, command, args, url, headers, env,
		 api_key, tool_prefix, timeout_sec, settings, enabled, created_by, created_at, updated_at
		 FROM mcp_servers`
	var qArgs []any
	if !store.IsCrossTenant(ctx) {
		tenantID := store.TenantIDFromContext(ctx)
		if tenantID == uuid.Nil {
			return []store.MCPServerData{}, nil
		}
		query += ` WHERE tenant_id = ?`
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
		createdAt, updatedAt := scanTimePair()
		if err := rows.Scan(
			&srv.ID, &srv.Name, &displayName, &srv.Transport, &command,
			&args, &url, &headers, &env,
			&apiKey, &toolPrefix, &srv.TimeoutSec,
			&srv.Settings, &srv.Enabled, &srv.CreatedBy, createdAt, updatedAt,
		); err != nil {
			continue
		}
		srv.CreatedAt = createdAt.Time
		srv.UpdatedAt = updatedAt.Time
		srv.DisplayName = derefStr(displayName)
		srv.Command = derefStr(command)
		srv.URL = derefStr(url)
		srv.ToolPrefix = derefStr(toolPrefix)
		srv.Args = derefBytes(args)
		srv.Headers = s.decryptJSON(derefBytes(headers))
		srv.Env = s.decryptJSON(derefBytes(env))
		if apiKey != nil && *apiKey != "" && s.encKey != "" {
			if decrypted, err := crypto.Decrypt(*apiKey, s.encKey); err == nil {
				srv.APIKey = decrypted
			}
		} else {
			srv.APIKey = derefStr(apiKey)
		}
		result = append(result, srv)
	}
	return result, rows.Err()
}

func (s *SQLiteMCPServerStore) UpdateServer(ctx context.Context, id uuid.UUID, updates map[string]any) error {
	if key, ok := updates["api_key"]; ok {
		if keyStr, isStr := key.(string); isStr && keyStr != "" && s.encKey != "" {
			encrypted, err := crypto.Encrypt(keyStr, s.encKey)
			if err != nil {
				return fmt.Errorf("encrypt api key: %w", err)
			}
			updates["api_key"] = encrypted
		}
	}
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
				updates[field] = json.RawMessage(s.encryptJSON(raw))
			}
		}
	}
	updates["updated_at"] = time.Now().UTC()
	if store.IsCrossTenant(ctx) {
		return execMapUpdate(ctx, s.db, "mcp_servers", id, updates)
	}
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required for update")
	}
	return execMapUpdateWhereTenant(ctx, s.db, "mcp_servers", updates, id, tid)
}

func (s *SQLiteMCPServerStore) DeleteServer(ctx context.Context, id uuid.UUID) error {
	if store.IsCrossTenant(ctx) {
		_, err := s.db.ExecContext(ctx, "DELETE FROM mcp_servers WHERE id = ?", id)
		return err
	}
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required")
	}
	_, err := s.db.ExecContext(ctx, "DELETE FROM mcp_servers WHERE id = ? AND tenant_id = ?", id, tid)
	return err
}

// encryptJSON encrypts a JSON blob by wrapping ciphertext as a JSON string.
// Unencrypted: {"key":"val"} (JSON object). Encrypted: "aes-gcm:..." (JSON string).
func (s *SQLiteMCPServerStore) encryptJSON(data []byte) []byte {
	if s.encKey == "" || len(data) == 0 || string(data) == "{}" || string(data) == "null" {
		return data
	}
	enc, err := crypto.Encrypt(string(data), s.encKey)
	if err != nil {
		slog.Warn("mcp: failed to encrypt json", "error", err)
		return data
	}
	wrapped, _ := json.Marshal(enc)
	return wrapped
}

// decryptJSON decrypts a JSON blob if it is an encrypted JSON string.
func (s *SQLiteMCPServerStore) decryptJSON(data []byte) []byte {
	if s.encKey == "" || len(data) == 0 || data[0] != '"' {
		return data
	}
	var encStr string
	if json.Unmarshal(data, &encStr) != nil {
		return data
	}
	dec, err := crypto.Decrypt(encStr, s.encKey)
	if err != nil {
		slog.Warn("mcp: failed to decrypt json", "error", err)
		return data
	}
	return []byte(dec)
}
