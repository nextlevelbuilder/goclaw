package pg

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	activityBufferSize  = 1000
	activityBatchMax    = 100
	activityFlushPeriod = 500 * time.Millisecond
)

// PGWorkstationActivityStore implements store.WorkstationActivityStore backed by Postgres.
// Inserts are buffered (channel size 1000) and flushed in batches every 500ms or 100 rows,
// keeping exec hot-path latency below 1ms.
type PGWorkstationActivityStore struct {
	db  *sql.DB
	buf chan *store.WorkstationActivity
	wg  sync.WaitGroup
}

// NewPGWorkstationActivityStore creates the store and starts the background flush goroutine.
func NewPGWorkstationActivityStore(db *sql.DB) *PGWorkstationActivityStore {
	s := &PGWorkstationActivityStore{
		db:  db,
		buf: make(chan *store.WorkstationActivity, activityBufferSize),
	}
	s.wg.Add(1)
	go s.flusher()
	return s
}

// Insert enqueues the row for async batch insert. Drops and warns if buffer is full.
func (s *PGWorkstationActivityStore) Insert(_ context.Context, row *store.WorkstationActivity) error {
	select {
	case s.buf <- row:
	default:
		slog.Warn("workstation.activity.buffer_full", "action", row.Action)
	}
	return nil
}

// List returns up to limit rows for the workstation, newest first.
// Cursor-based pagination: pass last seen ID to continue from that point.
func (s *PGWorkstationActivityStore) List(ctx context.Context, workstationID uuid.UUID, limit int, cursor *uuid.UUID) ([]store.WorkstationActivity, *uuid.UUID, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	var rows *sql.Rows
	var err error
	if cursor == nil {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, tenant_id, workstation_id, agent_id, action, cmd_hash, cmd_preview,
			        exit_code, duration_ms, deny_reason, created_at
			 FROM workstation_activity
			 WHERE workstation_id = $1
			 ORDER BY created_at DESC
			 LIMIT $2`,
			workstationID, limit+1,
		)
	} else {
		// Cursor: created_at of the cursor row acts as the page boundary.
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, tenant_id, workstation_id, agent_id, action, cmd_hash, cmd_preview,
			        exit_code, duration_ms, deny_reason, created_at
			 FROM workstation_activity
			 WHERE workstation_id = $1
			   AND created_at < (SELECT created_at FROM workstation_activity WHERE id = $2)
			 ORDER BY created_at DESC
			 LIMIT $3`,
			workstationID, *cursor, limit+1,
		)
	}
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var result []store.WorkstationActivity
	for rows.Next() {
		var a store.WorkstationActivity
		if err := rows.Scan(
			&a.ID, &a.TenantID, &a.WorkstationID, &a.AgentID, &a.Action,
			&a.CmdHash, &a.CmdPreview, &a.ExitCode, &a.DurationMS, &a.DenyReason, &a.CreatedAt,
		); err != nil {
			return nil, nil, err
		}
		result = append(result, a)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	var nextCursor *uuid.UUID
	if len(result) > limit {
		last := result[limit-1].ID
		nextCursor = &last
		result = result[:limit]
	}
	return result, nextCursor, nil
}

// Prune deletes rows created before the given time in batches to avoid long locks.
// Returns total rows deleted.
func (s *PGWorkstationActivityStore) Prune(ctx context.Context, before time.Time) (int64, error) {
	var total int64
	for {
		res, err := s.db.ExecContext(ctx,
			`DELETE FROM workstation_activity
			 WHERE id IN (
			   SELECT id FROM workstation_activity WHERE created_at < $1 LIMIT 1000
			 )`,
			before,
		)
		if err != nil {
			return total, err
		}
		n, _ := res.RowsAffected()
		total += n
		if n < 1000 {
			break
		}
		// Brief sleep between batches to reduce lock pressure.
		time.Sleep(100 * time.Millisecond)
	}
	return total, nil
}

// flusher reads from buf and batch-inserts into the DB every 500ms or 100 rows.
func (s *PGWorkstationActivityStore) flusher() {
	defer s.wg.Done()
	ticker := time.NewTicker(activityFlushPeriod)
	defer ticker.Stop()

	var batch []*store.WorkstationActivity
	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := s.batchInsert(context.Background(), batch); err != nil {
			slog.Warn("workstation.activity.flush_error", "error", err, "count", len(batch))
		}
		batch = batch[:0]
	}

	for {
		select {
		case row, ok := <-s.buf:
			if !ok {
				flush()
				return
			}
			batch = append(batch, row)
			if len(batch) >= activityBatchMax {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

// batchInsert inserts rows using individual statements (no unnest for portability).
func (s *PGWorkstationActivityStore) batchInsert(ctx context.Context, rows []*store.WorkstationActivity) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO workstation_activity
		   (id, tenant_id, workstation_id, agent_id, action, cmd_hash, cmd_preview,
		    exit_code, duration_ms, deny_reason, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		 ON CONFLICT (id) DO NOTHING`,
	)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer stmt.Close()

	for _, r := range rows {
		if _, err := stmt.ExecContext(ctx,
			r.ID, r.TenantID, r.WorkstationID, r.AgentID, r.Action,
			r.CmdHash, r.CmdPreview, r.ExitCode, r.DurationMS, r.DenyReason, r.CreatedAt,
		); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// Stop drains the buffer and shuts down the flush goroutine.
func (s *PGWorkstationActivityStore) Stop() {
	close(s.buf)
	s.wg.Wait()
}
