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

func (s *SQLiteTeamStore) ClaimTask(ctx context.Context, taskID, agentID, teamID uuid.UUID) error {
	now := time.Now()
	lockExpires := now.Add(taskLockDuration)
	tid := tenantIDForInsert(ctx)
	res, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks SET status = ?, owner_agent_id = ?, locked_at = ?, lock_expires_at = ?, updated_at = ?
		 WHERE id = ? AND status = ? AND owner_agent_id IS NULL AND team_id = ? AND tenant_id = ?`,
		store.TeamTaskStatusInProgress, agentID, now, lockExpires, now,
		taskID, store.TeamTaskStatusPending, teamID, tid,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("task not available for claiming (already claimed or not pending)")
	}
	return nil
}

func (s *SQLiteTeamStore) AssignTask(ctx context.Context, taskID, agentID, teamID uuid.UUID) error {
	now := time.Now()
	lockExpires := now.Add(taskLockDuration)
	tid := tenantIDForInsert(ctx)
	res, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks SET status = ?, owner_agent_id = ?, locked_at = ?, lock_expires_at = ?, updated_at = ?
		 WHERE id = ? AND team_id = ? AND status = ? AND tenant_id = ?`,
		store.TeamTaskStatusInProgress, agentID, now, lockExpires, now,
		taskID, teamID, store.TeamTaskStatusPending, tid,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("task not available for assignment (not pending or wrong team)")
	}
	return nil
}

