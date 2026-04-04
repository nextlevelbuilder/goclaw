package pg

import (
	"context"
	"fmt"
	"log/slog"
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
	tid := tenantIDForInsert(ctx)
	res, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks SET progress_percent = $1, progress_step = $2, lock_expires_at = $3, updated_at = $4
		 WHERE id = $5 AND status = $6 AND team_id = $7 AND tenant_id = $8`,
		percent, step, lockExpires, now,
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
		// Diagnostic: log actual status to help trace why the task is not in_progress.
		if diag, _ := s.GetTask(ctx, taskID); diag != nil {
			slog.Warn("UpdateTaskProgress: status mismatch", "task_id", taskID,
				"expected", "in_progress", "actual", diag.Status, "lock_expires", diag.LockExpiresAt)
		}
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
	tid := tenantIDForInsert(ctx)
	res, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks SET lock_expires_at = $1, updated_at = $2
		 WHERE id = $3 AND team_id = $4 AND status = $5 AND tenant_id = $6`,
		lockExpires, now,
		taskID, teamID, store.TeamTaskStatusInProgress, tid,
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
// Stale recovery (batch — all v2 active teams in one query)
// ============================================================

// v2ActiveTeamJoin is the JOIN clause that filters to v2 active teams.
const v2ActiveTeamJoin = `JOIN agent_teams tm ON tm.id = t.team_id
		 AND tm.status = 'active'
		 AND COALESCE((tm.settings->>'version')::int, 0) >= 2`

// RecoverAllStaleTasks resets in_progress tasks with expired locks across all v2 active teams.
func (s *PGTeamStore) RecoverAllStaleTasks(ctx context.Context) ([]store.RecoveredTaskInfo, error) {
	now := time.Now()
	rows, err := s.db.QueryContext(ctx,
		`UPDATE team_tasks t
		 SET status = $1, locked_at = NULL, lock_expires_at = NULL,
		     followup_at = NULL, followup_count = 0, followup_message = NULL,
		     followup_channel = NULL, followup_chat_id = NULL, updated_at = $2
		 FROM agent_teams tm
		 WHERE t.team_id = tm.id AND tm.status = 'active'
		   AND COALESCE((tm.settings->>'version')::int, 0) >= 2
		   AND t.status = $3
		   AND t.lock_expires_at IS NOT NULL AND t.lock_expires_at < $2
		 RETURNING t.id, t.team_id, t.tenant_id, t.task_number, t.subject, COALESCE(t.channel, ''), COALESCE(t.chat_id, '')`,
		store.TeamTaskStatusPending, now, store.TeamTaskStatusInProgress,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRecoveredTaskInfoRows(rows)
}

// ForceRecoverAllTasks resets ALL in_progress tasks across v2 active teams (startup).
func (s *PGTeamStore) ForceRecoverAllTasks(ctx context.Context) ([]store.RecoveredTaskInfo, error) {
	now := time.Now()
	rows, err := s.db.QueryContext(ctx,
		`UPDATE team_tasks t
		 SET status = $1, locked_at = NULL, lock_expires_at = NULL,
		     followup_at = NULL, followup_count = 0, followup_message = NULL,
		     followup_channel = NULL, followup_chat_id = NULL, updated_at = $2
		 FROM agent_teams tm
		 WHERE t.team_id = tm.id AND tm.status = 'active'
		   AND COALESCE((tm.settings->>'version')::int, 0) >= 2
		   AND t.status = $3
		 RETURNING t.id, t.team_id, t.tenant_id, t.task_number, t.subject, COALESCE(t.channel, ''), COALESCE(t.chat_id, '')`,
		store.TeamTaskStatusPending, now, store.TeamTaskStatusInProgress,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRecoveredTaskInfoRows(rows)
}

// ListRecoverableTasks returns pending + stale-locked tasks for a single team.
// Used by DispatchUnblockedTasks after task completion.
func (s *PGTeamStore) ListRecoverableTasks(ctx context.Context, teamID uuid.UUID) ([]store.TeamTaskData, error) {
	now := time.Now()
	tid := tenantIDForInsert(ctx)
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+taskSelectCols+`
		 `+taskJoinClause+`
		 WHERE t.team_id = $1
		   AND t.tenant_id = $6
		   AND (
		     t.status = $2
		     OR (t.status = $3 AND t.lock_expires_at IS NOT NULL AND t.lock_expires_at < $4)
		   )
		 ORDER BY t.priority DESC, t.created_at
		 LIMIT $5`,
		teamID, store.TeamTaskStatusPending, store.TeamTaskStatusInProgress, now, maxListTasksRows, tid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTaskRowsJoined(rows)
}

// MarkAllStaleTasks marks pending tasks older than olderThan as stale across all v2 active teams.
func (s *PGTeamStore) MarkAllStaleTasks(ctx context.Context, olderThan time.Time) ([]store.RecoveredTaskInfo, error) {
	now := time.Now()
	rows, err := s.db.QueryContext(ctx,
		`UPDATE team_tasks t
		 SET status = $1, updated_at = $2
		 FROM agent_teams tm
		 WHERE t.team_id = tm.id AND tm.status = 'active'
		   AND COALESCE((tm.settings->>'version')::int, 0) >= 2
		   AND t.status = $3 AND t.updated_at < $4
		 RETURNING t.id, t.team_id, t.tenant_id, t.task_number, t.subject, COALESCE(t.channel, ''), COALESCE(t.chat_id, '')`,
		store.TeamTaskStatusStale, now, store.TeamTaskStatusPending, olderThan,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRecoveredTaskInfoRows(rows)
}

// MarkInReviewStaleTasks marks in_review tasks older than olderThan as stale across all v2 active teams.
func (s *PGTeamStore) MarkInReviewStaleTasks(ctx context.Context, olderThan time.Time) ([]store.RecoveredTaskInfo, error) {
	now := time.Now()
	rows, err := s.db.QueryContext(ctx,
		`UPDATE team_tasks t
		 SET status = $1, updated_at = $2
		 FROM agent_teams tm
		 WHERE t.team_id = tm.id AND tm.status = 'active'
		   AND COALESCE((tm.settings->>'version')::int, 0) >= 2
		   AND t.status = $3 AND t.updated_at < $4
		 RETURNING t.id, t.team_id, t.tenant_id, t.task_number, t.subject, COALESCE(t.channel, ''), COALESCE(t.chat_id, '')`,
		store.TeamTaskStatusStale, now, store.TeamTaskStatusInReview, olderThan,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRecoveredTaskInfoRows(rows)
}

// FixOrphanedBlockedTasks unblocks blocked tasks where all blockers have reached terminal status.
// Safety net for cases where unblockDependentTasks() transaction rolled back.
func (s *PGTeamStore) FixOrphanedBlockedTasks(ctx context.Context) ([]store.RecoveredTaskInfo, error) {
	now := time.Now()
	rows, err := s.db.QueryContext(ctx,
		`UPDATE team_tasks t
		 SET blocked_by = '{}', status = $1, updated_at = $2
		 FROM agent_teams tm
		 WHERE t.team_id = tm.id AND tm.status = 'active'
		   AND COALESCE((tm.settings->>'version')::int, 0) >= 2
		   AND t.status = 'blocked'
		   AND array_length(t.blocked_by, 1) > 0
		   AND NOT EXISTS (
		     SELECT 1 FROM unnest(t.blocked_by) AS bid(id)
		     JOIN team_tasks bt ON bt.id = bid.id AND bt.tenant_id = t.tenant_id
		     WHERE bt.status NOT IN ($3, $4, $5)
		   )
		 RETURNING t.id, t.team_id, t.tenant_id, t.task_number, t.subject, COALESCE(t.channel, ''), COALESCE(t.chat_id, '')`,
		store.TeamTaskStatusPending, now,
		store.TeamTaskStatusCompleted, store.TeamTaskStatusFailed, store.TeamTaskStatusCancelled,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRecoveredTaskInfoRows(rows)
}

func scanRecoveredTaskInfoRows(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]store.RecoveredTaskInfo, error) {
	var out []store.RecoveredTaskInfo
	for rows.Next() {
		var info store.RecoveredTaskInfo
		if err := rows.Scan(&info.ID, &info.TeamID, &info.TenantID, &info.TaskNumber, &info.Subject, &info.Channel, &info.ChatID); err != nil {
			return nil, err
		}
		out = append(out, info)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *PGTeamStore) ResetTaskStatus(ctx context.Context, taskID, teamID uuid.UUID) error {
	now := time.Now()
	tid := tenantIDForInsert(ctx)
	res, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks SET status = $1, locked_at = NULL, lock_expires_at = NULL, result = NULL,
		 progress_percent = NULL, progress_step = NULL, updated_at = $2
		 WHERE id = $3 AND team_id = $4 AND status IN ($5, $6, $7, $8) AND tenant_id = $9`,
		store.TeamTaskStatusPending, now,
		taskID, teamID, store.TeamTaskStatusStale, store.TeamTaskStatusFailed,
		store.TeamTaskStatusCancelled, store.TeamTaskStatusInReview, tid,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("task not available for reset (not stale/failed/cancelled/in_review or wrong team)")
	}
	return nil
}
