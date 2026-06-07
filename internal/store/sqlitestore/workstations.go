//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SQLiteWorkstationStore implements store.WorkstationStore backed by SQLite.
// metadata and default_env columns are AES-256-GCM encrypted at rest.
//
// permStore is optional: when non-nil, Create seeds default allowlist entries
// in the same DB transaction as the workstation row insert (H5 fix).
type SQLiteWorkstationStore struct {
	db        *sql.DB
	encKey    string
	permStore store.WorkstationPermissionStore
}

// NewSQLiteWorkstationStore creates a SQLiteWorkstationStore.
func NewSQLiteWorkstationStore(db *sql.DB, encryptionKey string) *SQLiteWorkstationStore {
	return &SQLiteWorkstationStore{db: db, encKey: encryptionKey}
}

// SetPermStore wires the permission store so Create can seed defaults atomically.
func (s *SQLiteWorkstationStore) SetPermStore(ps store.WorkstationPermissionStore) {
	s.permStore = ps
}

const wsSelectCols = `id, workstation_key, tenant_id, name, backend_type,
 metadata, default_cwd, default_env, active, created_at, updated_at, created_by`

// wsAllowedFields is the allowlist for Update().
var wsAllowedFields = map[string]bool{
	"name": true, "backend_type": true, "metadata": true,
	"default_cwd": true, "default_env": true, "active": true, "updated_at": true,
}

func (s *SQLiteWorkstationStore) encryptField(plaintext []byte, field string) ([]byte, error) {
	if len(plaintext) == 0 || s.encKey == "" {
		return plaintext, nil
	}
	enc, err := crypto.Encrypt(string(plaintext), s.encKey)
	if err != nil {
		return nil, fmt.Errorf("encrypt %s: %w", field, err)
	}
	return []byte(enc), nil
}

func (s *SQLiteWorkstationStore) decryptField(ciphertext []byte, field string) []byte {
	if len(ciphertext) == 0 || s.encKey == "" {
		return ciphertext
	}
	dec, err := crypto.Decrypt(string(ciphertext), s.encKey)
	if err != nil {
		slog.Warn("workstation: failed to decrypt field", "field", field, "error", err)
		return ciphertext
	}
	return []byte(dec)
}

// Create inserts a new workstation row and seeds default permission allowlist entries
// inside a single DB transaction (H5 fix: atomic — no partially-seeded state on crash).
func (s *SQLiteWorkstationStore) Create(ctx context.Context, ws *store.Workstation) error {
	if ws.ID == uuid.Nil {
		ws.ID = store.GenNewID()
	}
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required")
	}
	ws.TenantID = tid

	encMeta, err := s.encryptField(ws.Metadata, "metadata")
	if err != nil {
		return err
	}
	encEnv, err := s.encryptField(ws.DefaultEnv, "default_env")
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	ws.CreatedAt = now
	ws.UpdatedAt = now
	nowStr := now.Format(time.RFC3339Nano)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("workstation create begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err = tx.ExecContext(ctx,
		`INSERT INTO workstations
		 (id, workstation_key, tenant_id, name, backend_type, metadata, default_cwd, default_env,
		  active, created_at, updated_at, created_by)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		ws.ID.String(), ws.WorkstationKey, tid.String(), ws.Name, string(ws.BackendType),
		encMeta, ws.DefaultCWD, encEnv,
		boolToInt(ws.Active), nowStr, nowStr, ws.CreatedBy,
	); err != nil {
		return fmt.Errorf("workstation create: %w", err)
	}

	// Seed default binary allowlist inside same transaction (H5 fix).
	if s.permStore != nil {
		for _, pattern := range store.DefaultAllowedBinaries {
			if _, err = tx.ExecContext(ctx,
				`INSERT OR IGNORE INTO workstation_permissions
				 (id, workstation_id, tenant_id, pattern, enabled, created_by, created_at)
				 VALUES (?,?,?,?,1,'system',?)`,
				store.GenNewID().String(), ws.ID.String(), tid.String(), pattern, nowStr,
			); err != nil {
				return fmt.Errorf("seed permission %q: %w", pattern, err)
			}
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("workstation create commit: %w", err)
	}

	slog.Info("workstation.register",
		"workstation_id", ws.ID,
		"tenant_id", tid,
		"backend", ws.BackendType,
		"created_by", ws.CreatedBy,
	)
	return nil
}

func (s *SQLiteWorkstationStore) GetByID(ctx context.Context, id uuid.UUID) (*store.Workstation, error) {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return nil, sql.ErrNoRows
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT `+wsSelectCols+` FROM workstations WHERE id = ? AND tenant_id = ?`,
		id.String(), tid.String())
	return s.scanRow(row)
}

