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

func resolveContextChannelInstanceID(ctx context.Context, db *sql.DB, scope store.ChannelContextScope) (uuid.UUID, error) {
	if scope.ChannelInstanceID != uuid.Nil {
		return scope.ChannelInstanceID, nil
	}
	if scope.ChannelInstanceName == "" {
		return uuid.Nil, sql.ErrNoRows
	}
	var id uuid.UUID
	err := db.QueryRowContext(ctx,
		`SELECT id FROM channel_instances WHERE tenant_id = ? AND name = ?`,
		tenantIDForInsert(ctx), scope.ChannelInstanceName,
	).Scan(&id)
	return id, err
}

func validateChannelScope(scopeType, scopeKey string) error {
	switch scopeType {
	case store.ChannelScopeTypeChannel:
		return nil
	case store.ChannelScopeTypeGroup, store.ChannelScopeTypeUser, store.ChannelScopeTypeRole:
		if scopeKey == "" {
			return fmt.Errorf("scope_key required for %s scope", scopeType)
		}
		return nil
	default:
		return fmt.Errorf("invalid scope_type: %s", scopeType)
	}
}

func rawPtrOrNull(raw *json.RawMessage) any {
	if raw == nil || len(*raw) == 0 {
		return nil
	}
	return string(*raw)
}

func jsonOrEmptyObject(raw json.RawMessage) any {
	if len(raw) == 0 {
		return "{}"
	}
	return string(raw)
}

func (s *SQLiteMCPServerStore) UpsertContextGrant(ctx context.Context, g *store.MCPContextGrant) error {
	if err := validateChannelScope(g.ScopeType, g.ScopeKey); err != nil {
		return err
	}
	if err := store.ValidateUserID(g.GrantedBy); err != nil {
		return err
	}
	if g.ID == uuid.Nil {
		g.ID = store.GenNewID()
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO mcp_context_grants
		   (id, tenant_id, channel_instance_id, scope_type, scope_key, server_id, enabled,
		    tool_allow, tool_deny, config_overrides, granted_by, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT (tenant_id, channel_instance_id, scope_type, scope_key, server_id)
		 DO UPDATE SET enabled = excluded.enabled,
		   tool_allow = excluded.tool_allow,
		   tool_deny = excluded.tool_deny,
		   config_overrides = excluded.config_overrides,
		   granted_by = excluded.granted_by,
		   updated_at = excluded.updated_at`,
		g.ID, tenantIDForInsert(ctx), g.ChannelInstanceID, g.ScopeType, g.ScopeKey, g.ServerID, g.Enabled,
		jsonOrNull(g.ToolAllow), jsonOrNull(g.ToolDeny), jsonOrNull(g.ConfigOverrides),
		g.GrantedBy, now, now,
	)
	return err
}

func (s *SQLiteMCPServerStore) DeleteContextGrant(ctx context.Context, channelInstanceID uuid.UUID, scopeType, scopeKey string, serverID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM mcp_context_grants
		 WHERE tenant_id = ? AND channel_instance_id = ? AND scope_type = ? AND scope_key = ? AND server_id = ?`,
		tenantIDForInsert(ctx), channelInstanceID, scopeType, scopeKey, serverID,
	)
	return err
}

func (s *SQLiteMCPServerStore) ListContextGrants(ctx context.Context, channelInstanceID uuid.UUID, scopeType, scopeKey string) ([]store.MCPContextGrant, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, channel_instance_id, scope_type, scope_key, server_id, enabled,
		        COALESCE(tool_allow, 'null'), COALESCE(tool_deny, 'null'),
		        COALESCE(config_overrides, 'null'), granted_by, created_at, updated_at
		 FROM mcp_context_grants
		 WHERE tenant_id = ? AND channel_instance_id = ? AND scope_type = ? AND scope_key = ?
		 ORDER BY created_at`,
		tenantIDForInsert(ctx), channelInstanceID, scopeType, scopeKey,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]store.MCPContextGrant, 0)
	for rows.Next() {
		var g store.MCPContextGrant
		var createdAt, updatedAt sqliteTime
		if err := rows.Scan(&g.ID, &g.ChannelInstanceID, &g.ScopeType, &g.ScopeKey, &g.ServerID, &g.Enabled,
			&g.ToolAllow, &g.ToolDeny, &g.ConfigOverrides, &g.GrantedBy, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		g.CreatedAt = createdAt.Time
		g.UpdatedAt = updatedAt.Time
		result = append(result, g)
	}
	return result, rows.Err()
}

func (s *SQLiteMCPServerStore) ListContextGrantsForScope(ctx context.Context, scope store.ChannelContextScope) ([]store.MCPContextGrant, error) {
	id, err := resolveContextChannelInstanceID(ctx, s.db, scope)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.ListContextGrants(ctx, id, scope.ScopeType, scope.ScopeKey)
}

func (s *SQLiteMCPServerStore) SetContextCredentials(ctx context.Context, creds *store.MCPContextCredentials) error {
	if err := validateChannelScope(creds.ScopeType, creds.ScopeKey); err != nil {
		return err
	}
	if err := store.ValidateUserID(creds.CreatedBy); err != nil {
		return err
	}
	if creds.ID == uuid.Nil {
		creds.ID = store.GenNewID()
	}
	var apiKey sql.NullString
	if creds.APIKey != "" && s.encKey != "" {
		enc, err := crypto.Encrypt(creds.APIKey, s.encKey)
		if err != nil {
			return fmt.Errorf("encrypt mcp context api_key: %w", err)
		}
		apiKey = sql.NullString{String: enc, Valid: true}
	} else if creds.APIKey != "" {
		apiKey = sql.NullString{String: creds.APIKey, Valid: true}
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
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO mcp_context_credentials
		   (id, tenant_id, channel_instance_id, scope_type, scope_key, server_id,
		    api_key, headers, env, created_by, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT (tenant_id, channel_instance_id, scope_type, scope_key, server_id)
		 DO UPDATE SET api_key = excluded.api_key,
		   headers = excluded.headers,
		   env = excluded.env,
		   created_by = excluded.created_by,
		   updated_at = excluded.updated_at`,
		creds.ID, tenantIDForInsert(ctx), creds.ChannelInstanceID, creds.ScopeType, creds.ScopeKey, creds.ServerID,
		apiKey, headersEnc, envEnc, creds.CreatedBy, now, now,
	)
	return err
}

