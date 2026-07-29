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

// SQLiteCLIConnectionStore implements store.CLIConnectionStore backed by SQLite.
// Unlike connected_agent_credentials (a PG-only stub on desktop), this one is a
// real implementation — schema v28 creates both tables for SQLite too.
//
// SQLite storage notes: `enabled` is INTEGER 0/1 and timestamps are TEXT
// (RFC3339Nano), so writes format times and reads go through sqliteTime.
type SQLiteCLIConnectionStore struct {
	db     *sql.DB
	encKey string
}

func NewSQLiteCLIConnectionStore(db *sql.DB, encryptionKey string) *SQLiteCLIConnectionStore {
	return &SQLiteCLIConnectionStore{db: db, encKey: encryptionKey}
}

// cliConnSelectCols keeps every read in sync. endpoint/created_by are nullable in
// the table but plain strings on the domain struct, so they are coalesced here.
const cliConnSelectCols = `id, tenant_id, name, kind, provider, mode,
		 COALESCE(endpoint, '') AS endpoint, enabled, config,
		 COALESCE(created_by, '') AS created_by, created_at, updated_at`

// cliConnVisibility builds the visibility predicate for reads: the tenant's own
// rows PLUS global rows (tenant_id IS NULL), mirroring mcp_servers. A nil
// tenantID must NOT match every row — it sees only global rows, so the
// `tenant_id = ?` half is dropped entirely rather than bound to NULL.
func cliConnVisibility(tenantID *uuid.UUID) (string, []any) {
	if tenantID == nil {
		return `tenant_id IS NULL`, nil
	}
	return `(tenant_id = ? OR tenant_id IS NULL)`, []any{*tenantID}
}

