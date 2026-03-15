package pg

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ============================================================
// Progress
// ============================================================

func (s *PGTeamStore) UpdateTaskProgress(ctx context.Context, taskID, teamID uuid.UUID, percent int, step string) error {
	if percent < 0 || percent > 100 {
		return fmt.Errorf("progress percent must be 0-100, got %d", percent)
	}
	// Also renews lock_expires_at as a heartbeat.
	now := time.Now()
	lockExpires := now.Add(taskLockDuration)
	res, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks SET progress_percent = $1, progress_step = $2, lock_expires_at = $3, updated_at = $4
		 WHERE id = $5 AND status = $6 AND team_id = $7`,
		percent, step, lockExpires, now,
		taskID, store.TeamTaskStatusInProgress, teamID,
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

// RenewTaskLock extends the lock expiration for an in-progress task.
// Called periodically by the consumer as a heartbeat to prevent
// the ticker from recovering a long-running task.
func (s *PGTeamStore) RenewTaskLock(ctx context.Context, taskID, teamID uuid.UUID) error {
	now := time.Now()
	lockExpires := now.Add(taskLockDuration)
	res, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks SET lock_expires_at = $1, updated_at = $2
		 WHERE id = $3 AND team_id = $4 AND status = $5`,
		lockExpires, now,
		taskID, teamID, store.TeamTaskStatusInProgress,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("task not in progress or not found")
	}
	return nil
}

// ============================================================
// Stale recovery
// ============================================================

func (s *PGTeamStore) RecoverStaleTasks(ctx context.Context, teamID uuid.UUID) (int, error) {
	now := time.Now()
	res, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks SET status = $1, owner_agent_id = NULL, locked_at = NULL, lock_expires_at = NULL,
		 followup_at = NULL, followup_count = 0, followup_message = NULL, followup_channel = NULL, followup_chat_id = NULL,
		 updated_at = $2
		 WHERE team_id = $3 AND status = $4 AND lock_expires_at IS NOT NULL AND lock_expires_at < $2`,
		store.TeamTaskStatusPending, now,
		teamID, store.TeamTaskStatusInProgress,
	)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

func (s *PGTeamStore) ForceRecoverAllTasks(ctx context.Context, teamID uuid.UUID) (int, error) {
	now := time.Now()
	res, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks SET status = $1, owner_agent_id = NULL, locked_at = NULL, lock_expires_at = NULL,
		 followup_at = NULL, followup_count = 0, followup_message = NULL, followup_channel = NULL, followup_chat_id = NULL,
		 updated_at = $2
		 WHERE team_id = $3 AND status = $4`,
		store.TeamTaskStatusPending, now,
		teamID, store.TeamTaskStatusInProgress,
	)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

func (s *PGTeamStore) ListRecoverableTasks(ctx context.Context, teamID uuid.UUID) ([]store.TeamTaskData, error) {
	now := time.Now()
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+taskSelectCols+`
		 `+taskJoinClause+`
		 WHERE t.team_id = $1
		   AND (
		     t.status = $2
		     OR (t.status = $3 AND t.lock_expires_at IS NOT NULL AND t.lock_expires_at < $4)
		   )
		 ORDER BY t.priority DESC, t.created_at
		 LIMIT $5`,
		teamID, store.TeamTaskStatusPending, store.TeamTaskStatusInProgress, now, maxListTasksRows)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTaskRowsJoined(rows)
}

func (s *PGTeamStore) MarkStaleTasks(ctx context.Context, teamID uuid.UUID, olderThan time.Time) (int, error) {
	now := time.Now()
	res, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks SET status = $1, updated_at = $2
		 WHERE team_id = $3 AND status = $4 AND updated_at < $5`,
		store.TeamTaskStatusStale, now,
		teamID, store.TeamTaskStatusPending, olderThan,
	)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

func (s *PGTeamStore) ResetTaskStatus(ctx context.Context, taskID, teamID uuid.UUID) error {
	now := time.Now()
	res, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks SET status = $1, locked_at = NULL, lock_expires_at = NULL, result = NULL, updated_at = $2
		 WHERE id = $3 AND team_id = $4 AND status IN ($5, $6)`,
		store.TeamTaskStatusPending, now,
		taskID, teamID, store.TeamTaskStatusStale, store.TeamTaskStatusFailed,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("task not available for reset (not stale/failed or wrong team)")
	}
	return nil
}
