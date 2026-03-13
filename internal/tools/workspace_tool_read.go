package tools

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// WorkspaceReadTool allows agents to read, list, delete, pin, tag, and comment
// on files in the team shared workspace.
type WorkspaceReadTool struct {
	manager *TeamToolManager
	dataDir string
}

func NewWorkspaceReadTool(manager *TeamToolManager, dataDir string) *WorkspaceReadTool {
	return &WorkspaceReadTool{manager: manager, dataDir: dataDir}
}

func (t *WorkspaceReadTool) Name() string { return "workspace_read" }

func (t *WorkspaceReadTool) Description() string {
	return "Read and manage files in the team shared workspace. " +
		"Actions: list, read (default), delete, pin/tag (lead only), history, comment, comments."
}

func (t *WorkspaceReadTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"description": "'list', 'read' (default), 'delete', 'pin', 'tag', 'history', 'comment', 'comments'",
			},
			"file_name": map[string]any{
				"type":        "string",
				"description": "File name (required for most actions)",
			},
			"scope": map[string]any{
				"type":        "string",
				"description": "'channel' (default, per-user) or 'team' (shared, requires workspace_scope=shared)",
			},
			"pinned": map[string]any{
				"type":        "boolean",
				"description": "For action=pin: set pinned state",
			},
			"tags": map[string]any{
				"type":        "array",
				"description": "For action=tag: tags to set (deliverable, handoff, reference, draft)",
				"items":       map[string]any{"type": "string"},
			},
			"version": map[string]any{
				"type":        "integer",
				"description": "For action=read: read specific version",
			},
			"text": map[string]any{
				"type":        "string",
				"description": "For action=comment: comment text",
			},
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
		return t.executeList(ctx, team, channel, chatID, ws)
	case "read":
		return t.executeRead(ctx, args, team, channel, chatID)
	case "delete":
		return t.executeDelete(ctx, args, team, agentID, role, channel, chatID)
	case "pin":
		return t.executePin(ctx, args, team, role, channel, chatID)
	case "tag":
		return t.executeTag(ctx, args, team, role, channel, chatID)
	case "history":
		return t.executeHistory(ctx, args, team, channel, chatID)
	case "comment":
		return t.executeComment(ctx, args, team, agentID, channel, chatID)
	case "comments":
		return t.executeComments(ctx, args, team, channel, chatID)
	default:
		return ErrorResult(fmt.Sprintf("unknown action %q", action))
	}
}

func (t *WorkspaceReadTool) executeList(ctx context.Context, team *store.TeamData, channel, chatID string, ws workspaceSettings) *Result {
	files, err := t.manager.teamStore.ListWorkspaceFiles(ctx, team.ID, channel, chatID)
	if err != nil {
		return ErrorResult("failed to list workspace files: " + err.Error())
	}
	if len(files) == 0 {
		return NewResult("No workspace files in this scope.")
	}

	// Show quota info.
	var header string
	quotaMB := ws.quotaMB(defaultQuotaMB)
	if quotaMB > 0 {
		totalSize, _ := t.manager.teamStore.GetWorkspaceTotalSize(ctx, team.ID)
		usedMB := float64(totalSize) / (1024 * 1024)
		header = fmt.Sprintf("Workspace files (%d files, %.1f MB / %d MB quota):\n", len(files), usedMB, quotaMB)
	} else {
		header = fmt.Sprintf("Workspace files (%d files):\n", len(files))
	}

	var lines []string
	for _, f := range files {
		var tags strings.Builder
		if f.Pinned {
			tags.WriteString(" [pinned]")
		}
		for _, tag := range f.Tags {
			tags.WriteString(" [" + tag + "]")
		}
		taskLabel := ""
		if f.TaskID == nil {
			taskLabel = " [no task]"
		}
		missing := ""
		if _, err := os.Stat(f.FilePath); os.IsNotExist(err) {
			missing = " [missing]"
		}
		lines = append(lines, fmt.Sprintf("- %s (%s, %s, by %s)%s%s%s",
			f.FileName, f.MimeType, formatBytes(f.SizeBytes), f.UploadedByKey, tags.String(), taskLabel, missing))
	}
	return NewResult(header + strings.Join(lines, "\n"))
}

