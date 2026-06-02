//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SQLiteWorkstationPermissionStore implements store.WorkstationPermissionStore backed by SQLite.
type SQLiteWorkstationPermissionStore struct {
	db *sql.DB
}

// NewSQLiteWorkstationPermissionStore creates a SQLiteWorkstationPermissionStore.
func NewSQLiteWorkstationPermissionStore(db *sql.DB) *SQLiteWorkstationPermissionStore {
	return &SQLiteWorkstationPermissionStore{db: db}
}

const sqliteWPSelectCols = `id, workstation_id, tenant_id, pattern, enabled, created_by, created_at`

func (s *SQLiteWorkstationPermissionStore) ListForWorkstation(ctx context.Context, workstationID uuid.UUID) ([]store.WorkstationPermission, error) {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+sqliteWPSelectCols+` FROM workstation_permissions
		 WHERE workstation_id = ? AND tenant_id = ?
		 ORDER BY created_at`,
		workstationID.String(), tid.String())
	if err != nil {
		return nil, fmt.Errorf("workstation_permissions list: %w", err)
	}
	defer rows.Close()
	return sqliteScanPermRows(rows)
}

func (s *SQLiteWorkstationPermissionStore) Add(ctx context.Context, perm *store.WorkstationPermission) error {
	if perm.ID == uuid.Nil {
		perm.ID = store.GenNewID()
	}
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required")
	}
	perm.TenantID = tid
	if perm.CreatedAt.IsZero() {
		perm.CreatedAt = time.Now()
	}
	enabledInt := 0
	if perm.Enabled {
		enabledInt = 1
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO workstation_permissions
		 (id, workstation_id, tenant_id, pattern, enabled, created_by, created_at)
		 VALUES (?,?,?,?,?,?,?)`,
		perm.ID.String(), perm.WorkstationID.String(), tid.String(),
		perm.Pattern, enabledInt, perm.CreatedBy,
		perm.CreatedAt.Format("2006-01-02T15:04:05.000Z"),
	)
	if err != nil {
		return fmt.Errorf("workstation_permissions add: %w", err)
	}
	return nil
}

func (s *SQLiteWorkstationPermissionStore) Remove(ctx context.Context, id uuid.UUID) error {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required")
	}
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM workstation_permissions WHERE id = ? AND tenant_id = ?`,
		id.String(), tid.String())
	if err != nil {
		return fmt.Errorf("workstation_permissions remove: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *SQLiteWorkstationPermissionStore) SetEnabled(ctx context.Context, id uuid.UUID, enabled bool) error {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required")
	}
	enabledInt := 0
	if enabled {
		enabledInt = 1
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE workstation_permissions SET enabled = ? WHERE id = ? AND tenant_id = ?`,
		enabledInt, id.String(), tid.String())
	return err
}

// SeedDefaults inserts default safe binary names for a new workstation.
// Uses INSERT OR IGNORE — safe to call multiple times.
// Must be called inside the same transaction as workstation creation (H5 fix).
func (s *SQLiteWorkstationPermissionStore) SeedDefaults(ctx context.Context, workstationID, tenantID uuid.UUID) error {
	now := time.Now().Format("2006-01-02T15:04:05.000Z")
	for _, pattern := range store.DefaultAllowedBinaries {
		_, err := s.db.ExecContext(ctx,
			`INSERT OR IGNORE INTO workstation_permissions
			 (id, workstation_id, tenant_id, pattern, enabled, created_by, created_at)
			 VALUES (?,?,?,?,1,'system',?)`,
			store.GenNewID().String(), workstationID.String(), tenantID.String(), pattern, now,
		)
		if err != nil {
			return fmt.Errorf("seed default permission %q: %w", pattern, err)
		}
	}
	return nil
}

func sqliteScanPermRows(rows *sql.Rows) ([]store.WorkstationPermission, error) {
	var result []store.WorkstationPermission
	for rows.Next() {
		var p store.WorkstationPermission
		var idStr, wsIDStr, tenantIDStr, createdAtStr string
		var enabledInt int
		err := rows.Scan(&idStr, &wsIDStr, &tenantIDStr, &p.Pattern,
			&enabledInt, &p.CreatedBy, &createdAtStr)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				break
			}
			return nil, fmt.Errorf("scan workstation_permission: %w", err)
		}
		p.ID, _ = uuid.Parse(idStr)
		p.WorkstationID, _ = uuid.Parse(wsIDStr)
		p.TenantID, _ = uuid.Parse(tenantIDStr)
		p.Enabled = enabledInt != 0
		if t, err := time.Parse("2006-01-02T15:04:05.000Z", createdAtStr); err == nil {
			p.CreatedAt = t
		}
		result = append(result, p)
	}
	return result, rows.Err()
}
