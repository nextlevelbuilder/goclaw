package http

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

const maxManagedToolUploadSize = 20 << 20 // 20 MB
const maxManagedToolWriteSize = 10 << 20  // 10 MB

// ManagedToolsHandler handles managed tool management HTTP endpoints.
type ManagedToolsHandler struct {
	tools   *pg.PGManagedToolStore
	baseDir string
	token   string
	msgBus  *bus.MessageBus
}

// NewManagedToolsHandler creates a handler for managed tool management endpoints.
func NewManagedToolsHandler(tools *pg.PGManagedToolStore, baseDir, token string, msgBus *bus.MessageBus) *ManagedToolsHandler {
	return &ManagedToolsHandler{tools: tools, baseDir: baseDir, token: token, msgBus: msgBus}
}

// emitCacheInvalidate broadcasts a cache invalidation event if msgBus is set.
func (h *ManagedToolsHandler) emitCacheInvalidate(kind, key string) {
	if h.msgBus == nil {
		return
	}
	h.msgBus.Broadcast(bus.Event{
		Name:    protocol.EventCacheInvalidate,
		Payload: bus.CacheInvalidatePayload{Kind: kind, Key: key},
	})
}

// RegisterRoutes registers all managed tool management routes on the given mux.
func (h *ManagedToolsHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/managed-tools", h.authMiddleware(h.handleList))
	mux.HandleFunc("POST /v1/managed-tools/upload", h.authMiddleware(h.handleUpload))
	mux.HandleFunc("GET /v1/managed-tools/{id}", h.authMiddleware(h.handleGet))
	mux.HandleFunc("PUT /v1/managed-tools/{id}", h.authMiddleware(h.handleUpdate))
	mux.HandleFunc("DELETE /v1/managed-tools/{id}", h.authMiddleware(h.handleDelete))
	mux.HandleFunc("POST /v1/managed-tools/{id}/toggle", h.authMiddleware(h.handleToggle))
	mux.HandleFunc("GET /v1/managed-tools/{id}/files", h.authMiddleware(h.handleListFiles))
	mux.HandleFunc("GET /v1/managed-tools/{id}/files/{path...}", h.authMiddleware(h.handleReadFile))
	mux.HandleFunc("PUT /v1/managed-tools/{id}/files/{path...}", h.authMiddleware(h.handleWriteFile))
	mux.HandleFunc("GET /v1/managed-tools/{id}/versions", h.authMiddleware(h.handleListVersions))
}

func (h *ManagedToolsHandler) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.token != "" {
			if extractBearerToken(r) != h.token {
				locale := extractLocale(r)
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": i18n.T(locale, i18n.MsgUnauthorized)})
				return
			}
		}
		userID := extractUserID(r)
		ctx := store.WithLocale(r.Context(), extractLocale(r))
		if userID != "" {
			ctx = store.WithUserID(ctx, userID)
		}
		r = r.WithContext(ctx)
		next(w, r)
	}
}

func (h *ManagedToolsHandler) handleList(w http.ResponseWriter, r *http.Request) {
	tools := h.tools.ListManagedTools()
	writeJSON(w, http.StatusOK, map[string]any{"tools": tools})
}

func (h *ManagedToolsHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	id := r.PathValue("id")
	uid, err := uuid.Parse(id)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "managed tool")})
		return
	}
	tool, ok := h.tools.GetManagedToolByID(uid)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "managed tool", id)})
		return
	}
	writeJSON(w, http.StatusOK, tool)
}

func (h *ManagedToolsHandler) handleUpdate(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "managed tool")})
		return
	}

	var updates map[string]any
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return
	}
	// Prevent changing sensitive fields (use /toggle endpoint for enabled)
	delete(updates, "id")
	delete(updates, "owner_id")
	delete(updates, "file_path")
	delete(updates, "is_system")
	delete(updates, "enabled")

	if err := h.tools.UpdateManagedTool(id, updates); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	h.tools.BumpVersion()
	h.emitCacheInvalidate("managed_tools", idStr)
	emitAudit(h.msgBus, r, "managed-tool.updated", "managed_tool", idStr)
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

func (h *ManagedToolsHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "managed tool")})
		return
	}

	if err := h.tools.DeleteManagedTool(id); err != nil {
		if err.Error() == "cannot delete system managed tool" {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "cannot delete system managed tool"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	h.tools.BumpVersion()
	h.emitCacheInvalidate("managed_tools", idStr)
	emitAudit(h.msgBus, r, "managed-tool.deleted", "managed_tool", idStr)
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

// handleToggle enables or disables a managed tool.
// Body: {"enabled": bool}
func (h *ManagedToolsHandler) handleToggle(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "managed tool")})
		return
	}

	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return
	}

	if err := h.tools.ToggleManagedTool(id, body.Enabled); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	newStatus := ""
	if body.Enabled {
		newStatus = "active"
		_ = h.tools.UpdateManagedTool(id, map[string]any{"status": newStatus})
	}

	h.tools.BumpVersion()
	h.emitCacheInvalidate("managed_tools", idStr)
	emitAudit(h.msgBus, r, "managed-tool.toggled", "managed_tool", idStr)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "enabled": body.Enabled, "status": newStatus})
}