func (t *WorkspaceReadTool) executeRead(ctx context.Context, args map[string]any, team *store.TeamData, channel, chatID string) *Result {
	fileName, _ := args["file_name"].(string)
	if fileName == "" {
		return ErrorResult("file_name is required for action=read")
	}

	file, err := t.manager.teamStore.GetWorkspaceFile(ctx, team.ID, channel, chatID, fileName)
	if err != nil {
		return ErrorResult(err.Error())
	}

	// Check for specific version.
	if v, ok := args["version"].(float64); ok && int(v) > 0 {
		version, err := t.manager.teamStore.GetFileVersion(ctx, file.ID, int(v))
		if err != nil {
			return ErrorResult(err.Error())
		}
		data, err := os.ReadFile(version.FilePath)
		if err != nil {
			return ErrorResult(fmt.Sprintf("version %d file not found on disk", int(v)))
		}
		content := string(data)
		if len(content) > 100000 {
			content = content[:100000] + "\n\n[...truncated at 100K chars]"
		}
		return NewResult(fmt.Sprintf("--- %s (version %d, %s, by %s) ---\n%s",
			fileName, version.Version, formatBytes(version.SizeBytes), version.UploadedByKey, content))
	}

	// Binary files: return metadata only.
	if isBinaryMime(file.MimeType) {
		return NewResult(fmt.Sprintf("Binary file: %s (%s, %s, by %s). Use other tools to process binary files.",
			fileName, file.MimeType, formatBytes(file.SizeBytes), file.UploadedByKey))
	}

	// Read from disk.
	data, err := os.ReadFile(file.FilePath)
	if err != nil {
		return ErrorResult("file not found on disk: " + err.Error())
	}
	content := string(data)
	if len(content) > 100000 {
		content = content[:100000] + "\n\n[...truncated at 100K chars]"
	}

	return NewResult(fmt.Sprintf("--- %s (%s, %s, by %s) ---\n%s",
		fileName, file.MimeType, formatBytes(file.SizeBytes), file.UploadedByKey, content))
}

func (t *WorkspaceReadTool) executeDelete(ctx context.Context, args map[string]any, team *store.TeamData, agentID uuid.UUID, role, channel, chatID string) *Result {
	fileName, _ := args["file_name"].(string)
	if fileName == "" {
		return ErrorResult("file_name is required for action=delete")
	}

	// Check ownership for non-lead.
	file, err := t.manager.teamStore.GetWorkspaceFile(ctx, team.ID, channel, chatID, fileName)
	if err != nil {
		return ErrorResult(err.Error())
	}

	if file.Pinned {
		return ErrorResult("pinned files cannot be deleted — unpin first using action=pin pinned=false")
	}

	switch role {
	case store.TeamRoleReviewer:
		return ErrorResult("reviewers cannot delete workspace files")
	case store.TeamRoleMember:
		if file.UploadedBy != agentID {
			return ErrorResult("members can only delete their own files")
		}
	case store.TeamRoleLead:
		// Lead deleting another agent's file: check escalation.
		if file.UploadedBy != agentID {
			if esc := t.manager.checkEscalation(team, "delete"); esc != EscalationNone {
				if esc == EscalationReject {
					return ErrorResult("deleting others' files is not allowed by team escalation policy")
				}
				return t.manager.createEscalationTask(ctx, team, agentID,
					fmt.Sprintf("Delete file: %s", fileName),
					fmt.Sprintf("Agent requested to delete file %q uploaded by another agent.", fileName))
			}
		}
	}

	// Delete versions from disk.
	versions, _ := t.manager.teamStore.ListFileVersions(ctx, file.ID)
	for _, v := range versions {
		_ = os.Remove(v.FilePath)
	}

	// Delete DB record (cascades versions + comments).
	filePath, err := t.manager.teamStore.DeleteWorkspaceFile(ctx, team.ID, channel, chatID, fileName)
	if err != nil {
		return ErrorResult(err.Error())
	}

	// Delete disk file.
	_ = os.Remove(filePath)

	// Broadcast event.
	t.manager.broadcastTeamEvent(protocol.EventWorkspaceFileChanged, map[string]string{
		"team_id":   team.ID.String(),
		"channel":   channel,
		"chat_id":   chatID,
		"file_name": fileName,
		"action":    "delete",
	})

	return NewResult(fmt.Sprintf("Deleted workspace file %q", fileName))
}

func (t *WorkspaceReadTool) executePin(ctx context.Context, args map[string]any, team *store.TeamData, role, channel, chatID string) *Result {
	if role != store.TeamRoleLead {
		return ErrorResult("only the team lead can pin/unpin files")
	}

	// Check escalation policy.
	if esc := t.manager.checkEscalation(team, "pin"); esc != EscalationNone {
		if esc == EscalationReject {
			return ErrorResult("pin action is not allowed by team escalation policy")
		}
		agentID := store.AgentIDFromContext(ctx)
		fileName, _ := args["file_name"].(string)
		return t.manager.createEscalationTask(ctx, team, agentID,
			fmt.Sprintf("Pin file: %s", fileName),
			fmt.Sprintf("Agent requested to pin/unpin file %q in workspace.", fileName))
	}

	fileName, _ := args["file_name"].(string)
	if fileName == "" {
		return ErrorResult("file_name is required for action=pin")
	}

	pinned := true
	if v, ok := args["pinned"].(bool); ok {
		pinned = v
	}

	if err := t.manager.teamStore.PinWorkspaceFile(ctx, team.ID, channel, chatID, fileName, pinned); err != nil {
		return ErrorResult(err.Error())
	}

	action := "pinned"
	if !pinned {
		action = "unpinned"
	}
	return NewResult(fmt.Sprintf("File %q %s", fileName, action))
}

