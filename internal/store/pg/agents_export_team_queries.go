package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/google/uuid"
)

// Team export types — used exclusively by the agent export pipeline.

// TeamExport holds portable team metadata.
type TeamExport struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Status      string          `json:"status"`
	Settings    json.RawMessage `json:"settings,omitempty"`
}

// TeamMemberExport references a team member by agent_key (portable cross-system).
type TeamMemberExport struct {
	AgentKey string `json:"agent_key"`
	Role     string `json:"role"`
}

// TeamTaskExport is a portable team task (no internal UUIDs).
// Note: blocked_by (UUID[]) is intentionally omitted — task UUIDs change on import,
// so the dependency graph would be invalid. Tasks are imported with cleared blocked_by.
type TeamTaskExport struct {
	Subject         string          `json:"subject"`
	Description     string          `json:"description,omitempty"`
	Status          string          `json:"status"`
	Priority        int             `json:"priority"`
	Result          *string         `json:"result,omitempty"`
	Metadata        json.RawMessage `json:"metadata,omitempty"`
	TaskType        *string         `json:"task_type,omitempty"`
	TaskNumber      *int            `json:"task_number,omitempty"`
	Identifier      *string         `json:"identifier,omitempty"`
	OwnerAgentKey   *string         `json:"owner_agent_key,omitempty"`
	CreatedByKey    *string         `json:"created_by_key,omitempty"`
	AssigneeUserID  *string         `json:"assignee_user_id,omitempty"`
	ParentIdx       *int            `json:"parent_idx,omitempty"` // index into tasks array
	ProgressPercent *int            `json:"progress_percent,omitempty"`
	ProgressStep    *string         `json:"progress_step,omitempty"`
}

// TeamTaskCommentExport is a portable task comment referencing task by index.
type TeamTaskCommentExport struct {
	TaskIdx     int             `json:"task_idx"`
	AgentKey    *string         `json:"agent_key,omitempty"`
	UserID      *string         `json:"user_id,omitempty"`
	Content     string          `json:"content"`
	CommentType string          `json:"comment_type"`
	Metadata    json.RawMessage `json:"metadata,omitempty"`
}

// TeamTaskEventExport is a portable task event referencing task by index.
type TeamTaskEventExport struct {
	TaskIdx   int             `json:"task_idx"`
	EventType string          `json:"event_type"`
	ActorType string          `json:"actor_type"`
	ActorID   string          `json:"actor_id"`
	Data      json.RawMessage `json:"data,omitempty"`
}

// AgentLinkExport represents an inter-agent link using agent_key references.
type AgentLinkExport struct {
	SourceAgentKey string `json:"source_agent_key"`
	TargetAgentKey string `json:"target_agent_key"`
	Direction      string `json:"direction"`
	Description    string `json:"description,omitempty"`
}

// TeamTasksExport bundles tasks with their UUIDs for downstream comment/event mapping.
type TeamTasksExport struct {
	Tasks    []TeamTaskExport
	TaskUIDs []uuid.UUID // parallel to Tasks — used to map comments/events by index
}

// ExportTeamByLead finds the team led by agentID and returns metadata + members.
// Returns (nil, nil, nil) if the agent is not a team lead.
func ExportTeamByLead(ctx context.Context, db *sql.DB, agentID uuid.UUID) (*TeamExport, uuid.UUID, []TeamMemberExport, error) {
	tc, tcArgs, _, err := scopeClause(ctx, 2)
	if err != nil {
		return nil, uuid.Nil, nil, err
	}
	args := append([]any{agentID}, tcArgs...)

	var teamID uuid.UUID
	var t TeamExport
	err = db.QueryRowContext(ctx,
		"SELECT id, name, COALESCE(description,''), status, COALESCE(settings,'{}')"+
			" FROM agent_teams WHERE lead_agent_id = $1"+tc,
		args...,
	).Scan(&teamID, &t.Name, &t.Description, &t.Status, &t.Settings)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, uuid.Nil, nil, nil
	}
	if err != nil {
		return nil, uuid.Nil, nil, err
	}

	members, err := exportTeamMembers(ctx, db, teamID, agentID)
	if err != nil {
		return nil, uuid.Nil, nil, err
	}
	return &t, teamID, members, nil
}

