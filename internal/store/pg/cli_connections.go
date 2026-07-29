package pg

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

// PGCLIConnectionStore implements store.CLIConnectionStore backed by Postgres.
// Credentials use the same encrypt-on-write / decrypt-on-read pattern as
// connected_agent_credentials and config_secrets.
type PGCLIConnectionStore struct {
	db     *sql.DB
	encKey string
}

func NewPGCLIConnectionStore(db *sql.DB, encryptionKey string) *PGCLIConnectionStore {
	return &PGCLIConnectionStore{db: db, encKey: encryptionKey}
}

// cliConnSelectCols keeps every read in sync. endpoint/created_by are nullable in
// the table but plain strings on the domain struct, so they are coalesced here.
const cliConnSelectCols = `id, tenant_id, name, kind, provider, mode,
		 COALESCE(endpoint, '') AS endpoint, enabled, config,
		 COALESCE(created_by, '') AS created_by, created_at, updated_at`

// cliConnVisibility builds the visibility predicate for reads: the tenant's own
// rows PLUS global rows (tenant_id IS NULL), mirroring mcp_servers. A nil
// tenantID must NOT match every row — it sees only global rows, so the
// `tenant_id = $n` half is dropped entirely rather than bound to NULL.
// argIdx is the next free placeholder number.
func cliConnVisibility(tenantID *uuid.UUID, argIdx int) (string, []any) {
	if tenantID == nil {
		return `tenant_id IS NULL`, nil
	}
	return fmt.Sprintf(`(tenant_id = $%d OR tenant_id IS NULL)`, argIdx), []any{*tenantID}
}

