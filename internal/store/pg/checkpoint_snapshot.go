package pg

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// PGCheckpointSnapshotStore implements store.CheckpointSnapshotStore backed by
// PostgreSQL. Snapshot is stored as JSONB (Postgres enforces valid JSON); list
// reads are tenant-scoped and newest-first.
type PGCheckpointSnapshotStore struct {
	db *sql.DB
}

// NewPGCheckpointSnapshotStore creates a PG-backed checkpoint snapshot store.
func NewPGCheckpointSnapshotStore(db *sql.DB) *PGCheckpointSnapshotStore {
	return &PGCheckpointSnapshotStore{db: db}
}

// AppendCheckpointSnapshot inserts one append-only snapshot row. Seq is the
// caller-provided monotonic snapshot sequence for the run (newest first reads
// rely on it). Status must be a known snapshot status.
func (s *PGCheckpointSnapshotStore) AppendCheckpointSnapshot(ctx context.Context, snap *store.CheckpointSnapshot) error {
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
		 VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, $8)`,
		snap.ID, snap.TenantID, snap.RunID, snap.Seq, snap.Snapshot, snap.Status, snap.Iteration, snap.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("append checkpoint snapshot: insert: %w", err)
	}
	return nil
}

// GetCheckpointSnapshot returns one snapshot by (run_id, seq), scoped to the
// context tenant. Fails closed (WHERE 1=0) when a tenant ID is required but
// absent, so a record from another tenant cannot be leaked.
func (s *PGCheckpointSnapshotStore) GetCheckpointSnapshot(ctx context.Context, runID string, seq int) (*store.CheckpointSnapshot, error) {
	where, args := buildCheckpointSnapshotGetWhere(ctx, runID, seq)
	q := `SELECT id, tenant_id, run_id, seq, snapshot::text AS snapshot, status, iteration, created_at
		 FROM run_checkpoint_snapshots` + where
	var row checkpointSnapshotRow
	if err := pkgSqlxDB.GetContext(ctx, &row, q, args...); err != nil {
		return nil, err
	}
	got := row.toStore()
	return &got, nil
}

// ListCheckpointSnapshots returns snapshot rows filtered by opts, scoped to the
// context tenant. Newest first.
func (s *PGCheckpointSnapshotStore) ListCheckpointSnapshots(ctx context.Context, opts store.CheckpointSnapshotListOpts) ([]store.CheckpointSnapshot, error) {
	where, args := buildCheckpointSnapshotListWhere(ctx, opts)
	limit := opts.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := `SELECT id, tenant_id, run_id, seq, snapshot::text AS snapshot, status, iteration, created_at
		 FROM run_checkpoint_snapshots` + where +
		` ORDER BY seq DESC, id DESC` +
		fmt.Sprintf(" OFFSET %d LIMIT %d", opts.Offset, limit)

	var rows []checkpointSnapshotRow
	if err := pkgSqlxDB.SelectContext(ctx, &rows, q, args...); err != nil {
		return nil, err
	}
	items := make([]store.CheckpointSnapshot, len(rows))
	for i, row := range rows {
		items[i] = row.toStore()
	}
	return items, nil
}

// buildCheckpointSnapshotGetWhere scopes a single-snapshot read. Fails closed
// (WHERE 1=0) when a tenant ID is required but absent from the context.
func buildCheckpointSnapshotGetWhere(ctx context.Context, runID string, seq int) (string, []any) {
	if !store.IsCrossTenant(ctx) {
		tenantID := store.TenantIDFromContext(ctx)
		if tenantID == uuid.Nil {
			return " WHERE 1=0", nil
		}
		return " WHERE run_id = $1 AND seq = $2 AND tenant_id = $3", []any{runID, seq, tenantID}
	}
	return " WHERE run_id = $1 AND seq = $2", []any{runID, seq}
}

// buildCheckpointSnapshotListWhere scopes a snapshot list read. RunID is
// optional; the caller is expected to set it (a list without a run is an empty
// result). Fails closed (WHERE 1=0) when a tenant ID is required but absent.
func buildCheckpointSnapshotListWhere(ctx context.Context, opts store.CheckpointSnapshotListOpts) (string, []any) {
	var conditions []string
	var args []any
	argIdx := 1
	if !store.IsCrossTenant(ctx) {
		tenantID := store.TenantIDFromContext(ctx)
		if tenantID == uuid.Nil {
			return " WHERE 1=0", nil
		}
		conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argIdx))
		args = append(args, tenantID)
		argIdx++
	}
	if opts.RunID != "" {
		conditions = append(conditions, fmt.Sprintf("run_id = $%d", argIdx))
		args = append(args, opts.RunID)
		argIdx++
	}
	if len(conditions) == 0 {
		return " WHERE 1=0", nil
	}
	return " WHERE " + strings.Join(conditions, " AND "), args
}

type checkpointSnapshotRow struct {
	ID        uuid.UUID `db:"id"`
	TenantID  uuid.UUID `db:"tenant_id"`
	RunID     string    `db:"run_id"`
	Seq       int       `db:"seq"`
	Snapshot  []byte    `db:"snapshot"`
	Status    *string   `db:"status"`
	Iteration int       `db:"iteration"`
	CreatedAt time.Time `db:"created_at"`
}

func (r checkpointSnapshotRow) toStore() store.CheckpointSnapshot {
	return store.CheckpointSnapshot{
		ID:        r.ID,
		TenantID:  r.TenantID,
		RunID:     r.RunID,
		Seq:       r.Seq,
		Snapshot:  r.Snapshot,
		Status:    derefStr(r.Status),
		Iteration: r.Iteration,
		CreatedAt: r.CreatedAt,
	}
}