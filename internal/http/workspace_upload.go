package http

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels/media"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// WorkspaceUploadHandler handles file uploads to team workspaces.
type WorkspaceUploadHandler struct {
	teamStore store.TeamStore
	dataDir   string
	msgBus    *bus.MessageBus
}

// NewWorkspaceUploadHandler creates a new workspace upload handler.
func NewWorkspaceUploadHandler(teamStore store.TeamStore, dataDir string, msgBus *bus.MessageBus) *WorkspaceUploadHandler {
	return &WorkspaceUploadHandler{teamStore: teamStore, dataDir: dataDir, msgBus: msgBus}
}

// RegisterRoutes registers workspace file management endpoints.
func (h *WorkspaceUploadHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/teams/{teamId}/workspace/upload", requireAuth("", h.handleUpload))
	mux.HandleFunc("PUT /v1/teams/{teamId}/workspace/move", requireAuth("", h.handleMove))
}

func (h *WorkspaceUploadHandler) handleUpload(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	ctx := r.Context()

	if h.teamStore == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgTeamsNotConfigured)})
		return
	}

	// Parse team ID from path.
	teamID, err := uuid.Parse(r.PathValue("teamId"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid team_id"})
		return
	}

	// Team membership check (admins bypass — same pattern as RPC handlers).
	userID := store.UserIDFromContext(ctx)
	role := permissions.Role(store.RoleFromContext(ctx))
	if !permissions.HasMinRole(role, permissions.RoleAdmin) {
		if has, err := h.teamStore.HasTeamAccess(ctx, teamID, userID); err != nil || !has {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": i18n.T(locale, i18n.MsgPermissionDenied, "team workspace")})
			return
		}
	}

	// Determine workspace mode (shared vs isolated).
	chatID := r.URL.Query().Get("chat_id")
	team, err := h.teamStore.GetTeam(ctx, teamID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "team not found"})
		return
	}
	shared := tools.IsSharedWorkspace(team.Settings)
	if !shared && chatID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "chat_id")})
		return
	}
	if shared {
		chatID = "" // shared mode ignores chat_id
	}

	// Enforce file size limit at HTTP level.
	r.Body = http.MaxBytesReader(w, r.Body, tools.MaxFileSizeBytes)
	if err := r.ParseMultipartForm(tools.MaxFileSizeBytes); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgFileTooLarge)})
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgMissingFileField)})
		return
	}
	defer file.Close()

	// Sanitize filename.
	origName := filepath.Base(header.Filename)
	if origName == "." || origName == "/" || strings.Contains(origName, "..") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidFilename)})
		return
	}

	// Check blocked extensions.
	ext := strings.ToLower(filepath.Ext(origName))
	if tools.IsBlockedExtension(ext) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("file type %s is not allowed", ext)})
		return
	}

	// Resolve tenant-scoped workspace directory.
	tenantID := store.TenantIDFromContext(ctx)
	slug := store.TenantSlugFromContext(ctx)
	scopeDir := config.TenantTeamDir(h.dataDir, tenantID, slug, teamID)
	if chatID != "" {
		scopeDir = filepath.Join(scopeDir, chatID)
	}
	if err := os.MkdirAll(scopeDir, 0750); err != nil {
		slog.Error("workspace_upload: mkdir failed", "dir", scopeDir, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "failed to create directory")})
		return
	}

	// File quota check.
	entries, _ := os.ReadDir(scopeDir)
	fileCount := 0
	for _, e := range entries {
		if !e.IsDir() {
			fileCount++
		}
	}
	if fileCount >= tools.MaxFilesPerScope {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("workspace file limit reached (%d files)", tools.MaxFilesPerScope)})
		return
	}

	// Path boundary check (symlink escape prevention).
	diskPath := filepath.Clean(filepath.Join(scopeDir, origName))
	scopeReal, _ := filepath.EvalSymlinks(filepath.Clean(scopeDir))
	if scopeReal == "" {
		scopeReal = filepath.Clean(scopeDir)
	}
	if !strings.HasPrefix(diskPath, scopeReal+string(filepath.Separator)) && diskPath != scopeReal {
		slog.Warn("security.workspace_upload_escape", "path", origName, "scope", scopeReal)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidFilename)})
		return
	}

	// Write file to disk.
	out, err := os.Create(diskPath)
	if err != nil {
		slog.Error("workspace_upload: create file failed", "path", diskPath, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "failed to save file")})
		return
	}
	defer out.Close()

	written, err := io.Copy(out, file)
	if err != nil {
		os.Remove(diskPath)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "failed to save file")})
		return
	}

	// Broadcast workspace file change event for real-time UI updates.
	if h.msgBus != nil {
		bus.BroadcastForTenant(h.msgBus, protocol.EventWorkspaceFileChanged, tenantID, map[string]string{
			"team_id":   teamID.String(),
			"chat_id":   chatID,
			"file_name": origName,
			"action":    "created",
		})
	}

	mimeType := media.DetectMIMEType(origName)
	slog.Info("workspace_upload: file uploaded", "team", teamID, "chat_id", chatID, "file", origName, "size", written)

	writeJSON(w, http.StatusOK, map[string]any{
		"path":      diskPath,
		"filename":  origName,
		"size":      written,
		"mime_type": mimeType,
	})
}

