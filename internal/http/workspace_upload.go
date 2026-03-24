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

// RegisterRoutes registers the workspace upload endpoint.
func (h *WorkspaceUploadHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/teams/{teamId}/workspace/upload", requireAuth("", h.handleUpload))
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

	// Team membership check.
	userID := store.UserIDFromContext(ctx)
	if has, err := h.teamStore.HasTeamAccess(ctx, teamID, userID); err != nil || !has {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": i18n.T(locale, i18n.MsgPermissionDenied, "team workspace")})
		return
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