func (s *SQLiteMCPServerStore) GetContextCredentials(ctx context.Context, channelInstanceID uuid.UUID, scopeType, scopeKey string, serverID uuid.UUID) (*store.MCPContextCredentials, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, channel_instance_id, scope_type, scope_key, server_id, api_key, headers, env, created_by, created_at, updated_at
		 FROM mcp_context_credentials
		 WHERE tenant_id = ? AND channel_instance_id = ? AND scope_type = ? AND scope_key = ? AND server_id = ?`,
		tenantIDForInsert(ctx), channelInstanceID, scopeType, scopeKey, serverID,
	)
	creds, err := s.scanContextCredentials(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return creds, err
}

func (s *SQLiteMCPServerStore) GetContextCredentialsForScope(ctx context.Context, scope store.ChannelContextScope, serverID uuid.UUID) (*store.MCPContextCredentials, error) {
	id, err := resolveContextChannelInstanceID(ctx, s.db, scope)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.GetContextCredentials(ctx, id, scope.ScopeType, scope.ScopeKey, serverID)
}

func (s *SQLiteMCPServerStore) DeleteContextCredentials(ctx context.Context, channelInstanceID uuid.UUID, scopeType, scopeKey string, serverID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM mcp_context_credentials
		 WHERE tenant_id = ? AND channel_instance_id = ? AND scope_type = ? AND scope_key = ? AND server_id = ?`,
		tenantIDForInsert(ctx), channelInstanceID, scopeType, scopeKey, serverID,
	)
	return err
}