// handleMove moves/renames a file within a team workspace.
// Query params: ?from=name&to=name&chat_id=xxx
func (h *WorkspaceUploadHandler) handleMove(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	ctx := r.Context()

	if h.teamStore == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgTeamsNotConfigured)})
		return
	}

	teamID, err := uuid.Parse(r.PathValue("teamId"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid team_id"})
		return
	}

	// Team membership check (admins bypass — same pattern as RPC handlers).
	userID := store.UserIDFromContext(ctx)
	role := permissions.Role(store.RoleFromContext(ctx))
	if !permissions.HasMinRole(role, permissions.RoleAdmin) {
		if has, err := h.teamStore.HasTeamAccess(ctx, teamID, userID); err != nil || !has {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": i18n.T(locale, i18n.MsgPermissionDenied, "team workspace")})
			return
		}
	}

	fromName := r.URL.Query().Get("from")
	toName := r.URL.Query().Get("to")
	if fromName == "" || toName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "from, to")})
		return
	}

	// Reject path traversal.
	if strings.Contains(fromName, "..") || strings.Contains(toName, "..") ||
		strings.Contains(fromName, "\\") || strings.Contains(toName, "\\") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidPath)})
		return
	}

	// Resolve workspace scope.
	chatID := r.URL.Query().Get("chat_id")
	team, err := h.teamStore.GetTeam(ctx, teamID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "team not found"})
		return
	}
	shared := tools.IsSharedWorkspace(team.Settings)
	if !shared && chatID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "chat_id")})
		return
	}
	if shared {
		chatID = ""
	}

	tenantID := store.TenantIDFromContext(ctx)
	slug := store.TenantSlugFromContext(ctx)
	scopeDir := config.TenantTeamDir(h.dataDir, tenantID, slug, teamID)
	if chatID != "" {
		scopeDir = filepath.Join(scopeDir, chatID)
	}

	// Resolve and validate source.
	srcPath := filepath.Clean(filepath.Join(scopeDir, fromName))
	scopeReal, _ := filepath.EvalSymlinks(filepath.Clean(scopeDir))
	if scopeReal == "" {
		scopeReal = filepath.Clean(scopeDir)
	}

	srcReal, err := filepath.EvalSymlinks(srcPath)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": fmt.Sprintf("file not found: %s", fromName)})
		return
	}
	if !strings.HasPrefix(srcReal, scopeReal+string(filepath.Separator)) && srcReal != scopeReal {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidPath)})
		return
	}

	// Resolve and validate destination.
	destPath := filepath.Clean(filepath.Join(scopeDir, toName))
	if !strings.HasPrefix(destPath, scopeReal+string(filepath.Separator)) && destPath != scopeReal {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidPath)})
		return
	}

	// Auto-create destination parent directories (security already validated above).
	destDir := filepath.Dir(destPath)
	if err := os.MkdirAll(destDir, 0750); err != nil {
		slog.Error("workspace_move: mkdir failed", "dir", destDir, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "failed to create directory")})
		return
	}

	// Prevent overwriting.
	if _, err := os.Stat(destPath); err == nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "a file with that name already exists at the destination"})
		return
	}

	if err := os.Rename(srcPath, destPath); err != nil {
		slog.Error("workspace_move: failed", "from", fromName, "to", toName, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "failed to move file")})
		return
	}

	// Broadcast change event.
	if h.msgBus != nil {
		bus.BroadcastForTenant(h.msgBus, protocol.EventWorkspaceFileChanged, tenantID, map[string]string{
			"team_id":   teamID.String(),
			"chat_id":   chatID,
			"file_name": filepath.Base(toName),
			"action":    "moved",
		})
	}

	slog.Info("workspace_move: file moved", "team", teamID, "from", fromName, "to", toName)
	writeJSON(w, http.StatusOK, map[string]any{
		"from": fromName,
		"to":   toName,
	})
}
