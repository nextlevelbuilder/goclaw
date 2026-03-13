package pg

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ============================================================
// Tasks
// ============================================================

func (s *PGTeamStore) CreateTask(ctx context.Context, task *store.TeamTaskData) error {
	if task.ID == uuid.Nil {
		task.ID = store.GenNewID()
	}
	now := time.Now()
	task.CreatedAt = now
	task.UpdatedAt = now

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO team_tasks (id, team_id, subject, description, status, owner_agent_id, blocked_by, priority, result, user_id, channel, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		task.ID, task.TeamID, task.Subject, task.Description,
		task.Status, task.OwnerAgentID, pq.Array(task.BlockedBy),
		task.Priority, task.Result,
		sql.NullString{String: task.UserID, Valid: task.UserID != ""},
		sql.NullString{String: task.Channel, Valid: task.Channel != ""},
		now, now,
	)
	return err
}

func (s *PGTeamStore) UpdateTask(ctx context.Context, taskID uuid.UUID, updates map[string]any) error {
	if len(updates) == 0 {
		return nil
	}
	updates["updated_at"] = time.Now()
	return execMapUpdate(ctx, s.db, "team_tasks", taskID, updates)
}

func (s *PGTeamStore) ListTasks(ctx context.Context, teamID uuid.UUID, orderBy string, statusFilter string, userID string) ([]store.TeamTaskData, error) {
	orderClause := "t.priority DESC, t.created_at"
	if orderBy == "newest" {
		orderClause = "t.created_at DESC"
	}

	statusWhere := "AND t.status IN ('pending', 'pending_approval', 'in_progress', 'blocked')" // default: active only
	switch statusFilter {
	case store.TeamTaskFilterAll:
		statusWhere = ""
	case store.TeamTaskFilterCompleted:
		statusWhere = "AND t.status = 'completed'"
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT t.id, t.team_id, t.subject, t.description, t.status, t.owner_agent_id, t.blocked_by, t.priority, t.result, t.user_id, t.channel, t.created_at, t.updated_at,
		 COALESCE(a.agent_key, '') AS owner_agent_key
		 FROM team_tasks t
		 LEFT JOIN agents a ON a.id = t.owner_agent_id
		 WHERE t.team_id = $1 AND ($2 = '' OR t.user_id = $2) `+statusWhere+`
		 ORDER BY `+orderClause, teamID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTaskRowsJoined(rows)
}

func (s *PGTeamStore) GetTask(ctx context.Context, taskID uuid.UUID) (*store.TeamTaskData, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT t.id, t.team_id, t.subject, t.description, t.status, t.owner_agent_id, t.blocked_by, t.priority, t.result, t.user_id, t.channel, t.created_at, t.updated_at,
		 COALESCE(a.agent_key, '') AS owner_agent_key
		 FROM team_tasks t
		 LEFT JOIN agents a ON a.id = t.owner_agent_id
		 WHERE t.id = $1`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tasks, err := scanTaskRowsJoined(rows)
	if err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
		return nil, fmt.Errorf("task not found")
	}
	return &tasks[0], nil
}

