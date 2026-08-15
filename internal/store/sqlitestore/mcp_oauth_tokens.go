//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SQLiteMCPOAuthTokenStore implements store.MCPOAuthTokenStore backed by SQLite.
type SQLiteMCPOAuthTokenStore struct {
	db     *sql.DB
	encKey string
}

func NewSQLiteMCPOAuthTokenStore(db *sql.DB, encryptionKey string) *SQLiteMCPOAuthTokenStore {
	return &SQLiteMCPOAuthTokenStore{db: db, encKey: encryptionKey}
}

func (s *SQLiteMCPOAuthTokenStore) GetOAuthToken(ctx context.Context, serverID, tenantID uuid.UUID) (*store.MCPOAuthToken, error) {
	return s.getToken(ctx, serverID, tenantID, "")
}

func (s *SQLiteMCPOAuthTokenStore) GetUserOAuthToken(ctx context.Context, serverID, tenantID uuid.UUID, userID string) (*store.MCPOAuthToken, error) {
	return s.getToken(ctx, serverID, tenantID, userID)
}

func (s *SQLiteMCPOAuthTokenStore) getToken(ctx context.Context, serverID, tenantID uuid.UUID, userID string) (*store.MCPOAuthToken, error) {
	var row store.MCPOAuthToken
	var accessEnc, refreshEnc, secretEnc sql.NullString
	var expiresAtStr, issuedAtStr sql.NullString
	var scopes, resourceURI sql.NullString
	var idStr, serverStr, tenantStr string

	var query string
	var args []any
	if userID == "" {
		query = `SELECT id, server_id, tenant_id, COALESCE(user_id,''), access_token, refresh_token,
		          token_type, COALESCE(scopes,''), expires_at, issued_at,
		          dcr_client_id, dcr_client_secret, dcr_issuer, token_endpoint,
		          COALESCE(resource_uri,''), created_at, updated_at
		         FROM mcp_oauth_tokens
		         WHERE server_id=? AND tenant_id=? AND user_id IS NULL`
		args = []any{serverID.String(), tenantID.String()}
	} else {
		query = `SELECT id, server_id, tenant_id, COALESCE(user_id,''), access_token, refresh_token,
		          token_type, COALESCE(scopes,''), expires_at, issued_at,
		          dcr_client_id, dcr_client_secret, dcr_issuer, token_endpoint,
		          COALESCE(resource_uri,''), created_at, updated_at
		         FROM mcp_oauth_tokens
		         WHERE server_id=? AND tenant_id=? AND user_id=?`
		args = []any{serverID.String(), tenantID.String(), userID}
	}

	var createdStr, updatedStr string
	err := s.db.QueryRowContext(ctx, query, args...).Scan(
		&idStr, &serverStr, &tenantStr, &row.UserID,
		&accessEnc, &refreshEnc,
		&row.TokenType, &scopes, &expiresAtStr, &issuedAtStr,
		&row.DCRClientID, &secretEnc, &row.DCRIssuer, &row.TokenEndpoint,
		&resourceURI, &createdStr, &updatedStr,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	row.ID, _ = uuid.Parse(idStr)
	row.ServerID, _ = uuid.Parse(serverStr)
	row.TenantID, _ = uuid.Parse(tenantStr)
	if scopes.Valid {
		row.Scopes = scopes.String
	}
	if resourceURI.Valid {
		row.ResourceURI = resourceURI.String
	}
	if expiresAtStr.Valid && expiresAtStr.String != "" {
		if t, err2 := time.Parse(time.RFC3339Nano, expiresAtStr.String); err2 == nil {
			row.ExpiresAt = &t
		}
	}
	if issuedAtStr.Valid && issuedAtStr.String != "" {
		if t, err2 := time.Parse(time.RFC3339Nano, issuedAtStr.String); err2 == nil {
			row.IssuedAt = &t
		}
	}
	row.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdStr)
	row.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedStr)

	if accessEnc.Valid && accessEnc.String != "" {
		if dec, err2 := crypto.Decrypt(accessEnc.String, s.encKey); err2 == nil {
			row.AccessToken = dec
		} else {
			slog.Warn("mcpoauth: decrypt access_token failed", "server_id", serverID)
		}
	}
	if refreshEnc.Valid && refreshEnc.String != "" {
		if dec, err2 := crypto.Decrypt(refreshEnc.String, s.encKey); err2 == nil {
			row.RefreshToken = dec
		}
	}
	if secretEnc.Valid && secretEnc.String != "" {
		if dec, err2 := crypto.Decrypt(secretEnc.String, s.encKey); err2 == nil {
			row.DCRClientSecret = dec
		}
	}

	return &row, nil
}

func (s *SQLiteMCPOAuthTokenStore) UpsertOAuthToken(ctx context.Context, tok *store.MCPOAuthToken) error {
	if tok.ID == uuid.Nil {
		tok.ID = uuid.New()
	}

	accessEnc, err := crypto.Encrypt(tok.AccessToken, s.encKey)
	if err != nil {
		return err
	}
	var refreshEnc sql.NullString
	if tok.RefreshToken != "" {
		enc, err2 := crypto.Encrypt(tok.RefreshToken, s.encKey)
		if err2 != nil {
			return err2
		}
		refreshEnc = sql.NullString{String: enc, Valid: true}
	}
	var secretEnc sql.NullString
	if tok.DCRClientSecret != "" {
		enc, err2 := crypto.Encrypt(tok.DCRClientSecret, s.encKey)
		if err2 != nil {
			return err2
		}
		secretEnc = sql.NullString{String: enc, Valid: true}
	}

	var userIDVal any = nil
	if tok.UserID != "" {
		userIDVal = tok.UserID
	}
	var expiresStr, issuedStr any
	if tok.ExpiresAt != nil {
		expiresStr = tok.ExpiresAt.UTC().Format(time.RFC3339Nano)
	}
	if tok.IssuedAt != nil {
		issuedStr = tok.IssuedAt.UTC().Format(time.RFC3339Nano)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)

	_, err = s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO mcp_oauth_tokens
		  (id, server_id, tenant_id, user_id, access_token, refresh_token, token_type,
		   scopes, expires_at, issued_at, dcr_client_id, dcr_client_secret, dcr_issuer,
		   token_endpoint, resource_uri, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		tok.ID.String(), tok.ServerID.String(), tok.TenantID.String(), userIDVal,
		accessEnc, refreshEnc, tok.TokenType,
		tok.Scopes, expiresStr, issuedStr,
		tok.DCRClientID, secretEnc, tok.DCRIssuer,
		tok.TokenEndpoint, tok.ResourceURI,
		now, now,
	)
	return err
}

func (s *SQLiteMCPOAuthTokenStore) DeleteOAuthToken(ctx context.Context, serverID, tenantID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM mcp_oauth_tokens WHERE server_id=? AND tenant_id=? AND user_id IS NULL`,
		serverID.String(), tenantID.String(),
	)
	return err
}

func (s *SQLiteMCPOAuthTokenStore) DeleteUserOAuthToken(ctx context.Context, serverID, tenantID uuid.UUID, userID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM mcp_oauth_tokens WHERE server_id=? AND tenant_id=? AND user_id=?`,
		serverID.String(), tenantID.String(), userID,
	)
	return err
}

func (s *SQLiteMCPOAuthTokenStore) DeleteServerOAuthTokens(ctx context.Context, serverID, tenantID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM mcp_oauth_tokens WHERE server_id=? AND tenant_id=?`,
		serverID.String(), tenantID.String(),
	)
	return err
}