func (s *PGCLIConnectionStore) ListForTenant(ctx context.Context, tenantID *uuid.UUID, enabledOnly bool) ([]store.CLIConnection, error) {
	where, qArgs := cliConnVisibility(tenantID, 1)
	q := `SELECT ` + cliConnSelectCols + ` FROM cli_connections WHERE ` + where
	if enabledOnly {
		q += ` AND enabled = TRUE`
	}
	q += ` ORDER BY name`

	rows, err := s.db.QueryContext(ctx, q, qArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	conns := make([]store.CLIConnection, 0)
	for rows.Next() {
		conn, err := scanCLIConnection(rows)
		if err != nil {
			return nil, err
		}
		conns = append(conns, *conn)
	}
	return conns, rows.Err()
}

func (s *PGCLIConnectionStore) GetByID(ctx context.Context, tenantID *uuid.UUID, id uuid.UUID) (*store.CLIConnection, error) {
	where, tArgs := cliConnVisibility(tenantID, 2)
	qArgs := append([]any{id}, tArgs...)

	conn, err := scanCLIConnection(s.db.QueryRowContext(ctx,
		`SELECT `+cliConnSelectCols+` FROM cli_connections WHERE id = $1 AND `+where, qArgs...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func (s *PGCLIConnectionStore) Upsert(ctx context.Context, conn *store.CLIConnection) error {
	now := time.Now()
	config := jsonOrEmpty(conn.Config)

	if conn.ID == uuid.Nil {
		conn.ID = store.GenNewID()
		conn.CreatedAt = now
		conn.UpdatedAt = now
		_, err := s.db.ExecContext(ctx,
			`INSERT INTO cli_connections
			   (id, tenant_id, name, kind, provider, mode, endpoint, enabled, config, created_by, created_at, updated_at)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$11)`,
			conn.ID, conn.TenantID, conn.Name, conn.Kind, conn.Provider, conn.Mode,
			nilStr(conn.Endpoint), conn.Enabled, config, nilStr(conn.CreatedBy), now,
		)
		return err
	}

	// Update is scoped to the exact owner: a tenant may read global connections
	// but only a platform-scope caller (tenantID nil) may mutate them.
	conn.UpdatedAt = now
	res, err := s.db.ExecContext(ctx,
		`UPDATE cli_connections
		    SET name = $3, kind = $4, provider = $5, mode = $6, endpoint = $7,
		        enabled = $8, config = $9, updated_at = now()
		  WHERE id = $1 AND tenant_id IS NOT DISTINCT FROM $2::uuid`,
		conn.ID, conn.TenantID, conn.Name, conn.Kind, conn.Provider, conn.Mode,
		nilStr(conn.Endpoint), conn.Enabled, config,
	)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return fmt.Errorf("cli connection not found: %w", sql.ErrNoRows)
	}
	return nil
}

func (s *PGCLIConnectionStore) Delete(ctx context.Context, tenantID *uuid.UUID, id uuid.UUID) error {
	// Exact-owner match (not the read visibility clause) so a tenant cannot delete
	// a global connection. Credentials go with it via ON DELETE CASCADE.
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM cli_connections WHERE id = $1 AND tenant_id IS NOT DISTINCT FROM $2::uuid`,
		id, tenantID)
	return err
}

// --- Credentials ---

func (s *PGCLIConnectionStore) PutCredential(ctx context.Context, cred store.CLIConnectionCredential) error {
	stored := cred.Secret
	if s.encKey != "" {
		enc, err := crypto.Encrypt(cred.Secret, s.encKey)
		if err != nil {
			return fmt.Errorf("encrypt cli-connection credential: %w", err)
		}
		stored = enc
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO cli_connection_credentials
		   (connection_id, user_id, tenant_id, cred_type, inject, secret_enc, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
		 ON CONFLICT (connection_id, user_id)
		 DO UPDATE SET tenant_id = $3, cred_type = $4, inject = $5, secret_enc = $6, updated_at = $7`,
		cred.ConnectionID, cred.UserID, cred.TenantID, cred.Type, cred.Inject, stored, time.Now(),
	)
	return err
}

func (s *PGCLIConnectionStore) GetCredential(ctx context.Context, connectionID uuid.UUID, userID string) (*store.CLIConnectionCredential, error) {
	// One round trip for both candidates: the user's own row and the tenant-shared
	// row (user_id = ''). ORDER BY puts the exact user first so LIMIT 1 picks it
	// when present and falls back to the shared row otherwise.
	cred := &store.CLIConnectionCredential{ConnectionID: connectionID}
	err := s.db.QueryRowContext(ctx,
		`SELECT user_id, tenant_id, cred_type, inject, secret_enc, updated_at
		   FROM cli_connection_credentials
		  WHERE connection_id = $1 AND user_id IN ($2, $3)
		  ORDER BY (user_id = $2) DESC
		  LIMIT 1`,
		connectionID, userID, store.SharedCredentialUserID,
	).Scan(&cred.UserID, &cred.TenantID, &cred.Type, &cred.Inject, &cred.Secret, &cred.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if cred.Secret != "" && s.encKey != "" {
		dec, err := crypto.Decrypt(cred.Secret, s.encKey)
		if err != nil {
			return nil, fmt.Errorf("decrypt cli-connection credential: %w", err)
		}
		cred.Secret = dec
	}
	return cred, nil
}

func (s *PGCLIConnectionStore) DeleteCredential(ctx context.Context, connectionID uuid.UUID, userID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM cli_connection_credentials WHERE connection_id = $1 AND user_id = $2`,
		connectionID, userID)
	return err
}

// --- scan helpers ---

// cliConnScanner is satisfied by both *sql.Row and *sql.Rows.
type cliConnScanner interface {
	Scan(dest ...any) error
}

func scanCLIConnection(sc cliConnScanner) (*store.CLIConnection, error) {
	var conn store.CLIConnection
	var config []byte
	if err := sc.Scan(
		&conn.ID, &conn.TenantID, &conn.Name, &conn.Kind, &conn.Provider, &conn.Mode,
		&conn.Endpoint, &conn.Enabled, &config, &conn.CreatedBy, &conn.CreatedAt, &conn.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if len(config) > 0 {
		conn.Config = json.RawMessage(config)
	}
	return &conn, nil
}
