package tools

import (
	"context"
	"fmt"
	"log/slog"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// WorkspaceInterceptor validates writes and broadcasts events for team workspace files.
// When no team context is active (ToolTeamIDFromCtx returns ""), all methods are no-ops.
type WorkspaceInterceptor struct {
	teamMgr *TeamToolManager
}

func NewWorkspaceInterceptor(mgr *TeamToolManager) *WorkspaceInterceptor {
	return &WorkspaceInterceptor{teamMgr: mgr}
}

// HandleWrite validates a file write in team workspace context.
// Returns (true, nil) if the write should be treated as a delete (empty content).
// Returns (false, nil) to proceed with normal write.
// Returns (_, error) to block the write.
func (w *WorkspaceInterceptor) HandleWrite(ctx context.Context, path string, content string) (isDelete bool, err error) {
	if w == nil {
		return false, nil
	}
	teamIDStr := ToolTeamIDFromCtx(ctx)
	if teamIDStr == "" {
		return false, nil // Not in team context
	}

	// Only apply team validation when path is inside the team workspace.
	teamWs := ToolTeamWorkspaceFromCtx(ctx)
	if teamWs == "" || !strings.HasPrefix(filepath.Clean(path), filepath.Clean(teamWs)) {
		return false, nil // Write is to agent's own workspace, not team workspace
	}

	// Resolve team and role for RBAC. Fail-open: if resolution fails (DB issue,
	// corrupt cache), allow the write but log a warning for observability.
	team, agentID, err := w.teamMgr.resolveTeam(ctx)
	if err != nil {
		slog.Warn("workspace: team resolution failed, skipping validation", "team", teamIDStr, "error", err)
		return false, nil
	}
	role, err := w.teamMgr.resolveTeamRole(ctx, team, agentID)
	if err != nil {
		slog.Warn("workspace: role resolution failed, skipping validation", "team", teamIDStr, "error", err)
		return false, nil
	}

	// Empty content = delete.
	if content == "" {
		if role == store.TeamRoleReviewer {
			return false, fmt.Errorf("reviewers cannot delete workspace files")
		}
		return true, nil
	}

	// RBAC: reviewer cannot write.
	if role == store.TeamRoleReviewer {
		return false, fmt.Errorf("reviewers cannot write to the workspace")
	}

	// Blocked extensions.
	ext := strings.ToLower(filepath.Ext(path))
	if blockedExtensions[ext] {
		return false, fmt.Errorf("executable file type %q is not allowed", ext)
	}

	// File size limit (10MB).
	if len(content) > maxFileSizeBytes {
		return false, fmt.Errorf("file exceeds max size (10MB)")
	}

	// Quota: count files in team workspace scope.
	wsDir := teamWs
	if wsDir != "" {
		entries, err := os.ReadDir(wsDir)
		if err != nil {
			slog.Warn("workspace: quota check ReadDir failed", "dir", wsDir, "error", err)
		}
		fileCount := 0
		for _, e := range entries {
			if !e.IsDir() {
				fileCount++
			}
		}
		// Only check when creating new file (not updating existing).
		if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
			if fileCount >= maxFilesPerScope {
				return false, fmt.Errorf("workspace file limit reached (%d/%d)", fileCount, maxFilesPerScope)
			}
		}
	}

	return false, nil
}

// AfterWrite broadcasts a workspace file change event and auto-tracks
// file writes as task attachments when a team task ID is in context.
func (w *WorkspaceInterceptor) AfterWrite(ctx context.Context, path string, action string) {
	if w == nil {
		return
	}
	teamIDStr := ToolTeamIDFromCtx(ctx)
	if teamIDStr == "" {
		return
	}
	// Only process writes inside team workspace.
	teamWs := ToolTeamWorkspaceFromCtx(ctx)
	if teamWs == "" || !strings.HasPrefix(filepath.Clean(path), filepath.Clean(teamWs)) {
		return
	}

	fileName := filepath.Base(path)
	chatID := ToolChatIDFromCtx(ctx)
	if chatID == "" {
		chatID = store.UserIDFromContext(ctx)
	}

	// Broadcast workspace file change event for real-time UI updates.
	w.teamMgr.broadcastTeamEvent(protocol.EventWorkspaceFileChanged, map[string]string{
		"team_id":   teamIDStr,
		"channel":   "",
		"chat_id":   chatID,
		"file_name": fileName,
		"action":    action,
	})
	slog.Debug("workspace: file changed", "team", teamIDStr, "file", fileName, "action", action)

	// Auto-track file as task attachment when team task context is present.
	taskIDStr := TeamTaskIDFromCtx(ctx)
	if taskIDStr == "" {
		return
	}
	teamID, err := uuid.Parse(teamIDStr)
	if err != nil {
		return
	}
	taskID, err := uuid.Parse(taskIDStr)
	if err != nil {
		return
	}

	// Compute relative path from workspace root for storage.
	relPath, err := filepath.Rel(filepath.Clean(teamWs), filepath.Clean(path))
	if err != nil {
		relPath = fileName
	}

	switch action {
	case "write":
		// Get file size and detect MIME type.
		var fileSize int64
		if info, err := os.Stat(path); err == nil {
			fileSize = info.Size()
		}
		mimeType := mimeFromExt(filepath.Ext(path))

		// Resolve agent ID for created_by_agent_id.
		_, agentID, _ := w.teamMgr.resolveTeam(ctx)

		att := &store.TeamTaskAttachmentData{
			TaskID:           taskID,
			TeamID:           teamID,
			ChatID:           chatID,
			Path:             relPath,
			FileSize:         fileSize,
			MimeType:         mimeType,
			CreatedByAgentID: &agentID,
		}
		if err := w.teamMgr.teamStore.AttachFileToTask(ctx, att); err != nil {
			slog.Warn("workspace: auto-attach failed", "task_id", taskIDStr, "path", relPath, "error", err)
		} else {
			slog.Debug("workspace: auto-attached file to task", "task_id", taskIDStr, "path", relPath)
		}

	case "delete":
		if err := w.teamMgr.teamStore.DetachFileFromTask(ctx, taskID, relPath); err != nil {
			slog.Warn("workspace: auto-detach failed", "task_id", taskIDStr, "path", relPath, "error", err)
		}
	}
}

// mimeFromExt returns a MIME type for a file extension using the stdlib registry.
func mimeFromExt(ext string) string {
	if t := mime.TypeByExtension(ext); t != "" {
		return t
	}
	return "application/octet-stream"
}
