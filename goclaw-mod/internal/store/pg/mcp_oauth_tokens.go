package pg

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

// PGMCPOAuthTokenStore implements store.MCPOAuthTokenStore backed by Postgres.
type PGMCPOAuthTokenStore struct {
	db     *sql.DB
	encKey string
}

func NewPGMCPOAuthTokenStore(db *sql.DB, encryptionKey string) *PGMCPOAuthTokenStore {
	return &PGMCPOAuthTokenStore{db: db, encKey: encryptionKey}
}

func (s *PGMCPOAuthTokenStore) GetOAuthToken(ctx context.Context, serverID, tenantID uuid.UUID) (*store.MCPOAuthToken, error) {
	return s.getToken(ctx, serverID, tenantID, "")
}

func (s *PGMCPOAuthTokenStore) GetUserOAuthToken(ctx context.Context, serverID, tenantID uuid.UUID, userID string) (*store.MCPOAuthToken, error) {
	return s.getToken(ctx, serverID, tenantID, userID)
}

func (s *PGMCPOAuthTokenStore) getToken(ctx context.Context, serverID, tenantID uuid.UUID, userID string) (*store.MCPOAuthToken, error) {
	var row store.MCPOAuthToken
	var accessEnc, refreshEnc, secretEnc sql.NullString
	var expiresAt, issuedAt sql.NullTime
	var scopes, resourceURI sql.NullString

	var query string
	var args []any
	if userID == "" {
		query = `SELECT id, server_id, tenant_id, COALESCE(user_id,''), access_token, refresh_token,
		          token_type, COALESCE(scopes,''), expires_at, issued_at,
		          dcr_client_id, dcr_client_secret, dcr_issuer, token_endpoint,
		          COALESCE(resource_uri,''), created_at, updated_at
		         FROM mcp_oauth_tokens
		         WHERE server_id=$1 AND tenant_id=$2 AND user_id IS NULL`
		args = []any{serverID, tenantID}
	} else {
		query = `SELECT id, server_id, tenant_id, COALESCE(user_id,''), access_token, refresh_token,
		          token_type, COALESCE(scopes,''), expires_at, issued_at,
		          dcr_client_id, dcr_client_secret, dcr_issuer, token_endpoint,
		          COALESCE(resource_uri,''), created_at, updated_at
		         FROM mcp_oauth_tokens
		         WHERE server_id=$1 AND tenant_id=$2 AND user_id=$3`
		args = []any{serverID, tenantID, userID}
	}

	err := s.db.QueryRowContext(ctx, query, args...).Scan(
		&row.ID, &row.ServerID, &row.TenantID, &row.UserID,
		&accessEnc, &refreshEnc,
		&row.TokenType, &scopes, &expiresAt, &issuedAt,
		&row.DCRClientID, &secretEnc, &row.DCRIssuer, &row.TokenEndpoint,
		&resourceURI, &row.CreatedAt, &row.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	if scopes.Valid {
		row.Scopes = scopes.String
	}
	if resourceURI.Valid {
		row.ResourceURI = resourceURI.String
	}
	if expiresAt.Valid {
		row.ExpiresAt = &expiresAt.Time
	}
	if issuedAt.Valid {
		row.IssuedAt = &issuedAt.Time
	}

	// Decrypt sensitive fields.
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

func (s *PGMCPOAuthTokenStore) UpsertOAuthToken(ctx context.Context, tok *store.MCPOAuthToken) error {
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

	now := time.Now()

	// PostgreSQL treats NULLs as distinct in UNIQUE constraints — ON CONFLICT on
	// (server_id, tenant_id, user_id) never fires when user_id IS NULL. Use the
	// matching partial unique index target for each case instead.
	var query string
	var args []any
	if tok.UserID == "" {
		query = `
		INSERT INTO mcp_oauth_tokens
		  (id, server_id, tenant_id, user_id, access_token, refresh_token, token_type,
		   scopes, expires_at, issued_at, dcr_client_id, dcr_client_secret, dcr_issuer,
		   token_endpoint, resource_uri, created_at, updated_at)
		VALUES ($1,$2,$3,NULL,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
		ON CONFLICT (server_id, tenant_id) WHERE user_id IS NULL DO UPDATE SET
		  access_token      = EXCLUDED.access_token,
		  refresh_token     = EXCLUDED.refresh_token,
		  token_type        = EXCLUDED.token_type,
		  scopes            = EXCLUDED.scopes,
		  expires_at        = EXCLUDED.expires_at,
		  issued_at         = EXCLUDED.issued_at,
		  dcr_client_id     = EXCLUDED.dcr_client_id,
		  dcr_client_secret = EXCLUDED.dcr_client_secret,
		  dcr_issuer        = EXCLUDED.dcr_issuer,
		  token_endpoint    = EXCLUDED.token_endpoint,
		  resource_uri      = EXCLUDED.resource_uri,
		  updated_at        = EXCLUDED.updated_at`
		args = []any{
			tok.ID, tok.ServerID, tok.TenantID,
			accessEnc, refreshEnc, tok.TokenType,
			tok.Scopes, tok.ExpiresAt, tok.IssuedAt,
			tok.DCRClientID, secretEnc, tok.DCRIssuer,
			tok.TokenEndpoint, tok.ResourceURI,
			now, now,
		}
	} else {
		query = `
		INSERT INTO mcp_oauth_tokens
		  (id, server_id, tenant_id, user_id, access_token, refresh_token, token_type,
		   scopes, expires_at, issued_at, dcr_client_id, dcr_client_secret, dcr_issuer,
		   token_endpoint, resource_uri, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
		ON CONFLICT (server_id, tenant_id, user_id) WHERE user_id IS NOT NULL DO UPDATE SET
		  access_token      = EXCLUDED.access_token,
		  refresh_token     = EXCLUDED.refresh_token,
		  token_type        = EXCLUDED.token_type,
		  scopes            = EXCLUDED.scopes,
		  expires_at        = EXCLUDED.expires_at,
		  issued_at         = EXCLUDED.issued_at,
		  dcr_client_id     = EXCLUDED.dcr_client_id,
		  dcr_client_secret = EXCLUDED.dcr_client_secret,
		  dcr_issuer        = EXCLUDED.dcr_issuer,
		  token_endpoint    = EXCLUDED.token_endpoint,
		  resource_uri      = EXCLUDED.resource_uri,
		  updated_at        = EXCLUDED.updated_at`
		args = []any{
			tok.ID, tok.ServerID, tok.TenantID, tok.UserID,
			accessEnc, refreshEnc, tok.TokenType,
			tok.Scopes, tok.ExpiresAt, tok.IssuedAt,
			tok.DCRClientID, secretEnc, tok.DCRIssuer,
			tok.TokenEndpoint, tok.ResourceURI,
			now, now,
		}
	}

	_, err = s.db.ExecContext(ctx, query, args...)
	return err
}

func (s *PGMCPOAuthTokenStore) DeleteOAuthToken(ctx context.Context, serverID, tenantID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM mcp_oauth_tokens WHERE server_id=$1 AND tenant_id=$2 AND user_id IS NULL`,
		serverID, tenantID,
	)
	return err
}

func (s *PGMCPOAuthTokenStore) DeleteUserOAuthToken(ctx context.Context, serverID, tenantID uuid.UUID, userID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM mcp_oauth_tokens WHERE server_id=$1 AND tenant_id=$2 AND user_id=$3`,
		serverID, tenantID, userID,
	)
	return err
}

func (s *PGMCPOAuthTokenStore) DeleteServerOAuthTokens(ctx context.Context, serverID, tenantID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM mcp_oauth_tokens WHERE server_id=$1 AND tenant_id=$2`,
		serverID, tenantID,
	)
	return err
}