func (s *SQLiteMCPServerStore) ListContextCredentials(ctx context.Context, channelInstanceID uuid.UUID, scopeType, scopeKey string) ([]store.MCPContextCredentials, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, channel_instance_id, scope_type, scope_key, server_id, api_key, headers, env, created_by, created_at, updated_at
		 FROM mcp_context_credentials
		 WHERE tenant_id = ? AND channel_instance_id = ? AND scope_type = ? AND scope_key = ?
		 ORDER BY created_at`,
		tenantIDForInsert(ctx), channelInstanceID, scopeType, scopeKey,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]store.MCPContextCredentials, 0)
	for rows.Next() {
		creds, err := s.scanContextCredentials(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *creds)
	}
	return result, rows.Err()
}

func (s *SQLiteMCPServerStore) ListContextCredentialsForScope(ctx context.Context, scope store.ChannelContextScope) ([]store.MCPContextCredentials, error) {
	id, err := resolveContextChannelInstanceID(ctx, s.db, scope)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.ListContextCredentials(ctx, id, scope.ScopeType, scope.ScopeKey)
}

type contextCredentialScanner interface {
	Scan(dest ...any) error
}

func (s *SQLiteMCPServerStore) scanContextCredentials(row contextCredentialScanner) (*store.MCPContextCredentials, error) {
	var creds store.MCPContextCredentials
	var apiKey sql.NullString
	var headersEnc, envEnc []byte
	var createdAt, updatedAt sqliteTime
	if err := row.Scan(&creds.ID, &creds.ChannelInstanceID, &creds.ScopeType, &creds.ScopeKey, &creds.ServerID,
		&apiKey, &headersEnc, &envEnc, &creds.CreatedBy, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	creds.CreatedAt = createdAt.Time
	creds.UpdatedAt = updatedAt.Time
	if apiKey.Valid && apiKey.String != "" && s.encKey != "" {
		if dec, err := crypto.Decrypt(apiKey.String, s.encKey); err == nil {
			creds.APIKey = dec
		}
	} else if apiKey.Valid {
		creds.APIKey = apiKey.String
	}
	if len(headersEnc) > 0 {
		_ = json.Unmarshal(s.decryptJSON(headersEnc), &creds.Headers)
	}
	if len(envEnc) > 0 {
		_ = json.Unmarshal(s.decryptJSON(envEnc), &creds.Env)
	}
	return &creds, nil
}

func (s *SQLiteSecureCLIStore) UpsertContextGrant(ctx context.Context, g *store.SecureCLIContextGrant) error {
	if err := validateChannelScope(g.ScopeType, g.ScopeKey); err != nil {
		return err
	}
	if err := store.ValidateUserID(g.GrantedBy); err != nil {
		return err
	}
	if g.ID == uuid.Nil {
		g.ID = store.GenNewID()
	}
	var envBytes []byte
	if len(g.EncryptedEnv) > 0 && s.encKey != "" {
		encrypted, err := crypto.Encrypt(string(g.EncryptedEnv), s.encKey)
		if err != nil {
			return fmt.Errorf("encrypt context grant env: %w", err)
		}
		envBytes = []byte(encrypted)
	} else {
		envBytes = g.EncryptedEnv
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO secure_cli_context_grants
		   (id, tenant_id, channel_instance_id, scope_type, scope_key, binary_id,
		    deny_args, deny_verbose, timeout_seconds, tips, encrypted_env, enabled,
		    granted_by, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT (tenant_id, channel_instance_id, scope_type, scope_key, binary_id)
		 DO UPDATE SET deny_args = excluded.deny_args,
		   deny_verbose = excluded.deny_verbose,
		   timeout_seconds = excluded.timeout_seconds,
		   tips = excluded.tips,
		   encrypted_env = excluded.encrypted_env,
		   enabled = excluded.enabled,
		   granted_by = excluded.granted_by,
		   updated_at = excluded.updated_at`,
		g.ID, tenantIDForInsert(ctx), g.ChannelInstanceID, g.ScopeType, g.ScopeKey, g.BinaryID,
		rawPtrOrNull(g.DenyArgs), rawPtrOrNull(g.DenyVerbose), g.TimeoutSeconds, g.Tips, envBytes, g.Enabled,
		g.GrantedBy, now, now,
	)
	return err
}