// exportTeamMembers returns members (excluding lead) with agent_key resolved.
func exportTeamMembers(ctx context.Context, db *sql.DB, teamID, leadAgentID uuid.UUID) ([]TeamMemberExport, error) {
	tc, tcArgs, _, err := scopeClauseAlias(ctx, 3, "m")
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx,
		"SELECT a.agent_key, m.role"+
			" FROM agent_team_members m"+
			" JOIN agents a ON a.id = m.agent_id"+
			" WHERE m.team_id = $1 AND m.agent_id != $2"+tc,
		append([]any{teamID, leadAgentID}, tcArgs...)...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TeamMemberExport
	for rows.Next() {
		var m TeamMemberExport
		if err := rows.Scan(&m.AgentKey, &m.Role); err != nil {
			slog.Warn("export.team.member.scan", "error", err)
			continue
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ExportTeamTasks returns all tasks for a team, resolving agent keys for owner/creator.
func ExportTeamTasks(ctx context.Context, db *sql.DB, teamID uuid.UUID) (*TeamTasksExport, error) {
	tc, tcArgs, _, err := scopeClause(ctx, 2)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx,
		"SELECT t.id, t.subject, COALESCE(t.description,''), t.status, t.priority,"+
			" t.result, t.metadata, t.task_type, t.task_number, t.identifier,"+
			" oa.agent_key, ca.agent_key, t.assignee_user_id, t.parent_id,"+
			" t.progress_percent, t.progress_step"+
			" FROM team_tasks t"+
			" LEFT JOIN agents oa ON oa.id = t.owner_agent_id"+
			" LEFT JOIN agents ca ON ca.id = t.created_by_agent_id"+
			" WHERE t.team_id = $1"+tc+
			" ORDER BY t.created_at",
		append([]any{teamID}, tcArgs...)...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out TeamTasksExport
	idxByUID := make(map[uuid.UUID]int)

	for rows.Next() {
		var (
			id             uuid.UUID
			taskResult     sql.NullString
			metadata       []byte
			taskType       sql.NullString
			taskNumber     sql.NullInt64
			identifier     sql.NullString
			ownerAgentKey  sql.NullString
			createdByKey   sql.NullString
			assigneeUserID sql.NullString
			parentID       *uuid.UUID
			progressPct    sql.NullInt64
			progressStep   sql.NullString
		)
		var te TeamTaskExport
		if err := rows.Scan(
			&id, &te.Subject, &te.Description, &te.Status, &te.Priority,
			&taskResult, &metadata, &taskType, &taskNumber, &identifier,
			&ownerAgentKey, &createdByKey, &assigneeUserID, &parentID,
			&progressPct, &progressStep,
		); err != nil {
			slog.Warn("export.team.task.scan", "error", err)
			continue
		}

		if taskResult.Valid {
			te.Result = &taskResult.String
		}
		if len(metadata) > 0 {
			te.Metadata = json.RawMessage(metadata)
		}
		if taskType.Valid {
			te.TaskType = &taskType.String
		}
		if taskNumber.Valid {
			n := int(taskNumber.Int64)
			te.TaskNumber = &n
		}
		if identifier.Valid {
			te.Identifier = &identifier.String
		}
		if ownerAgentKey.Valid {
			te.OwnerAgentKey = &ownerAgentKey.String
		}
		if createdByKey.Valid {
			te.CreatedByKey = &createdByKey.String
		}
		if assigneeUserID.Valid {
			te.AssigneeUserID = &assigneeUserID.String
		}
		if progressPct.Valid {
			n := int(progressPct.Int64)
			te.ProgressPercent = &n
		}
		if progressStep.Valid {
			te.ProgressStep = &progressStep.String
		}

		idx := len(out.Tasks)
		idxByUID[id] = idx
		out.Tasks = append(out.Tasks, te)
		out.TaskUIDs = append(out.TaskUIDs, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Second pass: resolve parent_id → parent_idx
	if err := resolveTaskParentIdx(ctx, db, teamID, tc, tcArgs, &out, idxByUID); err != nil {
		slog.Warn("export.team.tasks.parent_resolve", "error", err)
	}
	return &out, nil
}

// resolveTaskParentIdx fills ParentIdx for tasks that have a parent_id.
func resolveTaskParentIdx(ctx context.Context, db *sql.DB, teamID uuid.UUID, tc string, tcArgs []any, out *TeamTasksExport, idxByUID map[uuid.UUID]int) error {
	rows, err := db.QueryContext(ctx,
		"SELECT id, parent_id FROM team_tasks WHERE team_id = $1 AND parent_id IS NOT NULL"+tc,
		append([]any{teamID}, tcArgs...)...,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var id, parentID uuid.UUID
		if err := rows.Scan(&id, &parentID); err != nil {
			continue
		}
		taskIdx, ok := idxByUID[id]
		if !ok {
			continue
		}
		if parentIdx, ok := idxByUID[parentID]; ok {
			out.Tasks[taskIdx].ParentIdx = &parentIdx
		}
	}
	return rows.Err()
}

// ExportTeamComments returns all comments for a team's tasks, using task index references.
func ExportTeamComments(ctx context.Context, db *sql.DB, teamID uuid.UUID, taskUIDs []uuid.UUID) ([]TeamTaskCommentExport, error) {
	if len(taskUIDs) == 0 {
		return nil, nil
	}
	tc, tcArgs, _, err := scopeClause(ctx, 2)
	if err != nil {
		return nil, err
	}
	idxByUID := make(map[uuid.UUID]int, len(taskUIDs))
	for i, uid := range taskUIDs {
		idxByUID[uid] = i
	}

	rows, err := db.QueryContext(ctx,
		"SELECT c.task_id, a.agent_key, c.user_id, c.content,"+
			" COALESCE(c.comment_type,'note'), c.metadata"+
			" FROM team_task_comments c"+
			" LEFT JOIN agents a ON a.id = c.agent_id"+
			" WHERE c.task_id IN (SELECT id FROM team_tasks WHERE team_id = $1"+tc+")"+
			" ORDER BY c.created_at",
		append([]any{teamID}, tcArgs...)...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TeamTaskCommentExport
	for rows.Next() {
		var (
			taskID   uuid.UUID
			agentKey sql.NullString
			userID   sql.NullString
			metadata []byte
		)
		var ce TeamTaskCommentExport
		if err := rows.Scan(&taskID, &agentKey, &userID, &ce.Content, &ce.CommentType, &metadata); err != nil {
			slog.Warn("export.team.comment.scan", "error", err)
			continue
		}
		idx, ok := idxByUID[taskID]
		if !ok {
			continue
		}
		ce.TaskIdx = idx
		if agentKey.Valid {
			ce.AgentKey = &agentKey.String
		}
		if userID.Valid && userID.String != "" {
			ce.UserID = &userID.String
		}
		if len(metadata) > 0 {
			ce.Metadata = json.RawMessage(metadata)
		}
		out = append(out, ce)
	}
	return out, rows.Err()
}

// ExportTeamEvents returns all events for a team's tasks, using task index references.
func ExportTeamEvents(ctx context.Context, db *sql.DB, teamID uuid.UUID, taskUIDs []uuid.UUID) ([]TeamTaskEventExport, error) {
	if len(taskUIDs) == 0 {
		return nil, nil
	}
	tc, tcArgs, _, err := scopeClause(ctx, 2)
	if err != nil {
		return nil, err
	}
	idxByUID := make(map[uuid.UUID]int, len(taskUIDs))
	for i, uid := range taskUIDs {
		idxByUID[uid] = i
	}

	rows, err := db.QueryContext(ctx,
		"SELECT task_id, event_type, actor_type, actor_id, data"+
			" FROM team_task_events"+
			" WHERE task_id IN (SELECT id FROM team_tasks WHERE team_id = $1"+tc+")"+
			" ORDER BY created_at",
		append([]any{teamID}, tcArgs...)...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TeamTaskEventExport
	for rows.Next() {
		var (
			taskID uuid.UUID
			data   []byte
		)
		var ev TeamTaskEventExport
		if err := rows.Scan(&taskID, &ev.EventType, &ev.ActorType, &ev.ActorID, &data); err != nil {
			slog.Warn("export.team.event.scan", "error", err)
			continue
		}
		idx, ok := idxByUID[taskID]
		if !ok {
			continue
		}
		ev.TaskIdx = idx
		if len(data) > 0 {
			ev.Data = json.RawMessage(data)
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

// ExportAgentLinks returns all agent links where agentID is source or target.
func ExportAgentLinks(ctx context.Context, db *sql.DB, agentID uuid.UUID) ([]AgentLinkExport, error) {
	tc, tcArgs, _, err := scopeClauseAlias(ctx, 2, "l")
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx,
		"SELECT sa.agent_key, ta.agent_key, l.direction, COALESCE(l.description,'')"+
			" FROM agent_links l"+
			" JOIN agents sa ON sa.id = l.source_agent_id"+
			" JOIN agents ta ON ta.id = l.target_agent_id"+
			" WHERE (l.source_agent_id = $1 OR l.target_agent_id = $1)"+tc,
		append([]any{agentID}, tcArgs...)...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AgentLinkExport
	for rows.Next() {
		var l AgentLinkExport
		if err := rows.Scan(&l.SourceAgentKey, &l.TargetAgentKey, &l.Direction, &l.Description); err != nil {
			slog.Warn("export.agent_link.scan", "error", err)
			continue
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ExportTeamPreviewCounts returns team-related counts for the preview endpoint.
// Returns zeros if the agent is not a team lead.
func ExportTeamPreviewCounts(ctx context.Context, db *sql.DB, agentID uuid.UUID) (teamTasks, teamMembers, agentLinks int, err error) {
	tc, tcArgs, _, err := scopeClause(ctx, 2)
	if err != nil {
		return
	}
	args := append([]any{agentID}, tcArgs...)

	var teamID uuid.UUID
	err = db.QueryRowContext(ctx,
		"SELECT id FROM agent_teams WHERE lead_agent_id = $1"+tc, args...,
	).Scan(&teamID)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
		// Still count agent links even if no team
		_ = db.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM agent_links WHERE (source_agent_id = $1 OR target_agent_id = $1)"+tc,
			args...,
		).Scan(&agentLinks)
		return
	}
	if err != nil {
		return
	}

	tc2, tcArgs2, _, _ := scopeClause(ctx, 2)
	_ = db.QueryRowContext(ctx,
		"SELECT"+
			" (SELECT COUNT(*) FROM team_tasks WHERE team_id = $1"+tc2+"),"+
			" (SELECT COUNT(*) FROM agent_team_members WHERE team_id = $1"+tc2+")",
		append([]any{teamID}, tcArgs2...)...,
	).Scan(&teamTasks, &teamMembers)

	_ = db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM agent_links WHERE (source_agent_id = $1 OR target_agent_id = $1)"+tc2,
		append([]any{agentID}, tcArgs2...)...,
	).Scan(&agentLinks)

	return
}