func (s *SQLiteTeamStore) CompleteTask(ctx context.Context, taskID, teamID uuid.UUID, result string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	tid := tenantIDForInsert(ctx)
	res, err := tx.ExecContext(ctx,
		`UPDATE team_tasks SET status = ?, result = ?, locked_at = NULL, lock_expires_at = NULL,
		 followup_at = NULL, followup_count = 0, followup_message = NULL, followup_channel = NULL, followup_chat_id = NULL,
		 progress_percent = NULL, updated_at = ?
		 WHERE id = ? AND status = ? AND team_id = ? AND tenant_id = ?`,
		store.TeamTaskStatusCompleted, result, time.Now(),
		taskID, store.TeamTaskStatusInProgress, teamID, tid,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("task not in progress or not found")
	}

	if err := unblockDependentTasksSQLite(ctx, tx, taskID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteTeamStore) CancelTask(ctx context.Context, taskID, teamID uuid.UUID, reason string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now()
	tid := tenantIDForInsert(ctx)
	res, err := tx.ExecContext(ctx,
		`UPDATE team_tasks SET status = ?, result = ?, locked_at = NULL, lock_expires_at = NULL,
		 followup_at = NULL, followup_count = 0, followup_message = NULL, followup_channel = NULL, followup_chat_id = NULL,
		 progress_percent = NULL, updated_at = ?
		 WHERE id = ? AND status NOT IN (?, ?) AND team_id = ? AND tenant_id = ?`,
		store.TeamTaskStatusCancelled, reason, now,
		taskID, store.TeamTaskStatusCompleted, store.TeamTaskStatusCancelled, teamID, tid,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("task not found, already completed/cancelled, or wrong team")
	}

	if err := unblockDependentTasksSQLite(ctx, tx, taskID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteTeamStore) FailTask(ctx context.Context, taskID, teamID uuid.UUID, errMsg string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now()
	tid := tenantIDForInsert(ctx)
	res, err := tx.ExecContext(ctx,
		`UPDATE team_tasks SET status = ?, result = ?, locked_at = NULL, lock_expires_at = NULL,
		 followup_at = NULL, followup_count = 0, followup_message = NULL, followup_channel = NULL, followup_chat_id = NULL,
		 progress_percent = NULL, updated_at = ?
		 WHERE id = ? AND status = ? AND team_id = ? AND tenant_id = ?`,
		store.TeamTaskStatusFailed, "FAILED: "+errMsg, now,
		taskID, store.TeamTaskStatusInProgress, teamID, tid,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("task not in progress or not found")
	}

	if err := unblockDependentTasksSQLite(ctx, tx, taskID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteTeamStore) FailPendingTask(ctx context.Context, taskID, teamID uuid.UUID, errMsg string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now()
	tid := tenantIDForInsert(ctx)
	res, err := tx.ExecContext(ctx,
		`UPDATE team_tasks SET status = ?, result = ?, locked_at = NULL, lock_expires_at = NULL,
		 progress_percent = NULL, updated_at = ?
		 WHERE id = ? AND status IN (?, ?) AND team_id = ? AND tenant_id = ?`,
		store.TeamTaskStatusFailed, "FAILED: "+errMsg, now,
		taskID, store.TeamTaskStatusPending, store.TeamTaskStatusBlocked, teamID, tid,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("task not pending/blocked or not found")
	}

	if err := unblockDependentTasksSQLite(ctx, tx, taskID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteTeamStore) ReviewTask(ctx context.Context, taskID, teamID uuid.UUID) error {
	tid := tenantIDForInsert(ctx)
	res, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks SET status = ?, updated_at = ?
		 WHERE id = ? AND status = ? AND team_id = ? AND tenant_id = ?`,
		store.TeamTaskStatusInReview, time.Now(),
		taskID, store.TeamTaskStatusInProgress, teamID, tid,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("task not in progress or not found")
	}
	return nil
}

func (s *SQLiteTeamStore) ApproveTask(ctx context.Context, taskID, teamID uuid.UUID, comment string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now()
	tid := tenantIDForInsert(ctx)
	res, err := tx.ExecContext(ctx,
		`UPDATE team_tasks SET status = ?, locked_at = NULL, lock_expires_at = NULL,
		 followup_at = NULL, followup_count = 0, followup_message = NULL, followup_channel = NULL, followup_chat_id = NULL,
		 progress_percent = NULL, updated_at = ?
		 WHERE id = ? AND status = ? AND team_id = ? AND tenant_id = ?`,
		store.TeamTaskStatusCompleted, now,
		taskID, store.TeamTaskStatusInReview, teamID, tid,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("task not in review or not found")
	}

	if err := unblockDependentTasksSQLite(ctx, tx, taskID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteTeamStore) RejectTask(ctx context.Context, taskID, teamID uuid.UUID, reason string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now()
	tid := tenantIDForInsert(ctx)
	res, err := tx.ExecContext(ctx,
		`UPDATE team_tasks SET status = ?, result = ?, locked_at = NULL, lock_expires_at = NULL,
		 followup_at = NULL, followup_count = 0, followup_message = NULL, followup_channel = NULL, followup_chat_id = NULL,
		 progress_percent = NULL, updated_at = ?
		 WHERE id = ? AND status = ? AND team_id = ? AND tenant_id = ?`,
		store.TeamTaskStatusCancelled, reason, now,
		taskID, store.TeamTaskStatusInReview, teamID, tid,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("task not in review or not found")
	}

	if err := unblockDependentTasksSQLite(ctx, tx, taskID); err != nil {
		return err
	}
	return tx.Commit()
}

// unblockDependentTasksSQLite removes taskID from blocked_by JSON arrays and
// transitions blocked→pending when all blockers are resolved.
// SQLite has no array_remove — we do it in Go: fetch affected rows, update in-process.
func unblockDependentTasksSQLite(ctx context.Context, tx *sql.Tx, taskID uuid.UUID) error {
	tid := tenantIDForInsert(ctx)
	taskIDStr := taskID.String()

	// Find all blocked tasks that reference this taskID in their blocked_by JSON array.
	rows, err := tx.QueryContext(ctx,
		`SELECT id, blocked_by FROM team_tasks
		 WHERE status IN ('blocked', 'pending') AND tenant_id = ?
		   AND EXISTS (SELECT 1 FROM json_each(blocked_by) WHERE json_each.value = ?)`,
		tid, taskIDStr,
	)
	if err != nil {
		return err
	}

	type toUpdate struct {
		id        uuid.UUID
		blockedBy []string
	}
	var pending []toUpdate
	for rows.Next() {
		var id uuid.UUID
		var rawJSON []byte
		if err := rows.Scan(&id, &rawJSON); err != nil {
			rows.Close()
			return err
		}
		var blockedBy []string
		if len(rawJSON) > 0 {
			_ = json.Unmarshal(rawJSON, &blockedBy)
		}
		// Remove taskID from the slice.
		filtered := blockedBy[:0]
		for _, s := range blockedBy {
			if s != taskIDStr {
				filtered = append(filtered, s)
			}
		}
		pending = append(pending, toUpdate{id: id, blockedBy: filtered})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	now := time.Now()
	for _, u := range pending {
		newJSON := jsonStringArray(u.blockedBy)
		newStatus := "pending"
		if len(u.blockedBy) > 0 {
			newStatus = "blocked"
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE team_tasks SET blocked_by = ?, status = ?, updated_at = ? WHERE id = ? AND tenant_id = ?`,
			newJSON, newStatus, now, u.id, tid,
		); err != nil {
			return err
		}
	}
	return nil
}
