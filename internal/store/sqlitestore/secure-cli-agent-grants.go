//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SQLiteSecureCLIAgentGrantStore implements store.SecureCLIAgentGrantStore backed by SQLite.
type SQLiteSecureCLIAgentGrantStore struct {
	db *sql.DB
}

// NewSQLiteSecureCLIAgentGrantStore creates a new SQLiteSecureCLIAgentGrantStore.
func NewSQLiteSecureCLIAgentGrantStore(db *sql.DB) *SQLiteSecureCLIAgentGrantStore {
	return &SQLiteSecureCLIAgentGrantStore{db: db}
}

const grantSelectCols = `id, binary_id, agent_id, deny_args, deny_verbose, timeout_seconds, tips, enabled, created_at, updated_at`

func (s *SQLiteSecureCLIAgentGrantStore) Create(ctx context.Context, g *store.SecureCLIAgentGrant) error {
	if g.ID == uuid.Nil {
		g.ID = store.GenNewID()
	}
	now := time.Now().UTC()
	g.CreatedAt = now
	g.UpdatedAt = now
	nowStr := now.Format(time.RFC3339Nano)

	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		tenantID = store.MasterTenantID
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO secure_cli_agent_grants
		 (id, binary_id, agent_id, deny_args, deny_verbose, timeout_seconds, tips, enabled, tenant_id, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		g.ID, g.BinaryID, g.AgentID,
		nullableJSONRaw(g.DenyArgs), nullableJSONRaw(g.DenyVerbose),
		g.TimeoutSeconds, g.Tips,
		g.Enabled, tenantID, nowStr, nowStr,
	)
	return err
}

func (s *SQLiteSecureCLIAgentGrantStore) Get(ctx context.Context, id uuid.UUID) (*store.SecureCLIAgentGrant, error) {
	query := `SELECT ` + grantSelectCols + ` FROM secure_cli_agent_grants WHERE id = ?`
	args := []any{id}
	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			return nil, sql.ErrNoRows
		}
		query += ` AND tenant_id = ?`
		args = append(args, tid)
	}
	row := s.db.QueryRowContext(ctx, query, args...)
	return s.scanRow(row)
}

var grantAllowedFields = map[string]bool{
	"deny_args": true, "deny_verbose": true, "timeout_seconds": true,
	"tips": true, "enabled": true, "updated_at": true,
}

func (s *SQLiteSecureCLIAgentGrantStore) Update(ctx context.Context, id uuid.UUID, updates map[string]any) error {
	for k := range updates {
		if !grantAllowedFields[k] {
			delete(updates, k)
		}
	}
	updates["updated_at"] = time.Now().UTC().Format(time.RFC3339Nano)

	if store.IsCrossTenant(ctx) {
		return execMapUpdate(ctx, s.db, "secure_cli_agent_grants", id, updates)
	}
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required")
	}
	return execMapUpdateWhereTenant(ctx, s.db, "secure_cli_agent_grants", updates, id, tid)
}

func (s *SQLiteSecureCLIAgentGrantStore) Delete(ctx context.Context, id uuid.UUID) error {
	if store.IsCrossTenant(ctx) {
		_, err := s.db.ExecContext(ctx, "DELETE FROM secure_cli_agent_grants WHERE id = ?", id)
		return err
	}
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required")
	}
	_, err := s.db.ExecContext(ctx, "DELETE FROM secure_cli_agent_grants WHERE id = ? AND tenant_id = ?", id, tid)
	return err
}

func (s *SQLiteSecureCLIAgentGrantStore) ListByBinary(ctx context.Context, binaryID uuid.UUID) ([]store.SecureCLIAgentGrant, error) {
	query := `SELECT ` + grantSelectCols + ` FROM secure_cli_agent_grants WHERE binary_id = ?`
	args := []any{binaryID}
	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			return nil, nil
		}
		query += ` AND tenant_id = ?`
		args = append(args, tid)
	}
	query += ` ORDER BY created_at`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return s.scanRows(rows)
}

func (s *SQLiteSecureCLIAgentGrantStore) ListByAgent(ctx context.Context, agentID uuid.UUID) ([]store.SecureCLIAgentGrant, error) {
	query := `SELECT ` + grantSelectCols + ` FROM secure_cli_agent_grants WHERE agent_id = ?`
	args := []any{agentID}
	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			return nil, nil
		}
		query += ` AND tenant_id = ?`
		args = append(args, tid)
	}
	query += ` ORDER BY created_at`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return s.scanRows(rows)
}

func (s *SQLiteSecureCLIAgentGrantStore) scanRow(row *sql.Row) (*store.SecureCLIAgentGrant, error) {
	var g store.SecureCLIAgentGrant
	var denyArgs, denyVerbose []byte
	var timeout *int
	var tips *string
	var createdAt, updatedAt sqliteTime

	err := row.Scan(
		&g.ID, &g.BinaryID, &g.AgentID,
		&denyArgs, &denyVerbose, &timeout, &tips,
		&g.Enabled, &createdAt, &updatedAt,
	)
	if err != nil {
		return nil, err
	}
	applyGrantNullable(&g, denyArgs, denyVerbose, timeout, tips)
	g.CreatedAt = createdAt.Time
	g.UpdatedAt = updatedAt.Time
	return &g, nil
}

func (s *SQLiteSecureCLIAgentGrantStore) scanRows(rows *sql.Rows) ([]store.SecureCLIAgentGrant, error) {
	defer rows.Close()
	var result []store.SecureCLIAgentGrant
	for rows.Next() {
		var g store.SecureCLIAgentGrant
		var denyArgs, denyVerbose []byte
		var timeout *int
		var tips *string
		var createdAt, updatedAt sqliteTime

		if err := rows.Scan(
			&g.ID, &g.BinaryID, &g.AgentID,
			&denyArgs, &denyVerbose, &timeout, &tips,
			&g.Enabled, &createdAt, &updatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan secure_cli_agent_grants row: %w", err)
		}
		applyGrantNullable(&g, denyArgs, denyVerbose, timeout, tips)
		g.CreatedAt = createdAt.Time
		g.UpdatedAt = updatedAt.Time
		result = append(result, g)
	}
	return result, rows.Err()
}

// applyGrantNullable converts scanned nullable values to pointer fields on the grant struct.
func applyGrantNullable(g *store.SecureCLIAgentGrant, denyArgs, denyVerbose []byte, timeout *int, tips *string) {
	if len(denyArgs) > 0 {
		raw := json.RawMessage(denyArgs)
		g.DenyArgs = &raw
	}
	if len(denyVerbose) > 0 {
		raw := json.RawMessage(denyVerbose)
		g.DenyVerbose = &raw
	}
	g.TimeoutSeconds = timeout
	g.Tips = tips
}

// nullableJSONRaw returns nil if the pointer is nil, otherwise the raw bytes.
func nullableJSONRaw(v *json.RawMessage) any {
	if v == nil {
		return nil
	}
	return []byte(*v)
}
