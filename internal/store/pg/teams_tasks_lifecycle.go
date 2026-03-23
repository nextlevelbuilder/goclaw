package pg

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func (s *PGTeamStore) ClaimTask(ctx context.Context, taskID, agentID, teamID uuid.UUID) error {
	now := time.Now()
	lockExpires := now.Add(taskLockDuration)
	tid := tenantIDForInsert(ctx)
	res, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks SET status = $1, owner_agent_id = $2, locked_at = $3, lock_expires_at = $4, updated_at = $3
		 WHERE id = $5 AND status = $6 AND owner_agent_id IS NULL AND team_id = $7 AND tenant_id = $8`,
		store.TeamTaskStatusInProgress, agentID, now, lockExpires,
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

func (s *PGTeamStore) AssignTask(ctx context.Context, taskID, agentID, teamID uuid.UUID) error {
	now := time.Now()
	lockExpires := now.Add(taskLockDuration)
	tid := tenantIDForInsert(ctx)
	res, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks SET status = $1, owner_agent_id = $2, locked_at = $3, lock_expires_at = $4, updated_at = $3
		 WHERE id = $5 AND team_id = $6 AND status = $7 AND tenant_id = $8`,
		store.TeamTaskStatusInProgress, agentID, now, lockExpires,
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

func (s *PGTeamStore) CompleteTask(ctx context.Context, taskID, teamID uuid.UUID, result string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	tid := tenantIDForInsert(ctx)
	res, err := tx.ExecContext(ctx,
		`UPDATE team_tasks SET status = $1, result = $2, locked_at = NULL, lock_expires_at = NULL,
		 followup_at = NULL, followup_count = 0, followup_message = NULL, followup_channel = NULL, followup_chat_id = NULL,
		 progress_percent = NULL,
		 updated_at = $3
		 WHERE id = $4 AND status = $5 AND team_id = $6 AND tenant_id = $7`,
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

	if err := unblockDependentTasks(ctx, tx, taskID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *PGTeamStore) CancelTask(ctx context.Context, taskID, teamID uuid.UUID, reason string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now()
	tid := tenantIDForInsert(ctx)
	res, err := tx.ExecContext(ctx,
		`UPDATE team_tasks SET status = $1, result = $2, locked_at = NULL, lock_expires_at = NULL,
		 followup_at = NULL, followup_count = 0, followup_message = NULL, followup_channel = NULL, followup_chat_id = NULL,
		 progress_percent = NULL,
		 updated_at = $3
		 WHERE id = $4 AND status NOT IN ($5, $1) AND team_id = $6 AND tenant_id = $7`,
		store.TeamTaskStatusCancelled, reason, now,
		taskID, store.TeamTaskStatusCompleted, teamID, tid,
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

	if err := unblockDependentTasks(ctx, tx, taskID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *PGTeamStore) FailTask(ctx context.Context, taskID, teamID uuid.UUID, errMsg string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now()
	tid := tenantIDForInsert(ctx)
	res, err := tx.ExecContext(ctx,
		`UPDATE team_tasks SET status = $1, result = $2, locked_at = NULL, lock_expires_at = NULL,
		 followup_at = NULL, followup_count = 0, followup_message = NULL, followup_channel = NULL, followup_chat_id = NULL,
		 progress_percent = NULL,
		 updated_at = $3
		 WHERE id = $4 AND status = $5 AND team_id = $6 AND tenant_id = $7`,
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

	if err := unblockDependentTasks(ctx, tx, taskID); err != nil {
		return err
	}
	return tx.Commit()
}

// FailPendingTask marks a pending or blocked task as failed (post-turn validation).
func (s *PGTeamStore) FailPendingTask(ctx context.Context, taskID, teamID uuid.UUID, errMsg string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now()
	tid := tenantIDForInsert(ctx)
	res, err := tx.ExecContext(ctx,
		`UPDATE team_tasks SET status = $1, result = $2, locked_at = NULL, lock_expires_at = NULL,
		 progress_percent = NULL, updated_at = $3
		 WHERE id = $4 AND status IN ($5, $6) AND team_id = $7 AND tenant_id = $8`,
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

	if err := unblockDependentTasks(ctx, tx, taskID); err != nil {
		return err
	}
	return tx.Commit()
}

// unblockDependentTasks removes taskID from blocked_by arrays and transitions blocked→pending
// when all blockers are resolved. Must be called within a transaction.
// Scoped by tenant_id to prevent cross-tenant unblocking.
func unblockDependentTasks(ctx context.Context, tx *sql.Tx, taskID uuid.UUID) error {
	tid := tenantIDForInsert(ctx)
	_, err := tx.ExecContext(ctx,
		`UPDATE team_tasks SET
		   blocked_by = array_remove(blocked_by, $1),
		   status = CASE WHEN status = 'blocked' AND array_length(array_remove(blocked_by, $1), 1) IS NULL THEN 'pending' ELSE status END,
		   updated_at = $2
		 WHERE $1 = ANY(blocked_by) AND tenant_id = $3`,
		taskID, time.Now(), tid,
	)
	return err
}

func (s *PGTeamStore) ReviewTask(ctx context.Context, taskID, teamID uuid.UUID) error {
	tid := tenantIDForInsert(ctx)
	res, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks SET status = $1, updated_at = $2
		 WHERE id = $3 AND status = $4 AND team_id = $5 AND tenant_id = $6`,
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

func (s *PGTeamStore) ApproveTask(ctx context.Context, taskID, teamID uuid.UUID, comment string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now()
	tid := tenantIDForInsert(ctx)
	res, err := tx.ExecContext(ctx,
		`UPDATE team_tasks SET status = $1, locked_at = NULL, lock_expires_at = NULL,
		 followup_at = NULL, followup_count = 0, followup_message = NULL, followup_channel = NULL, followup_chat_id = NULL,
		 progress_percent = NULL,
		 updated_at = $2
		 WHERE id = $3 AND status = $4 AND team_id = $5 AND tenant_id = $6`,
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

	if err := unblockDependentTasks(ctx, tx, taskID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *PGTeamStore) RejectTask(ctx context.Context, taskID, teamID uuid.UUID, reason string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now()
	tid := tenantIDForInsert(ctx)
	res, err := tx.ExecContext(ctx,
		`UPDATE team_tasks SET status = $1, result = $2, locked_at = NULL, lock_expires_at = NULL,
		 followup_at = NULL, followup_count = 0, followup_message = NULL, followup_channel = NULL, followup_chat_id = NULL,
		 progress_percent = NULL,
		 updated_at = $3
		 WHERE id = $4 AND status = $5 AND team_id = $6 AND tenant_id = $7`,
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

	if err := unblockDependentTasks(ctx, tx, taskID); err != nil {
		return err
	}
	return tx.Commit()
}
