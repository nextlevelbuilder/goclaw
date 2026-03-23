package pg

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ============================================================
// Follow-up reminders
// ============================================================

func (s *PGTeamStore) SetTaskFollowup(ctx context.Context, taskID, teamID uuid.UUID, followupAt time.Time, max int, message, channel, chatID string) error {
	now := time.Now()
	tid := tenantIDForInsert(ctx)
	res, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks SET followup_at = $1, followup_max = $2, followup_message = $3, followup_channel = $4, followup_chat_id = $5, updated_at = $6
		 WHERE id = $7 AND team_id = $8 AND status = $9 AND tenant_id = $10`,
		followupAt, max, message, channel, chatID, now,
		taskID, teamID, store.TeamTaskStatusInProgress, tid,
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

func (s *PGTeamStore) ClearTaskFollowup(ctx context.Context, taskID uuid.UUID) error {
	tid := tenantIDForInsert(ctx)
	_, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks SET followup_at = NULL, followup_count = 0, followup_message = NULL, followup_channel = NULL, followup_chat_id = NULL, updated_at = $1
		 WHERE id = $2 AND tenant_id = $3`,
		time.Now(), taskID, tid,
	)
	return err
}

// ListAllFollowupDueTasks returns due followup tasks across all v2 active teams (batch).
func (s *PGTeamStore) ListAllFollowupDueTasks(ctx context.Context) ([]store.TeamTaskData, error) {
	now := time.Now()
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+taskSelectCols+`
		 `+taskJoinClause+`
		 `+v2ActiveTeamJoin+`
		 WHERE t.followup_at IS NOT NULL
		   AND t.followup_at <= $1
		   AND t.status = $2
		   AND (t.followup_max = 0 OR t.followup_count < t.followup_max)
		 ORDER BY t.followup_at
		 LIMIT 100`,
		now, store.TeamTaskStatusInProgress,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTaskRowsJoined(rows)
}

func (s *PGTeamStore) IncrementFollowupCount(ctx context.Context, taskID uuid.UUID, nextAt *time.Time) error {
	tid := tenantIDForInsert(ctx)
	_, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks SET followup_count = followup_count + 1, followup_at = $1, updated_at = $2
		 WHERE id = $3 AND tenant_id = $4`,
		nextAt, time.Now(), taskID, tid,
	)
	return err
}

func (s *PGTeamStore) ClearFollowupByScope(ctx context.Context, channel, chatID string) (int, error) {
	tid := tenantIDForInsert(ctx)
	res, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks
		 SET followup_at = NULL, followup_count = 0, followup_message = NULL,
		     followup_channel = NULL, followup_chat_id = NULL, updated_at = NOW()
		 WHERE followup_channel = $1 AND followup_chat_id = $2
		   AND followup_at IS NOT NULL AND status = $3 AND tenant_id = $4`,
		channel, chatID, store.TeamTaskStatusInProgress, tid,
	)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

func (s *PGTeamStore) SetFollowupForActiveTasks(ctx context.Context, teamID uuid.UUID, channel, chatID string, followupAt time.Time, max int, message string) (int, error) {
	tid := tenantIDForInsert(ctx)
	res, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks
		 SET followup_at = $4, followup_max = $5, followup_message = $6,
		     followup_channel = $2, followup_chat_id = $3, updated_at = NOW()
		 WHERE team_id = $1
		   AND status = $7
		   AND followup_at IS NULL
		   AND tenant_id = $8
		   AND (
		     (COALESCE(channel,'') = $2 AND COALESCE(chat_id,'') = $3)
		     OR followup_channel = $2
		     OR (COALESCE(channel,'') IN ('', 'system', 'delegate') AND COALESCE(chat_id,'') = '')
		   )`,
		teamID, channel, chatID, followupAt, max, message, store.TeamTaskStatusInProgress, tid,
	)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

func (s *PGTeamStore) HasActiveMemberTasks(ctx context.Context, teamID uuid.UUID, excludeAgentID uuid.UUID) (bool, error) {
	tid := tenantIDForInsert(ctx)
	var exists bool
	err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS(
			SELECT 1 FROM team_tasks
			WHERE team_id = $1
			  AND owner_agent_id IS NOT NULL
			  AND owner_agent_id != $2
			  AND status IN ($3, $4, $5)
			  AND tenant_id = $6
		)`,
		teamID, excludeAgentID,
		store.TeamTaskStatusPending, store.TeamTaskStatusInProgress, store.TeamTaskStatusBlocked, tid,
	).Scan(&exists)
	return exists, err
}
