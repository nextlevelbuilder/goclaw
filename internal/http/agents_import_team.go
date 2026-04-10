package http

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// importTeamSection creates a team, members, tasks, comments, events, links,
// and team workspace for the imported agent (which becomes the team lead).
func (h *AgentsHandler) importTeamSection(ctx context.Context, ag *store.AgentData, arc *importArchive, progressFn func(ProgressEvent)) error {
	tid := importTenantID(ctx)
	userID := store.UserIDFromContext(ctx)

	// Check if agent is already a team lead — skip to prevent duplicate teams
	var existingTeam bool
	_ = h.db.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM agent_teams WHERE lead_agent_id = $1 AND tenant_id = $2)",
		ag.ID, tid,
	).Scan(&existingTeam)
	if existingTeam {
		slog.Info("import: agent already has a team, skipping team import", "agent_id", ag.ID)
		if progressFn != nil {
			progressFn(ProgressEvent{Phase: "team", Status: "done", Detail: "skipped (team exists)"})
		}
		return nil
	}

	// Create team with new UUID
	teamID := uuid.Must(uuid.NewV7())
	_, err := h.db.ExecContext(ctx,
		`INSERT INTO agent_teams (id, name, lead_agent_id, description, status, settings, created_by, created_at, updated_at, tenant_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, NOW(), NOW(), $8)`,
		teamID, arc.teamMeta.Name, ag.ID,
		arc.teamMeta.Description, arc.teamMeta.Status,
		coalesceJSON(arc.teamMeta.Settings), userID, tid,
	)
	if err != nil {
		return fmt.Errorf("create team: %w", err)
	}

	// Add lead as member
	if _, err = h.db.ExecContext(ctx,
		`INSERT INTO agent_team_members (team_id, agent_id, role, tenant_id, joined_at)
		 VALUES ($1, $2, 'lead', $3, NOW())
		 ON CONFLICT (team_id, agent_id) DO NOTHING`,
		teamID, ag.ID, tid,
	); err != nil {
		slog.Warn("import.team: add lead member", "error", err)
	}

	// Resolve agent_key → agent_id for all referenced keys
	agentKeyToID := h.buildAgentKeyMap(ctx, tid, arc)
	// Always include the importing agent itself
	agentKeyToID[ag.AgentKey] = ag.ID

	// Insert non-lead members
	for _, m := range arc.teamMembers {
		memberID, ok := agentKeyToID[m.AgentKey]
		if !ok {
			slog.Info("import.team: member not found, skipping", "agent_key", m.AgentKey)
			continue
		}
		if _, err = h.db.ExecContext(ctx,
			`INSERT INTO agent_team_members (team_id, agent_id, role, tenant_id, joined_at)
			 VALUES ($1, $2, $3, $4, NOW())
			 ON CONFLICT (team_id, agent_id) DO NOTHING`,
			teamID, memberID, m.Role, tid,
		); err != nil {
			slog.Warn("import.team: insert member", "agent_key", m.AgentKey, "error", err)
		}
	}

	// Insert tasks — collect new IDs by index for parent/comment/event mapping
	taskIDByIdx := make([]uuid.UUID, len(arc.teamTasks))
	for i, t := range arc.teamTasks {
		newID := uuid.Must(uuid.NewV7())
		taskIDByIdx[i] = newID

		var ownerID *uuid.UUID
		if t.OwnerAgentKey != nil {
			if id, ok := agentKeyToID[*t.OwnerAgentKey]; ok {
				ownerID = &id
			}
		}
		var createdByID *uuid.UUID
		if t.CreatedByKey != nil {
			if id, ok := agentKeyToID[*t.CreatedByKey]; ok {
				createdByID = &id
			}
		}

		if _, err = h.db.ExecContext(ctx,
			`INSERT INTO team_tasks
			   (id, team_id, subject, description, status, priority, result, metadata,
			    task_type, task_number, identifier, owner_agent_id, created_by_agent_id,
			    assignee_user_id, progress_percent, progress_step, tenant_id, created_at, updated_at)
			 VALUES ($1,$2,$3,$4,'pending',$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,NOW(),NOW())`,
			newID, teamID, t.Subject, t.Description,
			t.Priority, nullStr(t.Result), nullJSON(t.Metadata),
			t.TaskType, t.TaskNumber, t.Identifier,
			ownerID, createdByID, t.AssigneeUserID,
			nullInt(t.ProgressPercent), nullStr(t.ProgressStep),
			tid,
		); err != nil {
			slog.Warn("import.team: insert task", "subject", t.Subject, "error", err)
		}
	}

	// Second pass: wire parent_id now that all task IDs exist
	for i, t := range arc.teamTasks {
		if t.ParentIdx == nil {
			continue
		}
		pidx := *t.ParentIdx
		if pidx < 0 || pidx >= len(taskIDByIdx) {
			continue
		}
		if _, err = h.db.ExecContext(ctx,
			`UPDATE team_tasks SET parent_id = $1 WHERE id = $2`,
			taskIDByIdx[pidx], taskIDByIdx[i],
		); err != nil {
			slog.Warn("import.team: set parent_id", "child_idx", i, "error", err)
		}
	}

	// Insert comments
	for _, c := range arc.teamComments {
		if c.TaskIdx < 0 || c.TaskIdx >= len(taskIDByIdx) {
			continue
		}
		taskID := taskIDByIdx[c.TaskIdx]

		var agentID *uuid.UUID
		if c.AgentKey != nil {
			if id, ok := agentKeyToID[*c.AgentKey]; ok {
				agentID = &id
			}
		}
		if _, err = h.db.ExecContext(ctx,
			`INSERT INTO team_task_comments (id, task_id, agent_id, user_id, content, comment_type, metadata, tenant_id, created_at)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NOW())`,
			uuid.Must(uuid.NewV7()), taskID, agentID, c.UserID,
			c.Content, c.CommentType, nullJSON(c.Metadata), tid,
		); err != nil {
			slog.Warn("import.team: insert comment", "task_id", taskID, "error", err)
		}
	}

	// Insert events
	for _, ev := range arc.teamEvents {
		if ev.TaskIdx < 0 || ev.TaskIdx >= len(taskIDByIdx) {
			continue
		}
		taskID := taskIDByIdx[ev.TaskIdx]
		if _, err = h.db.ExecContext(ctx,
			`INSERT INTO team_task_events (id, task_id, event_type, actor_type, actor_id, data, tenant_id, created_at)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,NOW())`,
			uuid.Must(uuid.NewV7()), taskID,
			ev.EventType, ev.ActorType, ev.ActorID, nullJSON(ev.Data), tid,
		); err != nil {
			slog.Warn("import.team: insert event", "task_id", taskID, "error", err)
		}
	}

	// Insert agent_links
	for _, l := range arc.teamLinks {
		srcID, srcOK := agentKeyToID[l.SourceAgentKey]
		tgtID, tgtOK := agentKeyToID[l.TargetAgentKey]
		if !srcOK || !tgtOK {
			slog.Info("import.team: agent_link skipped — agent not found",
				"source", l.SourceAgentKey, "target", l.TargetAgentKey)
			continue
		}
		if _, err = h.db.ExecContext(ctx,
			`INSERT INTO agent_links (id, source_agent_id, target_agent_id, direction, description, created_by, tenant_id)
			 VALUES ($1,$2,$3,$4,$5,$6,$7)
			 ON CONFLICT DO NOTHING`,
			uuid.Must(uuid.NewV7()), srcID, tgtID,
			l.Direction, l.Description, userID, tid,
		); err != nil {
			slog.Warn("import.team: insert link", "error", err)
		}
	}

	// Extract team workspace files
	if len(arc.teamWorkspace) > 0 && h.dataDir != "" {
		wsPath := filepath.Join(config.ExpandHome(h.dataDir), "teams", teamID.String())
		imported, wsErr := extractWorkspaceFiles(wsPath, arc.teamWorkspace, false)
		if wsErr != nil {
			slog.Warn("import.team: workspace extraction failed", "path", wsPath, "error", wsErr)
		}
		if progressFn != nil {
			progressFn(ProgressEvent{Phase: "team_workspace", Status: "done", Current: imported, Total: len(arc.teamWorkspace)})
		}
	}

	if progressFn != nil {
		progressFn(ProgressEvent{Phase: "team", Status: "done", Current: len(arc.teamTasks), Total: len(arc.teamTasks)})
	}
	return nil
}

