//go:build sqlite || sqliteonly

package sqlitestore

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
	sqliteActivityBufferSize  = 500
	sqliteActivityBatchMax    = 50
	sqliteActivityFlushPeriod = 500 * time.Millisecond
)

// SQLiteWorkstationActivityStore implements store.WorkstationActivityStore backed by SQLite.
// Uses the same buffered-flush pattern as the PG implementation, with smaller buffer
// (SQLite write throughput is lower than PG in concurrent scenarios).
type SQLiteWorkstationActivityStore struct {
	db  *sql.DB
	buf chan *store.WorkstationActivity
	wg  sync.WaitGroup
}

// NewSQLiteWorkstationActivityStore creates the store and starts the background flusher.
func NewSQLiteWorkstationActivityStore(db *sql.DB) *SQLiteWorkstationActivityStore {
	s := &SQLiteWorkstationActivityStore{
		db:  db,
		buf: make(chan *store.WorkstationActivity, sqliteActivityBufferSize),
	}
	s.wg.Add(1)
	go s.flusher()
	return s
}

// Insert enqueues the row; drops and warns if buffer full.
func (s *SQLiteWorkstationActivityStore) Insert(_ context.Context, row *store.WorkstationActivity) error {
	select {
	case s.buf <- row:
	default:
		slog.Warn("workstation.activity.buffer_full", "action", row.Action)
	}
	return nil
}

// List returns up to limit rows for the workstation, newest first.
func (s *SQLiteWorkstationActivityStore) List(ctx context.Context, workstationID uuid.UUID, limit int, cursor *uuid.UUID) ([]store.WorkstationActivity, *uuid.UUID, error) {
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
			 WHERE workstation_id = ?
			 ORDER BY created_at DESC
			 LIMIT ?`,
			workstationID.String(), limit+1,
		)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, tenant_id, workstation_id, agent_id, action, cmd_hash, cmd_preview,
			        exit_code, duration_ms, deny_reason, created_at
			 FROM workstation_activity
			 WHERE workstation_id = ?
			   AND created_at < (SELECT created_at FROM workstation_activity WHERE id = ?)
			 ORDER BY created_at DESC
			 LIMIT ?`,
			workstationID.String(), cursor.String(), limit+1,
		)
	}
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var result []store.WorkstationActivity
	for rows.Next() {
		var a store.WorkstationActivity
		var idStr, tenantStr, wsStr string
		var createdAtStr string
		if err := rows.Scan(
			&idStr, &tenantStr, &wsStr, &a.AgentID, &a.Action,
			&a.CmdHash, &a.CmdPreview, &a.ExitCode, &a.DurationMS, &a.DenyReason, &createdAtStr,
		); err != nil {
			return nil, nil, err
		}
		a.ID, _ = uuid.Parse(idStr)
		a.TenantID, _ = uuid.Parse(tenantStr)
		a.WorkstationID, _ = uuid.Parse(wsStr)
		a.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAtStr)
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

// Prune deletes rows older than before in batches.
func (s *SQLiteWorkstationActivityStore) Prune(ctx context.Context, before time.Time) (int64, error) {
	var total int64
	ts := before.UTC().Format(time.RFC3339Nano)
	for {
		res, err := s.db.ExecContext(ctx,
			`DELETE FROM workstation_activity
			 WHERE id IN (
			   SELECT id FROM workstation_activity WHERE created_at < ? LIMIT 1000
			 )`,
			ts,
		)
		if err != nil {
			return total, err
		}
		n, _ := res.RowsAffected()
		total += n
		if n < 1000 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	return total, nil
}

// flusher batches inserts from buf every 500ms or 50 rows.
func (s *SQLiteWorkstationActivityStore) flusher() {
	defer s.wg.Done()
	ticker := time.NewTicker(sqliteActivityFlushPeriod)
	defer ticker.Stop()

	var batch []*store.WorkstationActivity
	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := s.insertBatch(context.Background(), batch); err != nil {
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
			if len(batch) >= sqliteActivityBatchMax {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

// insertBatch writes rows in a single transaction.
func (s *SQLiteWorkstationActivityStore) insertBatch(ctx context.Context, rows []*store.WorkstationActivity) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx,
		`INSERT OR IGNORE INTO workstation_activity
		   (id, tenant_id, workstation_id, agent_id, action, cmd_hash, cmd_preview,
		    exit_code, duration_ms, deny_reason, created_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
	)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer stmt.Close()

	for _, r := range rows {
		ts := r.CreatedAt.UTC().Format(time.RFC3339Nano)
		if _, err := stmt.ExecContext(ctx,
			r.ID.String(), r.TenantID.String(), r.WorkstationID.String(),
			r.AgentID, r.Action, r.CmdHash, r.CmdPreview,
			r.ExitCode, r.DurationMS, r.DenyReason, ts,
		); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// Stop drains the buffer and shuts down the flusher goroutine.
func (s *SQLiteWorkstationActivityStore) Stop() {
	close(s.buf)
	s.wg.Wait()
}
