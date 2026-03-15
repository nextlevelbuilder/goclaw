package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

const (
	maxFileSizeBytes = 10 * 1024 * 1024 // 10MB
	maxFilesPerScope = 100
	maxBatchSize     = 20
	// maxVersionsPerFile = 5 // [DB-DISABLED] versioning requires DB
)

// WorkspaceWriteTool allows agents to write files to the team shared workspace.
type WorkspaceWriteTool struct {
	manager *TeamToolManager
	dataDir string
}

func NewWorkspaceWriteTool(manager *TeamToolManager, dataDir string) *WorkspaceWriteTool {
	return &WorkspaceWriteTool{manager: manager, dataDir: dataDir}
}

func (t *WorkspaceWriteTool) Name() string { return "workspace_write" }

func (t *WorkspaceWriteTool) Description() string {
	return "Write files to the team shared workspace (visible to all members). Supports batch write and template management (lead only)."
}

func (t *WorkspaceWriteTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"description": "'write' (default) or 'set_template' (lead only)",
			},
			"file_name": map[string]any{
				"type":        "string",
				"description": "File name (alphanumeric + hyphens/underscores/dots, max 255 chars)",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "File content (text)",
			},
			"files": map[string]any{
				"type":        "array",
				"description": "Batch write: array of {file_name, content} objects (max 20)",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"file_name": map[string]any{"type": "string"},
						"content":   map[string]any{"type": "string"},
					},
				},
			},
			// [DB-DISABLED] task_id linkage requires DB
			// "task_id": map[string]any{
			// 	"type":        "string",
			// 	"description": "Link file to a team task ID (optional)",
			// },
			"templates": map[string]any{
				"type":        "array",
				"description": "For action=set_template: array of {file_name, content}",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"file_name": map[string]any{"type": "string"},
						"content":   map[string]any{"type": "string"},
					},
				},
			},
		},
	}
}

type writeFileEntry struct {
	FileName string `json:"file_name"`
	Content  string `json:"content"`
}

func (t *WorkspaceWriteTool) Execute(ctx context.Context, args map[string]any) *Result {
	action, _ := args["action"].(string)
	if action == "" {
		action = "write"
	}

	team, agentID, err := t.manager.resolveTeam(ctx)
	if err != nil {
		return ErrorResult(err.Error())
	}

	role, err := t.manager.resolveTeamRole(ctx, team, agentID)
	if err != nil {
		return ErrorResult(err.Error())
	}

	ws := parseWorkspaceSettings(team.Settings)

	switch action {
	case "set_template":
		return t.executeSetTemplate(ctx, args, team, role)
	case "write":
		return t.executeWrite(ctx, args, team, agentID, role, ws)
	default:
		return ErrorResult(fmt.Sprintf("unknown action %q (use 'write' or 'set_template')", action))
	}
}

func (t *WorkspaceWriteTool) executeSetTemplate(ctx context.Context, args map[string]any, team *store.TeamData, role string) *Result {
	if role != store.TeamRoleLead {
		return ErrorResult("only the team lead can set workspace templates")
	}

	// Check escalation policy.
	if esc := t.manager.checkEscalation(team, "set_template"); esc != EscalationNone {
		if esc == EscalationReject {
			return ErrorResult("set_template action is not allowed by team escalation policy")
		}
		agentID := store.AgentIDFromContext(ctx)
		return t.manager.createEscalationTask(ctx, team, agentID,
			"Set workspace templates",
			"Agent requested to update workspace templates.")
	}

	templatesRaw, ok := args["templates"]
	if !ok {
		return ErrorResult("templates parameter is required for action=set_template")
	}
	templatesJSON, err := json.Marshal(templatesRaw)
	if err != nil {
		return ErrorResult("invalid templates format")
	}
	var templates []writeFileEntry
	if err := json.Unmarshal(templatesJSON, &templates); err != nil {
		return ErrorResult("templates must be array of {file_name, content}")
	}

	// Validate template file names.
	for _, tmpl := range templates {
		if _, err := sanitizeFileName(tmpl.FileName); err != nil {
			return ErrorResult(fmt.Sprintf("template %q: %s", tmpl.FileName, err))
		}
	}

	// Update team settings with templates (stored in team settings JSON, not workspace DB).
	var settings map[string]any
	if team.Settings != nil {
		_ = json.Unmarshal(team.Settings, &settings)
	}
	if settings == nil {
		settings = make(map[string]any)
	}
	settings["workspace_templates"] = templates
	settingsJSON, _ := json.Marshal(settings)

	if err := t.manager.teamStore.UpdateTeam(ctx, team.ID, map[string]any{"settings": settingsJSON}); err != nil {
		return ErrorResult("failed to save templates: " + err.Error())
	}
	t.manager.InvalidateTeam()

	return NewResult(fmt.Sprintf("Set %d workspace template(s)", len(templates)))
}

