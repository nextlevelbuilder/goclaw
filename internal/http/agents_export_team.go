package http

import (
	"archive/tar"
	"context"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// exportTeamSection exports team metadata, members, tasks, comments, events, links,
// and team workspace files into the tar archive under the "team/" prefix.
func (h *AgentsHandler) exportTeamSection(ctx context.Context, tw *tar.Writer, agentID uuid.UUID, manifest *ExportManifest, progressFn func(ProgressEvent)) error {
	team, teamID, members, err := pg.ExportTeamByLead(ctx, h.db, agentID)
	if err != nil {
		return fmt.Errorf("query team: %w", err)
	}
	if team == nil {
		// Agent is not a team lead — nothing to export
		manifest.Sections["team"] = map[string]any{"lead": false}
		return nil
	}

	// team/team.json
	teamJSON, err := jsonIndent(team)
	if err != nil {
		return fmt.Errorf("marshal team: %w", err)
	}
	if err := addToTar(tw, "team/team.json", teamJSON); err != nil {
		return fmt.Errorf("write team/team.json: %w", err)
	}

	// team/members.jsonl
	if len(members) > 0 {
		data, err := marshalJSONL(members)
		if err != nil {
			return fmt.Errorf("marshal members: %w", err)
		}
		if err := addToTar(tw, "team/members.jsonl", data); err != nil {
			return fmt.Errorf("write team/members.jsonl: %w", err)
		}
	}

	// team/tasks.jsonl
	tasksExport, err := pg.ExportTeamTasks(ctx, h.db, teamID)
	if err != nil {
		slog.Warn("export: team tasks query failed", "team_id", teamID, "error", err)
		tasksExport = &pg.TeamTasksExport{}
	}
	if len(tasksExport.Tasks) > 0 {
		data, err := marshalJSONL(tasksExport.Tasks)
		if err != nil {
			return fmt.Errorf("marshal tasks: %w", err)
		}
		if err := addToTar(tw, "team/tasks.jsonl", data); err != nil {
			return fmt.Errorf("write team/tasks.jsonl: %w", err)
		}
	}

	// team/comments.jsonl
	comments, err := pg.ExportTeamComments(ctx, h.db, teamID, tasksExport.TaskUIDs)
	if err != nil {
		slog.Warn("export: team comments query failed", "team_id", teamID, "error", err)
	}
	if len(comments) > 0 {
		data, err := marshalJSONL(comments)
		if err != nil {
			return fmt.Errorf("marshal comments: %w", err)
		}
		if err := addToTar(tw, "team/comments.jsonl", data); err != nil {
			return fmt.Errorf("write team/comments.jsonl: %w", err)
		}
	}

	// team/events.jsonl
	events, err := pg.ExportTeamEvents(ctx, h.db, teamID, tasksExport.TaskUIDs)
	if err != nil {
		slog.Warn("export: team events query failed", "team_id", teamID, "error", err)
	}
	if len(events) > 0 {
		data, err := marshalJSONL(events)
		if err != nil {
			return fmt.Errorf("marshal events: %w", err)
		}
		if err := addToTar(tw, "team/events.jsonl", data); err != nil {
			return fmt.Errorf("write team/events.jsonl: %w", err)
		}
	}

	// team/links.jsonl — agent_links for this agent
	links, err := pg.ExportAgentLinks(ctx, h.db, agentID)
	if err != nil {
		slog.Warn("export: agent links query failed", "agent_id", agentID, "error", err)
	}
	if len(links) > 0 {
		data, err := marshalJSONL(links)
		if err != nil {
			return fmt.Errorf("marshal links: %w", err)
		}
		if err := addToTar(tw, "team/links.jsonl", data); err != nil {
			return fmt.Errorf("write team/links.jsonl: %w", err)
		}
	}

	// team/workspace/ — team filesystem workspace
	var wsFileCount int
	var wsTotalBytes int64
	if h.dataDir != "" {
		wsPath := filepath.Join(config.ExpandHome(h.dataDir), "teams", teamID.String())
		wsFileCount, wsTotalBytes, _ = h.exportTeamWorkspaceFiles(ctx, tw, wsPath, progressFn)
	}

	manifest.Sections["team"] = map[string]any{
		"lead":        true,
		"members":     len(members),
		"tasks":       len(tasksExport.Tasks),
		"comments":    len(comments),
		"events":      len(events),
		"links":       len(links),
		"ws_files":    wsFileCount,
		"ws_bytes":    wsTotalBytes,
	}
	if progressFn != nil {
		progressFn(ProgressEvent{
			Phase:   "team",
			Status:  "done",
			Current: len(tasksExport.Tasks),
			Total:   len(tasksExport.Tasks),
		})
	}
	return nil
}

// exportTeamWorkspaceFiles walks the team workspace directory and adds files under "team/workspace/".
func (h *AgentsHandler) exportTeamWorkspaceFiles(ctx context.Context, tw *tar.Writer, wsPath string, progressFn func(ProgressEvent)) (int, int64, error) {
	return h.exportWorkspaceFilesWithPrefix(ctx, tw, wsPath, "team/workspace/", progressFn)
}