// handleListVersions returns all available version numbers for a managed tool.
func (h *ManagedToolsHandler) handleListVersions(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "managed tool")})
		return
	}

	_, slug, currentVersion, ok := h.tools.GetManagedToolFilePath(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "managed tool", id.String())})
		return
	}

	slugDir := filepath.Join(h.baseDir, slug)
	entries, err := os.ReadDir(slugDir)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"versions": []int{currentVersion},
			"current":  currentVersion,
		})
		return
	}

	var versions []int
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		v, err := strconv.Atoi(e.Name())
		if err != nil || v < 1 {
			continue
		}
		versions = append(versions, v)
	}
	if len(versions) == 0 {
		versions = []int{currentVersion}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"versions": versions,
		"current":  currentVersion,
	})
}

// handleListFiles returns all files in a managed tool version directory.
func (h *ManagedToolsHandler) handleListFiles(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "managed tool")})
		return
	}

	_, slug, currentVersion, ok := h.tools.GetManagedToolFilePath(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "managed tool", id.String())})
		return
	}

	version := currentVersion
	if v := r.URL.Query().Get("version"); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil || parsed < 1 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidVersion)})
			return
		}
		version = parsed
	}

	versionDir := filepath.Join(h.baseDir, slug, strconv.Itoa(version))
	if _, err := os.Stat(versionDir); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgVersionNotFound)})
		return
	}

	type fileEntry struct {
		Path  string `json:"path"`
		Name  string `json:"name"`
		IsDir bool   `json:"isDir"`
		Size  int64  `json:"size"`
	}

	var files []fileEntry
	filepath.WalkDir(versionDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(versionDir, path)
		if rel == "." {
			return nil
		}
		// Skip symlinks — prevent escape from tool directory
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		entry := fileEntry{
			Path:  rel,
			Name:  d.Name(),
			IsDir: d.IsDir(),
		}
		if !d.IsDir() {
			if info, err := d.Info(); err == nil {
				entry.Size = info.Size()
			}
		}
		files = append(files, entry)
		return nil
	})

	if files == nil {
		files = []fileEntry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": files})
}

// handleReadFile reads a single file from a managed tool version directory.
func (h *ManagedToolsHandler) handleReadFile(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "managed tool")})
		return
	}

	relPath := r.PathValue("path")
	if relPath == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "path")})
		return
	}
	if strings.Contains(relPath, "..") {
		slog.Warn("security.managed_tool_files_traversal", "path", relPath)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidPath)})
		return
	}

	_, slug, currentVersion, ok := h.tools.GetManagedToolFilePath(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "managed tool", id.String())})
		return
	}

	version := currentVersion
	if v := r.URL.Query().Get("version"); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil || parsed < 1 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidVersion)})
			return
		}
		version = parsed
	}

	versionDir := filepath.Join(h.baseDir, slug, strconv.Itoa(version))
	absPath := filepath.Join(versionDir, filepath.Clean(relPath))

	// Verify resolved path is within the version directory
	if !strings.HasPrefix(absPath, versionDir+string(filepath.Separator)) {
		slog.Warn("security.managed_tool_files_escape", "resolved", absPath, "root", versionDir)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidPath)})
		return
	}

	// Use Lstat to detect symlinks — reject them to prevent directory escape
	info, err := os.Lstat(absPath)
	if err != nil || info.IsDir() {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgFileNotFound)})
		return
	}
	if info.Mode()&os.ModeSymlink != 0 {
		slog.Warn("security.managed_tool_files_symlink", "path", absPath)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidPath)})
		return
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToReadFile)})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"content": string(data),
		"path":    relPath,
		"size":    info.Size(),
	})
}

// handleWriteFile writes content to a file in a managed tool version directory.
func (h *ManagedToolsHandler) handleWriteFile(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "managed tool")})
		return
	}

	relPath := r.PathValue("path")
	if relPath == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "path")})
		return
	}
	if strings.Contains(relPath, "..") {
		slog.Warn("security.managed_tool_files_traversal", "path", relPath)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidPath)})
		return
	}

	_, slug, currentVersion, ok := h.tools.GetManagedToolFilePath(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "managed tool", id.String())})
		return
	}

	version := currentVersion
	if v := r.URL.Query().Get("version"); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil || parsed < 1 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidVersion)})
			return
		}
		version = parsed
	}

	versionDir := filepath.Join(h.baseDir, slug, strconv.Itoa(version))
	absPath := filepath.Join(versionDir, filepath.Clean(relPath))

	// Verify resolved path is within the version directory
	if !strings.HasPrefix(absPath, versionDir+string(filepath.Separator)) {
		slog.Warn("security.managed_tool_files_escape", "resolved", absPath, "root", versionDir)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidPath)})
		return
	}

	// Read request body (limit 10MB)
	r.Body = http.MaxBytesReader(w, r.Body, maxManagedToolWriteSize)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, "request body too large or unreadable")})
		return
	}

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "failed to create directory")})
		return
	}

	if err := os.WriteFile(absPath, data, 0644); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "failed to write file")})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