func (s *SQLiteSecureCLIStore) DeleteContextGrant(ctx context.Context, channelInstanceID uuid.UUID, scopeType, scopeKey string, binaryID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM secure_cli_context_grants
		 WHERE tenant_id = ? AND channel_instance_id = ? AND scope_type = ? AND scope_key = ? AND binary_id = ?`,
		tenantIDForInsert(ctx), channelInstanceID, scopeType, scopeKey, binaryID,
	)
	return err
}

func (s *SQLiteSecureCLIStore) ListContextGrants(ctx context.Context, channelInstanceID uuid.UUID, scopeType, scopeKey string) ([]store.SecureCLIContextGrant, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, channel_instance_id, scope_type, scope_key, binary_id,
		        deny_args, deny_verbose, timeout_seconds, tips, encrypted_env,
		        enabled, granted_by, created_at, updated_at
		 FROM secure_cli_context_grants
		 WHERE tenant_id = ? AND channel_instance_id = ? AND scope_type = ? AND scope_key = ?
		 ORDER BY created_at`,
		tenantIDForInsert(ctx), channelInstanceID, scopeType, scopeKey,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]store.SecureCLIContextGrant, 0)
	for rows.Next() {
		g, err := s.scanSecureCLIContextGrant(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *g)
	}
	return result, rows.Err()
}

func (s *SQLiteSecureCLIStore) ListContextGrantsForScope(ctx context.Context, scope store.ChannelContextScope) ([]store.SecureCLIContextGrant, error) {
	id, err := resolveContextChannelInstanceID(ctx, s.db, scope)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.ListContextGrants(ctx, id, scope.ScopeType, scope.ScopeKey)
}