// buildAgentKeyMap resolves all agent_keys referenced in the team section to UUIDs.
func (h *AgentsHandler) buildAgentKeyMap(ctx context.Context, tid uuid.UUID, arc *importArchive) map[string]uuid.UUID {
	keys := make(map[string]struct{})
	for _, m := range arc.teamMembers {
		keys[m.AgentKey] = struct{}{}
	}
	for _, t := range arc.teamTasks {
		if t.OwnerAgentKey != nil {
			keys[*t.OwnerAgentKey] = struct{}{}
		}
		if t.CreatedByKey != nil {
			keys[*t.CreatedByKey] = struct{}{}
		}
	}
	for _, c := range arc.teamComments {
		if c.AgentKey != nil {
			keys[*c.AgentKey] = struct{}{}
		}
	}
	for _, l := range arc.teamLinks {
		keys[l.SourceAgentKey] = struct{}{}
		keys[l.TargetAgentKey] = struct{}{}
	}

	out := make(map[string]uuid.UUID, len(keys))
	for key := range keys {
		var id uuid.UUID
		if err := h.db.QueryRowContext(ctx,
			`SELECT id FROM agents WHERE agent_key = $1 AND tenant_id = $2`, key, tid,
		).Scan(&id); err == nil {
			out[key] = id
		}
	}
	return out
}

// nullInt returns nil if n is nil, otherwise the int value — for nullable int columns.
func nullInt(n *int) any {
	if n == nil {
		return nil
	}
	return *n
}