func (s *SQLiteCLIConnectionStore) ListForTenant(ctx context.Context, tenantID *uuid.UUID, enabledOnly bool) ([]store.CLIConnection, error) {
	where, qArgs := cliConnVisibility(tenantID)
	q := `SELECT ` + cliConnSelectCols + ` FROM cli_connections WHERE ` + where
	if enabledOnly {
		q += ` AND enabled = 1`
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

func (s *SQLiteCLIConnectionStore) GetByID(ctx context.Context, tenantID *uuid.UUID, id uuid.UUID) (*store.CLIConnection, error) {
	where, tArgs := cliConnVisibility(tenantID)
	qArgs := append([]any{id}, tArgs...)

	conn, err := scanCLIConnection(s.db.QueryRowContext(ctx,
		`SELECT `+cliConnSelectCols+` FROM cli_connections WHERE id = ? AND `+where, qArgs...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func (s *SQLiteCLIConnectionStore) Upsert(ctx context.Context, conn *store.CLIConnection) error {
	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339Nano)
	config := jsonOrEmpty(conn.Config)

	if conn.ID == uuid.Nil {
		conn.ID = store.GenNewID()
		conn.CreatedAt = now
		conn.UpdatedAt = now
		_, err := s.db.ExecContext(ctx,
			`INSERT INTO cli_connections
			   (id, tenant_id, name, kind, provider, mode, endpoint, enabled, config, created_by, created_at, updated_at)
			 VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
			conn.ID, conn.TenantID, conn.Name, conn.Kind, conn.Provider, conn.Mode,
			nilStr(conn.Endpoint), conn.Enabled, config, nilStr(conn.CreatedBy), nowStr, nowStr,
		)
		return err
	}

	// Update is scoped to the exact owner: a tenant may read global connections
	// but only a platform-scope caller (tenantID nil) may mutate them. SQLite's
	// `IS` operator is NULL-safe, so one clause covers both cases.
	conn.UpdatedAt = now
	res, err := s.db.ExecContext(ctx,
		`UPDATE cli_connections
		    SET name = ?, kind = ?, provider = ?, mode = ?, endpoint = ?,
		        enabled = ?, config = ?, updated_at = ?
		  WHERE id = ? AND tenant_id IS ?`,
		conn.Name, conn.Kind, conn.Provider, conn.Mode, nilStr(conn.Endpoint),
		conn.Enabled, config, nowStr, conn.ID, conn.TenantID,
	)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return fmt.Errorf("cli connection not found: %w", sql.ErrNoRows)
	}
	return nil
}

func (s *SQLiteCLIConnectionStore) Delete(ctx context.Context, tenantID *uuid.UUID, id uuid.UUID) error {
	// Exact-owner match (not the read visibility clause) so a tenant cannot delete
	// a global connection. Credentials go with it via ON DELETE CASCADE.
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM cli_connections WHERE id = ? AND tenant_id IS ?`, id, tenantID)
	return err
}

// --- Credentials ---

func (s *SQLiteCLIConnectionStore) PutCredential(ctx context.Context, cred store.CLIConnectionCredential) error {
	stored := cred.Secret
	if s.encKey != "" {
		enc, err := crypto.Encrypt(cred.Secret, s.encKey)
		if err != nil {
			return fmt.Errorf("encrypt cli-connection credential: %w", err)
		}
		stored = enc
	}
	nowStr := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO cli_connection_credentials
		   (connection_id, user_id, tenant_id, cred_type, inject, secret_enc, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?)
		 ON CONFLICT (connection_id, user_id)
		 DO UPDATE SET tenant_id = excluded.tenant_id, cred_type = excluded.cred_type,
		               inject = excluded.inject, secret_enc = excluded.secret_enc,
		               updated_at = excluded.updated_at`,
		cred.ConnectionID, cred.UserID, cred.TenantID, cred.Type, cred.Inject, stored, nowStr, nowStr,
	)
	return err
}

func (s *SQLiteCLIConnectionStore) GetCredential(ctx context.Context, connectionID uuid.UUID, userID string) (*store.CLIConnectionCredential, error) {
	// One round trip for both candidates: the user's own row and the tenant-shared
	// row (user_id = ''). ORDER BY puts the exact user first so LIMIT 1 picks it
	// when present and falls back to the shared row otherwise.
	cred := &store.CLIConnectionCredential{ConnectionID: connectionID}
	var updatedAt sqliteTime
	err := s.db.QueryRowContext(ctx,
		`SELECT user_id, tenant_id, cred_type, inject, secret_enc, updated_at
		   FROM cli_connection_credentials
		  WHERE connection_id = ? AND user_id IN (?, ?)
		  ORDER BY (user_id = ?) DESC
		  LIMIT 1`,
		connectionID, userID, store.SharedCredentialUserID, userID,
	).Scan(&cred.UserID, &cred.TenantID, &cred.Type, &cred.Inject, &cred.Secret, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	cred.UpdatedAt = updatedAt.Time
	if cred.Secret != "" && s.encKey != "" {
		dec, err := crypto.Decrypt(cred.Secret, s.encKey)
		if err != nil {
			return nil, fmt.Errorf("decrypt cli-connection credential: %w", err)
		}
		cred.Secret = dec
	}
	return cred, nil
}

func (s *SQLiteCLIConnectionStore) DeleteCredential(ctx context.Context, connectionID uuid.UUID, userID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM cli_connection_credentials WHERE connection_id = ? AND user_id = ?`,
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
	var createdAt, updatedAt sqliteTime
	if err := sc.Scan(
		&conn.ID, &conn.TenantID, &conn.Name, &conn.Kind, &conn.Provider, &conn.Mode,
		&conn.Endpoint, &conn.Enabled, &config, &conn.CreatedBy, &createdAt, &updatedAt,
	); err != nil {
		return nil, err
	}
	if len(config) > 0 {
		conn.Config = json.RawMessage(config)
	}
	conn.CreatedAt = createdAt.Time
	conn.UpdatedAt = updatedAt.Time
	return &conn, nil
}
