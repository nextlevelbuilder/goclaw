//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SQLiteCheckpointSnapshotStore implements store.CheckpointSnapshotStore backed
// by SQLite. Snapshot is stored as TEXT; list reads are tenant-scoped and
// newest-first.
type SQLiteCheckpointSnapshotStore struct {
	db *sql.DB
}

// NewSQLiteCheckpointSnapshotStore creates a SQLite-backed checkpoint snapshot
// store.
func NewSQLiteCheckpointSnapshotStore(db *sql.DB) *SQLiteCheckpointSnapshotStore {
	return &SQLiteCheckpointSnapshotStore{db: db}
}

// AppendCheckpointSnapshot inserts one append-only snapshot row. Seq is the
// caller-provided monotonic snapshot sequence for the run (newest first reads
// rely on it). Status must be a known snapshot status.
func (s *SQLiteCheckpointSnapshotStore) AppendCheckpointSnapshot(ctx context.Context, snap *store.CheckpointSnapshot) error {
	if snap.ID == uuid.Nil {
		snap.ID = store.GenNewID()
	}
	if snap.CreatedAt.IsZero() {
		snap.CreatedAt = time.Now()
	}
	if snap.Status == "" {
		snap.Status = store.CheckpointSnapshotPaused
	}
	if !store.ValidCheckpointSnapshotStatus(snap.Status) {
		return fmt.Errorf("append checkpoint snapshot: unknown status %q", snap.Status)
	}
	if len(snap.Snapshot) == 0 {
		return fmt.Errorf("append checkpoint snapshot: snapshot required")
	}
	snap.TenantID = tenantIDForInsert(ctx)

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO run_checkpoint_snapshots (id, tenant_id, run_id, seq, snapshot, status, iteration, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		snap.ID.String(), snap.TenantID.String(), snap.RunID, snap.Seq,
		string(snap.Snapshot), snap.Status, snap.Iteration,
		snap.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("append checkpoint snapshot: insert: %w", err)
	}
	return nil
}

// GetCheckpointSnapshot returns one snapshot by (run_id, seq), scoped to the
// context tenant. Fails closed (WHERE 1=0) when a tenant ID is required but
// absent, so a record from another tenant cannot be leaked.
func (s *SQLiteCheckpointSnapshotStore) GetCheckpointSnapshot(ctx context.Context, runID string, seq int) (*store.CheckpointSnapshot, error) {
	where, args := buildSQLiteCheckpointSnapshotGetWhere(ctx, runID, seq)
	q := `SELECT id, tenant_id, run_id, seq, snapshot, status, iteration, created_at
		 FROM run_checkpoint_snapshots` + where
	var row checkpointSnapshotRow
	if err := scanCheckpointSnapshotRow(s.db.QueryRowContext(ctx, q, args...).Scan, &row); err != nil {
		return nil, err
	}
	got := row.toStore()
	return &got, nil
}

// ListCheckpointSnapshots returns snapshot rows filtered by opts, scoped to the
// context tenant. Newest first.
func (s *SQLiteCheckpointSnapshotStore) ListCheckpointSnapshots(ctx context.Context, opts store.CheckpointSnapshotListOpts) ([]store.CheckpointSnapshot, error) {
	where, args := buildSQLiteCheckpointSnapshotListWhere(ctx, opts)
	limit := opts.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := `SELECT id, tenant_id, run_id, seq, snapshot, status, iteration, created_at
		 FROM run_checkpoint_snapshots` + where +
		` ORDER BY seq DESC, id DESC` +
		fmt.Sprintf(" LIMIT %d OFFSET %d", limit, opts.Offset)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []store.CheckpointSnapshot
	for rows.Next() {
		var row checkpointSnapshotRow
		if err := scanCheckpointSnapshotRow(rows.Scan, &row); err != nil {
			return nil, err
		}
		items = append(items, row.toStore())
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if items == nil {
		items = []store.CheckpointSnapshot{}
	}
	return items, nil
}

// buildSQLiteCheckpointSnapshotGetWhere scopes a single-snapshot read. Fails
// closed (WHERE 1=0) when a tenant ID is required but absent from the context.
func buildSQLiteCheckpointSnapshotGetWhere(ctx context.Context, runID string, seq int) (string, []any) {
	if !store.IsCrossTenant(ctx) {
		tenantID := store.TenantIDFromContext(ctx)
		if tenantID == uuid.Nil {
			return " WHERE 1=0", nil
		}
		return " WHERE run_id = ? AND seq = ? AND tenant_id = ?", []any{runID, seq, tenantID.String()}
	}
	return " WHERE run_id = ? AND seq = ?", []any{runID, seq}
}

// buildSQLiteCheckpointSnapshotListWhere scopes a snapshot list read. RunID is
// optional; the caller is expected to set it. Fails closed (WHERE 1=0) when a
// tenant ID is required but absent.
func buildSQLiteCheckpointSnapshotListWhere(ctx context.Context, opts store.CheckpointSnapshotListOpts) (string, []any) {
	var conditions []string
	var args []any
	if !store.IsCrossTenant(ctx) {
		tenantID := store.TenantIDFromContext(ctx)
		if tenantID == uuid.Nil {
			return " WHERE 1=0", nil
		}
		conditions = append(conditions, "tenant_id = ?")
		args = append(args, tenantID.String())
	}
	if opts.RunID != "" {
		conditions = append(conditions, "run_id = ?")
		args = append(args, opts.RunID)
	}
	if len(conditions) == 0 {
		return " WHERE 1=0", nil
	}
	return " WHERE " + strings.Join(conditions, " AND "), args
}

type checkpointSnapshotRow struct {
	ID        uuid.UUID
	TenantID  uuid.UUID
	RunID     string
	Seq       int
	Snapshot  string
	Status    sql.NullString
	Iteration int
	CreatedAt sqliteTime
}

// scanCheckpointSnapshotRow scans one run_checkpoint_snapshots row, converting
// SQLite's TEXT UUIDs and timestamps into Go values.
func scanCheckpointSnapshotRow(scan func(dest ...any) error, r *checkpointSnapshotRow) error {
	var id, tenantID string
	var createdAt sqliteTime
	if err := scan(
		&id, &tenantID, &r.RunID, &r.Seq, &r.Snapshot, &r.Status, &r.Iteration, &createdAt,
	); err != nil {
		return err
	}
	r.ID = uuid.MustParse(id)
	r.TenantID = uuid.MustParse(tenantID)
	r.CreatedAt = createdAt
	return nil
}

func (r checkpointSnapshotRow) toStore() store.CheckpointSnapshot {
	return store.CheckpointSnapshot{
		ID:        r.ID,
		TenantID:  r.TenantID,
		RunID:     r.RunID,
		Seq:       r.Seq,
		Snapshot:  []byte(r.Snapshot),
		Status:    r.Status.String,
		Iteration: r.Iteration,
		CreatedAt: r.CreatedAt.Time,
	}
}