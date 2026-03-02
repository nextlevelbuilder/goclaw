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
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO mcp_servers (id, name, display_name, transport, command, args, url, headers, env,
		 api_key, tool_prefix, timeout_sec, settings, enabled, created_by, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`,
		srv.ID, srv.Name, nilStr(srv.DisplayName), srv.Transport, nilStr(srv.Command),
		jsonOrEmpty(srv.Args), nilStr(srv.URL), jsonOrEmpty(srv.Headers), jsonOrEmpty(srv.Env),
		nilStr(apiKey), nilStr(srv.ToolPrefix), srv.TimeoutSec,
		jsonOrEmpty(srv.Settings), srv.Enabled, srv.CreatedBy, now, now,
	)
	return err
}

func (s *PGMCPServerStore) GetServer(ctx context.Context, id uuid.UUID) (*store.MCPServerData, error) {
	return s.scanServer(s.db.QueryRowContext(ctx,
		`SELECT id, name, display_name, transport, command, args, url, headers, env,
		 api_key, tool_prefix, timeout_sec, settings, enabled, created_by, created_at, updated_at
		 FROM mcp_servers WHERE id = $1`, id))
}

func (s *PGMCPServerStore) GetServerByName(ctx context.Context, name string) (*store.MCPServerData, error) {
	return s.scanServer(s.db.QueryRowContext(ctx,
		`SELECT id, name, display_name, transport, command, args, url, headers, env,
		 api_key, tool_prefix, timeout_sec, settings, enabled, created_by, created_at, updated_at
		 FROM mcp_servers WHERE name = $1`, name))
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
	srv.Headers = derefBytes(headers)
	srv.Env = derefBytes(env)
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
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, display_name, transport, command, args, url, headers, env,
		 api_key, tool_prefix, timeout_sec, settings, enabled, created_by, created_at, updated_at
		 FROM mcp_servers ORDER BY name`)
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
		srv.Headers = derefBytes(headers)
		srv.Env = derefBytes(env)
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
	updates["updated_at"] = time.Now()
	return execMapUpdate(ctx, s.db, "mcp_servers", id, updates)
}

func (s *PGMCPServerStore) DeleteServer(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM mcp_servers WHERE id = $1", id)
	return err
}
