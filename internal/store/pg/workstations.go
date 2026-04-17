package pg

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

// PGWorkstationStore implements store.WorkstationStore backed by PostgreSQL.
// metadata and default_env columns are AES-256-GCM encrypted at rest.
//
// permStore is optional: when non-nil, Create seeds default allowlist entries
// inside the same DB transaction as the workstation row insert (H5 fix).
// Without atomicity, a crash between insert and seed leaves a permanently-locked
// workstation (default-deny with empty allowlist).
type PGWorkstationStore struct {
	db        *sql.DB
	encKey    string
	permStore store.WorkstationPermissionStore // may be nil until Phase 6 wiring
}

// NewPGWorkstationStore creates a PGWorkstationStore with the given DB + encryption key.
func NewPGWorkstationStore(db *sql.DB, encryptionKey string) *PGWorkstationStore {
	return &PGWorkstationStore{db: db, encKey: encryptionKey}
}

// SetPermStore wires the permission store so Create can seed defaults atomically.
// Call this after both stores are initialised (avoids circular construction).
func (s *PGWorkstationStore) SetPermStore(ps store.WorkstationPermissionStore) {
	s.permStore = ps
}

const workstationSelectCols = `id, workstation_key, tenant_id, name, backend_type,
 metadata, default_cwd, default_env, active, created_at, updated_at, created_by`

// workstationAllowedFields is the allowlist for Update().
var workstationAllowedFields = map[string]bool{
	"name": true, "backend_type": true, "metadata": true,
	"default_cwd": true, "default_env": true, "active": true, "updated_at": true,
}

func (s *PGWorkstationStore) encryptField(plaintext []byte, field string) ([]byte, error) {
	if len(plaintext) == 0 || s.encKey == "" {
		return plaintext, nil
	}
	enc, err := crypto.Encrypt(string(plaintext), s.encKey)
	if err != nil {
		return nil, fmt.Errorf("encrypt %s: %w", field, err)
	}
	return []byte(enc), nil
}

func (s *PGWorkstationStore) decryptField(ciphertext []byte, field string) []byte {
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
func (s *PGWorkstationStore) Create(ctx context.Context, ws *store.Workstation) error {
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

	now := time.Now()
	ws.CreatedAt = now
	ws.UpdatedAt = now

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("workstation create begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err = tx.ExecContext(ctx,
		`INSERT INTO workstations
		 (id, workstation_key, tenant_id, name, backend_type, metadata, default_cwd, default_env,
		  active, created_at, updated_at, created_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		ws.ID, ws.WorkstationKey, tid, ws.Name, ws.BackendType,
		encMeta, ws.DefaultCWD, encEnv,
		ws.Active, now, now, ws.CreatedBy,
	); err != nil {
		return fmt.Errorf("workstation create: %w", err)
	}

	// Seed default binary allowlist inside same transaction (H5 fix).
	// If permStore is not wired yet (e.g. test environment), skip seeding gracefully.
	if s.permStore != nil {
		for _, pattern := range store.DefaultAllowedBinaries {
			if _, err = tx.ExecContext(ctx,
				`INSERT INTO workstation_permissions
				 (id, workstation_id, tenant_id, pattern, enabled, created_by, created_at)
				 VALUES ($1,$2,$3,$4,TRUE,'system',NOW())
				 ON CONFLICT (workstation_id, pattern) DO NOTHING`,
				store.GenNewID(), ws.ID, tid, pattern,
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

func (s *PGWorkstationStore) GetByID(ctx context.Context, id uuid.UUID) (*store.Workstation, error) {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return nil, sql.ErrNoRows
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT `+workstationSelectCols+` FROM workstations WHERE id = $1 AND tenant_id = $2`,
		id, tid)
	return s.scanRow(row)
}

func (s *PGWorkstationStore) GetByKey(ctx context.Context, key string) (*store.Workstation, error) {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return nil, sql.ErrNoRows
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT `+workstationSelectCols+` FROM workstations WHERE workstation_key = $1 AND tenant_id = $2`,
		key, tid)
	return s.scanRow(row)
}

func (s *PGWorkstationStore) List(ctx context.Context) ([]store.Workstation, error) {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+workstationSelectCols+` FROM workstations WHERE tenant_id = $1 ORDER BY name`,
		tid)
	if err != nil {
		return nil, err
	}
	return s.scanRows(rows)
}

func (s *PGWorkstationStore) Update(ctx context.Context, id uuid.UUID, updates map[string]any) error {
	for k := range updates {
		if !workstationAllowedFields[k] {
			delete(updates, k)
		}
	}
	if len(updates) == 0 {
		return nil
	}

	// Encrypt metadata/default_env if present in updates.
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
	updates["updated_at"] = time.Now()

	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required for update")
	}
	return execMapUpdateWhereTenant(ctx, s.db, "workstations", updates, id, tid)
}

func (s *PGWorkstationStore) SetActive(ctx context.Context, id uuid.UUID, active bool) error {
	return s.Update(ctx, id, map[string]any{"active": active})
}

func (s *PGWorkstationStore) Delete(ctx context.Context, id uuid.UUID) error {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required")
	}
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM workstations WHERE id = $1 AND tenant_id = $2`, id, tid)
	return err
}

func (s *PGWorkstationStore) scanRow(row *sql.Row) (*store.Workstation, error) {
	var ws store.Workstation
	var meta, env []byte
	err := row.Scan(
		&ws.ID, &ws.WorkstationKey, &ws.TenantID, &ws.Name, &ws.BackendType,
		&meta, &ws.DefaultCWD, &env,
		&ws.Active, &ws.CreatedAt, &ws.UpdatedAt, &ws.CreatedBy,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, sql.ErrNoRows
		}
		return nil, err
	}
	ws.Metadata = s.decryptField(meta, "metadata")
	ws.DefaultEnv = s.decryptField(env, "default_env")
	return &ws, nil
}

func (s *PGWorkstationStore) scanRows(rows *sql.Rows) ([]store.Workstation, error) {
	defer rows.Close()
	var result []store.Workstation
	for rows.Next() {
		var ws store.Workstation
		var meta, env []byte
		if err := rows.Scan(
			&ws.ID, &ws.WorkstationKey, &ws.TenantID, &ws.Name, &ws.BackendType,
			&meta, &ws.DefaultCWD, &env,
			&ws.Active, &ws.CreatedAt, &ws.UpdatedAt, &ws.CreatedBy,
		); err != nil {
			slog.Error("workstation.scan_error", "err", err)
			continue
		}
		ws.Metadata = s.decryptField(meta, "metadata")
		ws.DefaultEnv = s.decryptField(env, "default_env")
		result = append(result, ws)
	}
	return result, rows.Err()
}