func (t *WorkspaceWriteTool) executeWrite(ctx context.Context, args map[string]any, team *store.TeamData, _ /* agentID */ any, role string, ws workspaceSettings) *Result {
	if role == store.TeamRoleReviewer {
		return ErrorResult("reviewers cannot write to the workspace")
	}

	// Resolve scope.
	channel, chatID, scopeErr := resolveWorkspaceScopeFromArgs(ctx, args, ws)
	if scopeErr != "" {
		return ErrorResult(scopeErr)
	}

	// [DB-DISABLED] task linkage requires DB
	// var taskID *uuid.UUID
	// if tid, ok := args["task_id"].(string); ok && tid != "" {
	// 	parsed, err := uuid.Parse(tid)
	// 	if err != nil {
	// 		return ErrorResult("invalid task_id: " + err.Error())
	// 	}
	// 	taskID = &parsed
	// } else if ctxTID := TeamTaskIDFromCtx(ctx); ctxTID != "" {
	// 	if parsed, err := uuid.Parse(ctxTID); err == nil {
	// 		taskID = &parsed
	// 	}
	// }

	// Normalize input to batch.
	var entries []writeFileEntry
	if filesRaw, ok := args["files"]; ok {
		filesJSON, err := json.Marshal(filesRaw)
		if err != nil {
			return ErrorResult("invalid files format")
		}
		if err := json.Unmarshal(filesJSON, &entries); err != nil {
			return ErrorResult("files must be array of {file_name, content}")
		}
	} else {
		fn, _ := args["file_name"].(string)
		content, _ := args["content"].(string)
		if fn == "" {
			return ErrorResult("file_name is required")
		}
		entries = []writeFileEntry{{FileName: fn, Content: content}}
	}

	if len(entries) == 0 {
		return ErrorResult("no files to write")
	}
	if len(entries) > maxBatchSize {
		return ErrorResult(fmt.Sprintf("batch size exceeds limit (%d max)", maxBatchSize))
	}

	// Validate all entries before writing.
	for i, e := range entries {
		name, err := sanitizeFileName(e.FileName)
		if err != nil {
			return ErrorResult(fmt.Sprintf("file %d: %s", i+1, err))
		}
		entries[i].FileName = name

		if _, err := inferMimeType(name); err != nil {
			return ErrorResult(fmt.Sprintf("file %q: %s", name, err))
		}
		if len(e.Content) > maxFileSizeBytes {
			return ErrorResult(fmt.Sprintf("file %q exceeds max size (10MB)", name))
		}
	}

	// Create workspace directory.
	dir, err := workspaceDir(t.dataDir, team.ID, channel, chatID)
	if err != nil {
		return ErrorResult(err.Error())
	}

	// Check file count limit from filesystem.
	existingFiles, _ := os.ReadDir(dir)
	fileCount := 0
	for _, f := range existingFiles {
		if !f.IsDir() {
			fileCount++
		}
	}

	// Auto-seed templates on first write to this scope.
	if fileCount == 0 {
		t.seedTemplates(team, channel, chatID, ws)
		// Recount after seeding.
		existingFiles, _ = os.ReadDir(dir)
		fileCount = 0
		for _, f := range existingFiles {
			if !f.IsDir() {
				fileCount++
			}
		}
	}

	if fileCount+len(entries) > maxFilesPerScope {
		return ErrorResult(fmt.Sprintf("workspace file limit reached (%d/%d)", fileCount, maxFilesPerScope))
	}

	// Write files directly to disk (no DB).
	var results []string
	var errors []string
	for _, e := range entries {
		diskPath := filepath.Join(dir, e.FileName)

		if err := os.WriteFile(diskPath, []byte(e.Content), 0640); err != nil {
			errors = append(errors, fmt.Sprintf("%s: %s", e.FileName, err))
			continue
		}

		results = append(results, fmt.Sprintf("%s (%s)", e.FileName, formatBytes(int64(len(e.Content)))))

		// Broadcast event.
		t.manager.broadcastTeamEvent(protocol.EventWorkspaceFileChanged, map[string]string{
			"team_id":   team.ID.String(),
			"channel":   channel,
			"chat_id":   chatID,
			"file_name": e.FileName,
			"action":    "write",
		})
	}

	if len(results) == 0 && len(errors) > 0 {
		return ErrorResult("all writes failed:\n" + strings.Join(errors, "\n"))
	}

	msg := fmt.Sprintf("Written %d file(s) to workspace: %s", len(results), strings.Join(results, ", "))
	if len(errors) > 0 {
		msg += fmt.Sprintf("\n%d failed: %s", len(errors), strings.Join(errors, "; "))
	}
	return NewResult(msg)
}

func (t *WorkspaceWriteTool) seedTemplates(team *store.TeamData, channel, chatID string, ws workspaceSettings) {
	if len(ws.WorkspaceTemplates) == 0 {
		return
	}

	dir, err := workspaceDir(t.dataDir, team.ID, channel, chatID)
	if err != nil {
		return
	}

	for _, tmpl := range ws.WorkspaceTemplates {
		name, err := sanitizeFileName(tmpl.FileName)
		if err != nil {
			continue
		}
		diskPath := filepath.Join(dir, name)
		if err := os.WriteFile(diskPath, []byte(tmpl.Content), 0640); err != nil {
			slog.Warn("workspace: template seed failed", "file", name, "error", err)
		}
	}
	slog.Info("workspace: seeded templates", "count", len(ws.WorkspaceTemplates), "team", team.ID, "channel", channel, "chat_id", chatID)
}

func formatBytes(b int64) string {
	switch {
	case b >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(b)/(1024*1024))
	case b >= 1024:
		return fmt.Sprintf("%.1f KB", float64(b)/1024)
	default:
		return fmt.Sprintf("%d B", b)
	}
}