func (t *WorkspaceReadTool) executeTag(ctx context.Context, args map[string]any, team *store.TeamData, role, channel, chatID string) *Result {
	if role != store.TeamRoleLead {
		return ErrorResult("only the team lead can set file tags")
	}

	// Check escalation policy.
	if esc := t.manager.checkEscalation(team, "tag"); esc != EscalationNone {
		if esc == EscalationReject {
			return ErrorResult("tag action is not allowed by team escalation policy")
		}
		agentID := store.AgentIDFromContext(ctx)
		fileName, _ := args["file_name"].(string)
		return t.manager.createEscalationTask(ctx, team, agentID,
			fmt.Sprintf("Tag file: %s", fileName),
			fmt.Sprintf("Agent requested to tag file %q in workspace.", fileName))
	}

	fileName, _ := args["file_name"].(string)
	if fileName == "" {
		return ErrorResult("file_name is required for action=tag")
	}

	tagsRaw, ok := args["tags"]
	if !ok {
		return ErrorResult("tags parameter is required for action=tag")
	}

	var tags []string
	switch v := tagsRaw.(type) {
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				if !validTags[s] {
					return ErrorResult(fmt.Sprintf("invalid tag %q (valid: deliverable, handoff, reference, draft)", s))
				}
				tags = append(tags, s)
			}
		}
	default:
		return ErrorResult("tags must be an array of strings")
	}

	if err := t.manager.teamStore.TagWorkspaceFile(ctx, team.ID, channel, chatID, fileName, tags); err != nil {
		return ErrorResult(err.Error())
	}

	return NewResult(fmt.Sprintf("File %q tagged: %s", fileName, strings.Join(tags, ", ")))
}

func (t *WorkspaceReadTool) executeHistory(ctx context.Context, args map[string]any, team *store.TeamData, channel, chatID string) *Result {
	fileName, _ := args["file_name"].(string)
	if fileName == "" {
		return ErrorResult("file_name is required for action=history")
	}

	file, err := t.manager.teamStore.GetWorkspaceFile(ctx, team.ID, channel, chatID, fileName)
	if err != nil {
		return ErrorResult(err.Error())
	}

	versions, err := t.manager.teamStore.ListFileVersions(ctx, file.ID)
	if err != nil {
		return ErrorResult("failed to list versions: " + err.Error())
	}

	if len(versions) == 0 {
		return NewResult(fmt.Sprintf("No version history for %q (current version only)", fileName))
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("Version history for %q:", fileName))
	lines = append(lines, fmt.Sprintf("  current: %s, by %s, %s", formatBytes(file.SizeBytes), file.UploadedByKey, file.UpdatedAt.Format("2006-01-02 15:04")))
	for _, v := range versions {
		lines = append(lines, fmt.Sprintf("  v%d: %s, by %s, %s", v.Version, formatBytes(v.SizeBytes), v.UploadedByKey, v.CreatedAt.Format("2006-01-02 15:04")))
	}
	return NewResult(strings.Join(lines, "\n"))
}

func (t *WorkspaceReadTool) executeComment(ctx context.Context, args map[string]any, team *store.TeamData, agentID uuid.UUID, channel, chatID string) *Result {
	fileName, _ := args["file_name"].(string)
	if fileName == "" {
		return ErrorResult("file_name is required for action=comment")
	}

	text, _ := args["text"].(string)
	if text == "" {
		return ErrorResult("text is required for action=comment")
	}
	const maxCommentLength = 10000
	if len(text) > maxCommentLength {
		return ErrorResult(fmt.Sprintf("comment exceeds max length (%d chars)", maxCommentLength))
	}

	file, err := t.manager.teamStore.GetWorkspaceFile(ctx, team.ID, channel, chatID, fileName)
	if err != nil {
		return ErrorResult(err.Error())
	}

	if err := t.manager.teamStore.AddFileComment(ctx, &store.TeamWorkspaceCommentData{
		FileID:  file.ID,
		AgentID: agentID,
		Content: text,
	}); err != nil {
		return ErrorResult("failed to add comment: " + err.Error())
	}

	return NewResult(fmt.Sprintf("Comment added to %q", fileName))
}

func (t *WorkspaceReadTool) executeComments(ctx context.Context, args map[string]any, team *store.TeamData, channel, chatID string) *Result {
	fileName, _ := args["file_name"].(string)
	if fileName == "" {
		return ErrorResult("file_name is required for action=comments")
	}

	file, err := t.manager.teamStore.GetWorkspaceFile(ctx, team.ID, channel, chatID, fileName)
	if err != nil {
		return ErrorResult(err.Error())
	}

	comments, err := t.manager.teamStore.ListFileComments(ctx, file.ID)
	if err != nil {
		return ErrorResult("failed to list comments: " + err.Error())
	}

	if len(comments) == 0 {
		return NewResult(fmt.Sprintf("No comments on %q", fileName))
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("Comments on %q:", fileName))
	for _, c := range comments {
		lines = append(lines, fmt.Sprintf("  [%s] %s: %s", c.CreatedAt.Format("2006-01-02 15:04"), c.AgentKey, c.Content))
	}
	return NewResult(strings.Join(lines, "\n"))
}