func (s *PGTeamStore) SearchTasks(ctx context.Context, teamID uuid.UUID, query string, limit int, userID string) ([]store.TeamTaskData, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT t.id, t.team_id, t.subject, t.description, t.status, t.owner_agent_id, t.blocked_by, t.priority, t.result, t.user_id, t.channel, t.created_at, t.updated_at,
		 COALESCE(a.agent_key, '') AS owner_agent_key
		 FROM team_tasks t
		 LEFT JOIN agents a ON a.id = t.owner_agent_id
		 WHERE t.team_id = $1 AND t.tsv @@ plainto_tsquery('simple', $2) AND ($4 = '' OR t.user_id = $4)
		 ORDER BY ts_rank(t.tsv, plainto_tsquery('simple', $2)) DESC
		 LIMIT $3`, teamID, query, limit, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTaskRowsJoined(rows)
}

func (s *PGTeamStore) ClaimTask(ctx context.Context, taskID, agentID, teamID uuid.UUID) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks SET status = $1, owner_agent_id = $2, updated_at = $3
		 WHERE id = $4 AND status = $5 AND owner_agent_id IS NULL AND team_id = $6`,
		store.TeamTaskStatusInProgress, agentID, time.Now(),
		taskID, store.TeamTaskStatusPending, teamID,
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

func (s *PGTeamStore) CompleteTask(ctx context.Context, taskID, teamID uuid.UUID, result string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Mark task as completed (must be in_progress — use ClaimTask first)
	res, err := tx.ExecContext(ctx,
		`UPDATE team_tasks SET status = $1, result = $2, updated_at = $3
		 WHERE id = $4 AND status = $5 AND team_id = $6`,
		store.TeamTaskStatusCompleted, result, time.Now(),
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

	// Unblock dependent tasks: remove this taskID from their blocked_by arrays.
	// Tasks whose blocked_by becomes empty transition from blocked→pending so they can be claimed.
	_, err = tx.ExecContext(ctx,
		`UPDATE team_tasks SET
		   blocked_by = array_remove(blocked_by, $1),
		   status = CASE WHEN status = 'blocked' AND blocked_by = ARRAY[$1]::uuid[] THEN 'pending' ELSE status END,
		   updated_at = $2
		 WHERE $1 = ANY(blocked_by)`,
		taskID, time.Now(),
	)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (s *PGTeamStore) ApproveTask(ctx context.Context, taskID, teamID uuid.UUID) error {
	// Transition pending_approval → pending or blocked (if blockers exist).
	// Single atomic UPDATE: check blocked_by array to decide target status.
	res, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks SET
		   status = CASE WHEN array_length(blocked_by, 1) > 0 THEN $1 ELSE $2 END,
		   updated_at = $3
		 WHERE id = $4 AND status = $5 AND team_id = $6`,
		store.TeamTaskStatusBlocked, store.TeamTaskStatusPending, time.Now(),
		taskID, store.TeamTaskStatusPendingApproval, teamID,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("task not found, not pending approval, or wrong team")
	}
	return nil
}

func (s *PGTeamStore) CancelTask(ctx context.Context, taskID, teamID uuid.UUID, reason string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Cancel the task (guard: only non-completed tasks)
	now := time.Now()
	res, err := tx.ExecContext(ctx,
		`UPDATE team_tasks SET status = $1, result = $2, updated_at = $3
		 WHERE id = $4 AND status != $5 AND team_id = $6`,
		store.TeamTaskStatusCompleted, "CANCELLED: "+reason, now,
		taskID, store.TeamTaskStatusCompleted, teamID,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("task not found, already completed, or wrong team")
	}

	// Unblock dependent tasks (same logic as CompleteTask)
	_, err = tx.ExecContext(ctx,
		`UPDATE team_tasks SET
		   blocked_by = array_remove(blocked_by, $1),
		   status = CASE WHEN status = 'blocked' AND blocked_by = ARRAY[$1]::uuid[] THEN 'pending' ELSE status END,
		   updated_at = $2
		 WHERE $1 = ANY(blocked_by)`,
		taskID, now,
	)
	if err != nil {
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
	res, err := tx.ExecContext(ctx,
		`UPDATE team_tasks SET status = $1, result = $2, updated_at = $3
		 WHERE id = $4 AND status = $5 AND team_id = $6`,
		store.TeamTaskStatusFailed, "FAILED: "+errMsg, now,
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

	// Unblock dependent tasks (same logic as CompleteTask/CancelTask)
	_, err = tx.ExecContext(ctx,
		`UPDATE team_tasks SET
		   blocked_by = array_remove(blocked_by, $1),
		   status = CASE WHEN status = 'blocked' AND blocked_by = ARRAY[$1]::uuid[] THEN 'pending' ELSE status END,
		   updated_at = $2
		 WHERE $1 = ANY(blocked_by)`,
		taskID, now,
	)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func scanTaskRowsJoined(rows *sql.Rows) ([]store.TeamTaskData, error) {
	var tasks []store.TeamTaskData
	for rows.Next() {
		var d store.TeamTaskData
		var desc, result, userID, channel sql.NullString
		var ownerID *uuid.UUID
		var blockedBy []uuid.UUID
		if err := rows.Scan(
			&d.ID, &d.TeamID, &d.Subject, &desc, &d.Status,
			&ownerID, pq.Array(&blockedBy), &d.Priority, &result,
			&userID, &channel,
			&d.CreatedAt, &d.UpdatedAt,
			&d.OwnerAgentKey,
		); err != nil {
			return nil, err
		}
		if desc.Valid {
			d.Description = desc.String
		}
		if result.Valid {
			d.Result = &result.String
		}
		if userID.Valid {
			d.UserID = userID.String
		}
		if channel.Valid {
			d.Channel = channel.String
		}
		d.OwnerAgentID = ownerID
		d.BlockedBy = blockedBy
		tasks = append(tasks, d)
	}
	return tasks, rows.Err()
}
