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

const mcpServerSelectCols = `id, name, display_name, transport, command, args, url, headers, env,
		 api_key, tool_prefix, timeout_sec, settings, enabled, created_by, created_at, updated_at`

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
	q := `SELECT ` + mcpServerSelectCols + ` FROM mcp_servers WHERE id = ?`
	qArgs := []any{id}
	if !store.IsCrossTenant(ctx) {
		tenantID := store.TenantIDFromContext(ctx)
		if tenantID == uuid.Nil {
			return nil, sql.ErrNoRows
		}
		q += ` AND tenant_id = ?`
		qArgs = append(qArgs, tenantID)
	}
	var row mcpServerRow
	if err := pkgSqlxDB.GetContext(ctx, &row, q, qArgs...); err != nil {
		return nil, err
	}
	srv := row.toMCPServerData()
	s.decryptServerFields(&srv)
	return &srv, nil
}

func (s *SQLiteMCPServerStore) GetServerByName(ctx context.Context, name string) (*store.MCPServerData, error) {
	q := `SELECT ` + mcpServerSelectCols + ` FROM mcp_servers WHERE name = ?`
	qArgs := []any{name}
	if !store.IsCrossTenant(ctx) {
		tenantID := store.TenantIDFromContext(ctx)
		if tenantID == uuid.Nil {
			return nil, sql.ErrNoRows
		}
		q += ` AND tenant_id = ?`
		qArgs = append(qArgs, tenantID)
	}
	var row mcpServerRow
	if err := pkgSqlxDB.GetContext(ctx, &row, q, qArgs...); err != nil {
		return nil, err
	}
	srv := row.toMCPServerData()
	s.decryptServerFields(&srv)
	return &srv, nil
}

// decryptServerFields decrypts api_key, headers, and env after scan.
func (s *SQLiteMCPServerStore) decryptServerFields(srv *store.MCPServerData) {
	srv.Headers = s.decryptJSON(srv.Headers)
	srv.Env = s.decryptJSON(srv.Env)
	if srv.APIKey != "" && s.encKey != "" {
		if decrypted, err := crypto.Decrypt(srv.APIKey, s.encKey); err == nil {
			srv.APIKey = decrypted
		} else {
			slog.Warn("mcp: failed to decrypt api key", "server", srv.Name, "error", err)
		}
	}
}

func (s *SQLiteMCPServerStore) ListServers(ctx context.Context) ([]store.MCPServerData, error) {
	q := `SELECT ` + mcpServerSelectCols + ` FROM mcp_servers`
	var qArgs []any
	if !store.IsCrossTenant(ctx) {
		tenantID := store.TenantIDFromContext(ctx)
		if tenantID == uuid.Nil {
			return []store.MCPServerData{}, nil
		}
		q += ` WHERE tenant_id = ?`
		qArgs = append(qArgs, tenantID)
	}
	q += ` ORDER BY name`

	var rows []mcpServerRow
	if err := pkgSqlxDB.SelectContext(ctx, &rows, q, qArgs...); err != nil {
		return nil, err
	}
	result := make([]store.MCPServerData, 0, len(rows))
	for _, r := range rows {
		srv := r.toMCPServerData()
		s.decryptServerFields(&srv)
		result = append(result, srv)
	}
	return result, nil
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