func (s *SQLiteSecureCLIStore) scanSecureCLIContextGrant(row contextCredentialScanner) (*store.SecureCLIContextGrant, error) {
	var g store.SecureCLIContextGrant
	var denyArgs, denyVerbose []byte
	var createdAt, updatedAt sqliteTime
	if err := row.Scan(&g.ID, &g.ChannelInstanceID, &g.ScopeType, &g.ScopeKey, &g.BinaryID,
		&denyArgs, &denyVerbose, &g.TimeoutSeconds, &g.Tips, &g.EncryptedEnv, &g.Enabled,
		&g.GrantedBy, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	g.CreatedAt = createdAt.Time
	g.UpdatedAt = updatedAt.Time
	if len(denyArgs) > 0 {
		raw := json.RawMessage(denyArgs)
		g.DenyArgs = &raw
	}
	if len(denyVerbose) > 0 {
		raw := json.RawMessage(denyVerbose)
		g.DenyVerbose = &raw
	}
	if len(g.EncryptedEnv) > 0 && s.encKey != "" {
		if decrypted, err := crypto.Decrypt(string(g.EncryptedEnv), s.encKey); err == nil {
			g.EncryptedEnv = []byte(decrypted)
		}
	}
	g.EnvSet = len(g.EncryptedEnv) > 0
	return &g, nil
}

func (s *SQLiteSecureCLIStore) SetContextCredentials(ctx context.Context, creds *store.SecureCLIContextCredentials) error {
	if err := validateChannelScope(creds.ScopeType, creds.ScopeKey); err != nil {
		return err
	}
	if err := store.ValidateUserID(creds.CreatedBy); err != nil {
		return err
	}
	if creds.ID == uuid.Nil {
		creds.ID = store.GenNewID()
	}
	var envBytes []byte
	if len(creds.EncryptedEnv) > 0 && s.encKey != "" {
		encrypted, err := crypto.Encrypt(string(creds.EncryptedEnv), s.encKey)
		if err != nil {
			return fmt.Errorf("encrypt context credential env: %w", err)
		}
		envBytes = []byte(encrypted)
	} else {
		envBytes = creds.EncryptedEnv
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO secure_cli_context_credentials
		   (id, tenant_id, channel_instance_id, scope_type, scope_key, binary_id,
		    encrypted_env, metadata, credential_type, host_scope, created_by, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT (tenant_id, channel_instance_id, scope_type, scope_key, binary_id)
		 DO UPDATE SET encrypted_env = excluded.encrypted_env,
		   metadata = excluded.metadata,
		   credential_type = excluded.credential_type,
		   host_scope = excluded.host_scope,
		   created_by = excluded.created_by,
		   updated_at = excluded.updated_at`,
		creds.ID, tenantIDForInsert(ctx), creds.ChannelInstanceID, creds.ScopeType, creds.ScopeKey, creds.BinaryID,
		envBytes, jsonOrEmptyObject(creds.Metadata), creds.CredentialType, creds.HostScope, creds.CreatedBy, now, now,
	)
	return err
}

func (s *SQLiteSecureCLIStore) GetContextCredentials(ctx context.Context, channelInstanceID uuid.UUID, scopeType, scopeKey string, binaryID uuid.UUID) (*store.SecureCLIContextCredentials, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, channel_instance_id, scope_type, scope_key, binary_id, encrypted_env,
		        COALESCE(metadata, '{}'), credential_type, host_scope, created_by, created_at, updated_at
		 FROM secure_cli_context_credentials
		 WHERE tenant_id = ? AND channel_instance_id = ? AND scope_type = ? AND scope_key = ? AND binary_id = ?`,
		tenantIDForInsert(ctx), channelInstanceID, scopeType, scopeKey, binaryID,
	)
	creds, err := s.scanSecureCLIContextCredentials(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return creds, err
}

func (s *SQLiteSecureCLIStore) GetContextCredentialsForScope(ctx context.Context, scope store.ChannelContextScope, binaryID uuid.UUID) (*store.SecureCLIContextCredentials, error) {
	id, err := resolveContextChannelInstanceID(ctx, s.db, scope)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.GetContextCredentials(ctx, id, scope.ScopeType, scope.ScopeKey, binaryID)
}

func (s *SQLiteSecureCLIStore) DeleteContextCredentials(ctx context.Context, channelInstanceID uuid.UUID, scopeType, scopeKey string, binaryID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM secure_cli_context_credentials
		 WHERE tenant_id = ? AND channel_instance_id = ? AND scope_type = ? AND scope_key = ? AND binary_id = ?`,
		tenantIDForInsert(ctx), channelInstanceID, scopeType, scopeKey, binaryID,
	)
	return err
}

func (s *SQLiteSecureCLIStore) ListContextCredentials(ctx context.Context, channelInstanceID uuid.UUID, scopeType, scopeKey string) ([]store.SecureCLIContextCredentials, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, channel_instance_id, scope_type, scope_key, binary_id, encrypted_env,
		        COALESCE(metadata, '{}'), credential_type, host_scope, created_by, created_at, updated_at
		 FROM secure_cli_context_credentials
		 WHERE tenant_id = ? AND channel_instance_id = ? AND scope_type = ? AND scope_key = ?
		 ORDER BY created_at`,
		tenantIDForInsert(ctx), channelInstanceID, scopeType, scopeKey,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]store.SecureCLIContextCredentials, 0)
	for rows.Next() {
		creds, err := s.scanSecureCLIContextCredentials(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *creds)
	}
	return result, rows.Err()
}

func (s *SQLiteSecureCLIStore) ListContextCredentialsForScope(ctx context.Context, scope store.ChannelContextScope) ([]store.SecureCLIContextCredentials, error) {
	id, err := resolveContextChannelInstanceID(ctx, s.db, scope)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.ListContextCredentials(ctx, id, scope.ScopeType, scope.ScopeKey)
}

func (s *SQLiteSecureCLIStore) scanSecureCLIContextCredentials(row contextCredentialScanner) (*store.SecureCLIContextCredentials, error) {
	var creds store.SecureCLIContextCredentials
	var createdAt, updatedAt sqliteTime
	if err := row.Scan(&creds.ID, &creds.ChannelInstanceID, &creds.ScopeType, &creds.ScopeKey, &creds.BinaryID,
		&creds.EncryptedEnv, &creds.Metadata, &creds.CredentialType, &creds.HostScope,
		&creds.CreatedBy, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	creds.CreatedAt = createdAt.Time
	creds.UpdatedAt = updatedAt.Time
	if len(creds.EncryptedEnv) > 0 && s.encKey != "" {
		if decrypted, err := crypto.Decrypt(string(creds.EncryptedEnv), s.encKey); err == nil {
			creds.EncryptedEnv = []byte(decrypted)
		}
	}
	return &creds, nil
}
