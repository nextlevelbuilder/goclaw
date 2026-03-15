package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// WorkspaceReadTool allows agents to read, list, and delete files in the team shared workspace.
// [DB-DISABLED] pin, tag, history, comment, comments actions are temporarily disabled (require DB).
type WorkspaceReadTool struct {
	manager *TeamToolManager
	dataDir string
}

func NewWorkspaceReadTool(manager *TeamToolManager, dataDir string) *WorkspaceReadTool {
	return &WorkspaceReadTool{manager: manager, dataDir: dataDir}
}

func (t *WorkspaceReadTool) Name() string { return "workspace_read" }

func (t *WorkspaceReadTool) Description() string {
	return "Read and manage files in the team shared workspace. Actions: list, read (default), delete."
}

func (t *WorkspaceReadTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"description": "'list', 'read' (default), 'delete'",
			},
			"file_name": map[string]any{
				"type":        "string",
				"description": "File name (required for read and delete)",
			},
			// [DB-DISABLED] These parameters require DB-backed features:
			// "pinned":  — pin/unpin (lead only)
			// "tags":    — tag files (lead only)
			// "version": — read specific version
			// "text":    — add comment
		},
	}
}

func (t *WorkspaceReadTool) Execute(ctx context.Context, args map[string]any) *Result {
	team, agentID, err := t.manager.resolveTeam(ctx)
	if err != nil {
		return ErrorResult(err.Error())
	}

	role, err := t.manager.resolveTeamRole(ctx, team, agentID)
	if err != nil {
		return ErrorResult(err.Error())
	}

	ws := parseWorkspaceSettings(team.Settings)

	// Resolve scope.
	channel, chatID, scopeErr := resolveWorkspaceScopeFromArgs(ctx, args, ws)
	if scopeErr != "" {
		return ErrorResult(scopeErr)
	}

	action, _ := args["action"].(string)
	if action == "" {
		action = "read"
	}

	switch action {
	case "list":
		return t.executeList(team, channel, chatID)
	case "read":
		return t.executeRead(args, team, channel, chatID)
	case "delete":
		return t.executeDelete(args, team, agentID, role, channel, chatID)
	// [DB-DISABLED] These actions require DB:
	// case "pin":
	// 	return t.executePin(ctx, args, team, role, channel, chatID)
	// case "tag":
	// 	return t.executeTag(ctx, args, team, role, channel, chatID)
	// case "history":
	// 	return t.executeHistory(ctx, args, team, channel, chatID)
	// case "comment":
	// 	return t.executeComment(ctx, args, team, agentID, channel, chatID)
	// case "comments":
	// 	return t.executeComments(ctx, args, team, channel, chatID)
	default:
		return ErrorResult(fmt.Sprintf("unknown action %q (use 'list', 'read', or 'delete')", action))
	}
}

func (t *WorkspaceReadTool) executeList(team *store.TeamData, channel, chatID string) *Result {
	dir, err := workspaceDir(t.dataDir, team.ID, channel, chatID)
	if err != nil {
		return ErrorResult(err.Error())
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return NewResult("No workspace files in this scope.")
	}

	var lines []string
	var totalSize int64
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		totalSize += info.Size()
		mimeType, _ := inferMimeType(entry.Name())
		lines = append(lines, fmt.Sprintf("- %s (%s, %s)", entry.Name(), mimeType, formatBytes(info.Size())))
	}

	if len(lines) == 0 {
		return NewResult("No workspace files in this scope.")
	}

	const maxListFiles = 50
	header := fmt.Sprintf("Workspace path: %s\nWorkspace files (%d files, %s):\n", dir, len(lines), formatBytes(totalSize))
	footer := "\n\nTo read a file, use workspace_read(action=read, file_name=\"<name>\") — do NOT use read_file for workspace files."
	if len(lines) > maxListFiles {
		result := header + strings.Join(lines[:maxListFiles], "\n")
		result += fmt.Sprintf("\n\n[...truncated, showing %d of %d files. Use bash `ls %s` to see all files, or `find %s -name '*.ext'` to filter by type]",
			maxListFiles, len(lines), dir, dir)
		return NewResult(result + footer)
	}
	return NewResult(header + strings.Join(lines, "\n") + footer)
}

func (t *WorkspaceReadTool) executeRead(args map[string]any, team *store.TeamData, channel, chatID string) *Result {
	fileName, _ := args["file_name"].(string)
	if fileName == "" {
		return ErrorResult("file_name is required for action=read")
	}

	// Sanitize to prevent path traversal.
	name, err := sanitizeFileName(fileName)
	if err != nil {
		return ErrorResult(err.Error())
	}

	dir, err := workspaceDir(t.dataDir, team.ID, channel, chatID)
	if err != nil {
		return ErrorResult(err.Error())
	}

	diskPath := filepath.Join(dir, name)
	info, err := os.Stat(diskPath)
	if err != nil {
		return ErrorResult(fmt.Sprintf("file %q not found in workspace — use workspace_read(action=list) to see available files", name))
	}

	mimeType, _ := inferMimeType(name)

	// Binary files: return metadata only.
	if isBinaryMime(mimeType) {
		return NewResult(fmt.Sprintf("Binary file: %s (%s, %s). Use other tools to process binary files.",
			name, mimeType, formatBytes(info.Size())))
	}

	data, err := os.ReadFile(diskPath)
	if err != nil {
		return ErrorResult("failed to read file: " + err.Error())
	}
	content := string(data)
	if len(content) > 100000 {
		content = content[:100000] + "\n\n[...truncated at 100K chars]"
	}

	return NewResult(fmt.Sprintf("--- %s (%s, %s) ---\n%s",
		name, mimeType, formatBytes(info.Size()), content))
}

func (t *WorkspaceReadTool) executeDelete(args map[string]any, team *store.TeamData, _ any, role, channel, chatID string) *Result {
	fileName, _ := args["file_name"].(string)
	if fileName == "" {
		return ErrorResult("file_name is required for action=delete")
	}

	if role == store.TeamRoleReviewer {
		return ErrorResult("reviewers cannot delete workspace files")
	}

	name, err := sanitizeFileName(fileName)
	if err != nil {
		return ErrorResult(err.Error())
	}

	dir, err := workspaceDir(t.dataDir, team.ID, channel, chatID)
	if err != nil {
		return ErrorResult(err.Error())
	}

	diskPath := filepath.Join(dir, name)
	if _, err := os.Stat(diskPath); os.IsNotExist(err) {
		return ErrorResult(fmt.Sprintf("file %q not found in workspace — use workspace_read(action=list) to see available files", name))
	}

	if err := os.Remove(diskPath); err != nil {
		return ErrorResult("failed to delete file: " + err.Error())
	}

	// Broadcast event.
	t.manager.broadcastTeamEvent(protocol.EventWorkspaceFileChanged, map[string]string{
		"team_id":   team.ID.String(),
		"channel":   channel,
		"chat_id":   chatID,
		"file_name": name,
		"action":    "delete",
	})

	return NewResult(fmt.Sprintf("Deleted workspace file %q", name))
}

// [DB-DISABLED] The following methods are temporarily disabled — they require the
// team_workspace_files DB table for metadata (pins, tags, versions, comments).
// They will be re-enabled when DB-backed workspace tracking is needed.
//
// func (t *WorkspaceReadTool) executePin(...)
// func (t *WorkspaceReadTool) executeTag(...)
// func (t *WorkspaceReadTool) executeHistory(...)
// func (t *WorkspaceReadTool) executeComment(...)
// func (t *WorkspaceReadTool) executeComments(...)