func (s *SQLiteWorkstationStore) GetByKey(ctx context.Context, key string) (*store.Workstation, error) {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return nil, sql.ErrNoRows
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT `+wsSelectCols+` FROM workstations WHERE workstation_key = ? AND tenant_id = ?`,
		key, tid.String())
	return s.scanRow(row)
}

func (s *SQLiteWorkstationStore) List(ctx context.Context) ([]store.Workstation, error) {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+wsSelectCols+` FROM workstations WHERE tenant_id = ? ORDER BY name`,
		tid.String())
	if err != nil {
		return nil, err
	}
	return s.scanRows(rows)
}

func (s *SQLiteWorkstationStore) Update(ctx context.Context, id uuid.UUID, updates map[string]any) error {
	for k := range updates {
		if !wsAllowedFields[k] {
			delete(updates, k)
		}
	}
	if len(updates) == 0 {
		return nil
	}

	for _, field := range []string{"metadata", "default_env"} {
		if raw, ok := updates[field]; ok {
			var plainBytes []byte
			switch v := raw.(type) {
			case []byte:
				plainBytes = v
			case string:
				plainBytes = []byte(v)
			}
			if len(plainBytes) > 0 {
				enc, err := s.encryptField(plainBytes, field)
				if err != nil {
					return err
				}
				updates[field] = enc
			}
		}
	}
	updates["updated_at"] = time.Now().UTC().Format(time.RFC3339Nano)

	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required for update")
	}
	return execMapUpdateWhereTenant(ctx, s.db, "workstations", updates, id, tid)
}

func (s *SQLiteWorkstationStore) SetActive(ctx context.Context, id uuid.UUID, active bool) error {
	return s.Update(ctx, id, map[string]any{"active": boolToInt(active)})
}

func (s *SQLiteWorkstationStore) Delete(ctx context.Context, id uuid.UUID) error {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required")
	}
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM workstations WHERE id = ? AND tenant_id = ?`,
		id.String(), tid.String())
	return err
}

func (s *SQLiteWorkstationStore) scanRow(row *sql.Row) (*store.Workstation, error) {
	var ws store.Workstation
	var idStr, tenantStr, backendStr string
	var meta, env []byte
	var activeInt int
	var createdAt, updatedAt sqliteTime

	err := row.Scan(
		&idStr, &ws.WorkstationKey, &tenantStr, &ws.Name, &backendStr,
		&meta, &ws.DefaultCWD, &env,
		&activeInt, &createdAt, &updatedAt, &ws.CreatedBy,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, sql.ErrNoRows
		}
		return nil, err
	}
	ws.ID, _ = uuid.Parse(idStr)
	ws.TenantID, _ = uuid.Parse(tenantStr)
	ws.BackendType = store.WorkstationBackend(backendStr)
	ws.Active = activeInt != 0
	ws.CreatedAt = createdAt.Time
	ws.UpdatedAt = updatedAt.Time
	ws.Metadata = s.decryptField(meta, "metadata")
	ws.DefaultEnv = s.decryptField(env, "default_env")
	return &ws, nil
}

func (s *SQLiteWorkstationStore) scanRows(rows *sql.Rows) ([]store.Workstation, error) {
	defer rows.Close()
	var result []store.Workstation
	for rows.Next() {
		var ws store.Workstation
		var idStr, tenantStr, backendStr string
		var meta, env []byte
		var activeInt int
		var createdAt, updatedAt sqliteTime
		if err := rows.Scan(
			&idStr, &ws.WorkstationKey, &tenantStr, &ws.Name, &backendStr,
			&meta, &ws.DefaultCWD, &env,
			&activeInt, &createdAt, &updatedAt, &ws.CreatedBy,
		); err != nil {
			continue
		}
		ws.ID, _ = uuid.Parse(idStr)
		ws.TenantID, _ = uuid.Parse(tenantStr)
		ws.BackendType = store.WorkstationBackend(backendStr)
		ws.Active = activeInt != 0
		ws.CreatedAt = createdAt.Time
		ws.UpdatedAt = updatedAt.Time
		ws.Metadata = s.decryptField(meta, "metadata")
		ws.DefaultEnv = s.decryptField(env, "default_env")
		result = append(result, ws)
	}
	return result, rows.Err()
}

// boolToInt converts bool to SQLite integer (1/0).
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
