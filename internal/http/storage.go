package http

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// StorageHandler provides HTTP endpoints for browsing and managing
// files inside the ~/.goclaw/ data directory.
// Skills directories are browsable (read-only) but deletion is blocked.
// sizeCacheEntry holds a cached storage size calculation for one tenant.
type sizeCacheEntry struct {
	total    int64
	files    int
	cachedAt time.Time
}

type StorageHandler struct {
	baseDir string // global data dir (resolved absolute path to ~/.goclaw/)

	// sizeCache caches the total storage size per tenant for 60 minutes.
	sizeCache sync.Map // tenantBaseDir (string) → *sizeCacheEntry
}

// NewStorageHandler creates a handler for workspace storage management.
func NewStorageHandler(baseDir string) *StorageHandler {
	return &StorageHandler{baseDir: baseDir}
}

// RegisterRoutes registers storage management routes on the given mux.
func (h *StorageHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/storage/files", h.auth(h.handleList))
	mux.HandleFunc("GET /v1/storage/files/{path...}", h.auth(h.handleRead))
	mux.HandleFunc("DELETE /v1/storage/files/{path...}", h.auth(h.handleDelete))
	mux.HandleFunc("GET /v1/storage/size", h.auth(h.handleSize))
	mux.HandleFunc("POST /v1/storage/files", requireAuth(permissions.RoleAdmin, h.handleUpload))
	mux.HandleFunc("PUT /v1/storage/move", requireAuth(permissions.RoleAdmin, h.handleMove))
}

func (h *StorageHandler) auth(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth("", next)
}

// tenantBaseDir resolves the data directory scoped to the requesting tenant.
// Master tenant returns the global baseDir (backward compat).
func (h *StorageHandler) tenantBaseDir(r *http.Request) string {
	tid := store.TenantIDFromContext(r.Context())
	slug := store.TenantSlugFromContext(r.Context())
	return config.TenantDataDir(h.baseDir, tid, slug)
}

// protectedDirs are top-level directories where upload, move, and deletion are blocked.
// These are system-managed: skills (managed via Skills page), media (managed via media handler),
// tenants (tenant isolation root — each tenant's data is scoped internally).
var protectedDirs = []string{"skills", "skills-store", "media", "tenants"}

func isProtectedPath(rel string) bool {
	top := rel
	if before, _, ok := strings.Cut(rel, "/"); ok {
		top = before
	}
	// Also handle forward slash on all platforms
	if i := strings.IndexByte(top, '/'); i >= 0 {
		top = top[:i]
	}
	for _, d := range protectedDirs {
		if strings.EqualFold(top, d) {
			return true
		}
	}
	return false
}

// handleList lists files and directories under ~/.goclaw/ with depth limiting.
// Query params:
//   - ?path=  scopes the listing to a subtree
//   - ?depth= max depth to walk (default 3, max 20)
func (h *StorageHandler) handleList(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	subPath := r.URL.Query().Get("path")
	if strings.Contains(subPath, "..") {
		slog.Warn("security.storage_traversal", "path", subPath)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidPath)})
		return
	}

	maxDepth := 3
	if d := r.URL.Query().Get("depth"); d != "" {
		if v, err := strconv.Atoi(d); err == nil && v >= 1 && v <= 20 {
			maxDepth = v
		}
	}

	base := h.tenantBaseDir(r)
	rootDir := base
	if subPath != "" {
		rootDir = filepath.Join(base, filepath.Clean(subPath))
		if !strings.HasPrefix(rootDir, base) {
			slog.Warn("security.storage_escape", "resolved", rootDir, "root", base)
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidPath)})
			return
		}
	}

	type fileEntry struct {
		Path        string `json:"path"`
		Name        string `json:"name"`
		IsDir       bool   `json:"isDir"`
		Size        int64  `json:"size"`
		HasChildren bool   `json:"hasChildren,omitempty"`
		Protected   bool   `json:"protected"`
	}

	var entries []fileEntry

	filepath.WalkDir(rootDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if path == rootDir {
			return nil
		}
		rel, _ := filepath.Rel(base, path)

		// Skip symlinks
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}

		// Skip system artifacts
		if skills.IsSystemArtifact(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Calculate depth relative to rootDir
		relToRoot, _ := filepath.Rel(rootDir, path)
		depth := strings.Count(relToRoot, string(filepath.Separator)) + 1

		// Beyond depth boundary: record the dir (with hasChildren hint) but don't descend.
		if d.IsDir() && depth > maxDepth {
			e := fileEntry{
				Path:      rel,
				Name:      d.Name(),
				IsDir:     true,
				Protected: isProtectedPath(rel),
			}
			if dirEntries, err := os.ReadDir(path); err == nil && len(dirEntries) > 0 {
				e.HasChildren = true
			}
			entries = append(entries, e)
			return filepath.SkipDir
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

		// For directories at max depth, check if they have children
		if d.IsDir() && depth == maxDepth {
			if dirEntries, err := os.ReadDir(path); err == nil && len(dirEntries) > 0 {
				entry.HasChildren = true
			}
		}

		entry.Protected = isProtectedPath(rel)
		entries = append(entries, entry)
		return nil
	})

	if entries == nil {
		entries = []fileEntry{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"files":   entries,
		"baseDir": base,
	})
}

// sizeCacheTTL is how long storage size calculations are cached.
const sizeCacheTTL = 60 * time.Minute

// handleSize streams the total storage size via SSE.
// Cached for 60 minutes; returns cached result immediately if valid.
func (h *StorageHandler) handleSize(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		locale := extractLocale(r)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgStreamingNotSupported)})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	sizeBase := h.tenantBaseDir(r)

	// Check per-tenant cache
	if entry, ok := h.sizeCache.Load(sizeBase); ok {
		ce := entry.(*sizeCacheEntry)
		if time.Since(ce.cachedAt) < sizeCacheTTL {
			writeSizeEvent(w, flusher, map[string]any{"total": ce.total, "files": ce.files, "done": true, "cached": true})
			return
		}
	}

	// Walk and stream progress
	var total int64
	var fileCount int
	lastFlush := time.Now()

	filepath.WalkDir(sizeBase, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if r.Context().Err() != nil {
			return filepath.SkipAll
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		rel, _ := filepath.Rel(sizeBase, path)
		if skills.IsSystemArtifact(rel) {
			return nil
		}
		if info, err := d.Info(); err == nil {
			total += info.Size()
			fileCount++
		}
		if fileCount%50 == 0 || time.Since(lastFlush) > 200*time.Millisecond {
			writeSizeEvent(w, flusher, map[string]any{"current": total, "files": fileCount})
			lastFlush = time.Now()
		}
		return nil
	})

	// Update per-tenant cache
	h.sizeCache.Store(sizeBase, &sizeCacheEntry{total: total, files: fileCount, cachedAt: time.Now()})

	// Send final event
	writeSizeEvent(w, flusher, map[string]any{"total": total, "files": fileCount, "done": true, "cached": false})
}

func writeSizeEvent(w http.ResponseWriter, flusher http.Flusher, data map[string]any) {
	jsonData, _ := json.Marshal(data)
	fmt.Fprintf(w, "data: %s\n\n", jsonData)
	flusher.Flush()
}

// handleRead reads a single file's content by relative path.
func (h *StorageHandler) handleRead(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	relPath := r.PathValue("path")
	if relPath == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "path")})
		return
	}
	if strings.Contains(relPath, "..") {
		slog.Warn("security.storage_traversal", "path", relPath)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidPath)})
		return
	}

	readBase := h.tenantBaseDir(r)
	absPath := filepath.Join(readBase, filepath.Clean(relPath))
	if !strings.HasPrefix(absPath, readBase+string(filepath.Separator)) {
		slog.Warn("security.storage_escape", "resolved", absPath, "root", readBase)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidPath)})
		return
	}

	info, err := os.Lstat(absPath)
	if err != nil || info.IsDir() {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgFileNotFound)})
		return
	}
	if info.Mode()&os.ModeSymlink != 0 {
		slog.Warn("security.storage_symlink", "path", absPath)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidPath)})
		return
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToReadFile)})
		return
	}

	// Raw mode: serve the file with its native content type (for images, downloads, etc.)
	if r.URL.Query().Get("raw") == "true" {
		ct := mime.TypeByExtension(filepath.Ext(absPath))
		if ct == "" {
			ct = http.DetectContentType(data)
		}
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Cache-Control", "private, max-age=300")
		if r.URL.Query().Get("download") == "true" {
			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(absPath)))
		}
		w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
		w.Write(data)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"content": string(data),
		"path":    relPath,
		"size":    info.Size(),
	})
}

// handleDelete removes a file or directory (recursively).
// Rejects deletion of the root dir and any path inside excluded directories.
func (h *StorageHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	relPath := r.PathValue("path")
	if relPath == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "path")})
		return
	}
	if strings.Contains(relPath, "..") {
		slog.Warn("security.storage_traversal", "path", relPath)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidPath)})
		return
	}

	if isProtectedPath(relPath) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": i18n.T(locale, i18n.MsgCannotDeleteSkillsDir)})
		return
	}

	delBase := h.tenantBaseDir(r)
	absPath := filepath.Join(delBase, filepath.Clean(relPath))
	if !strings.HasPrefix(absPath, delBase+string(filepath.Separator)) {
		slog.Warn("security.storage_escape", "resolved", absPath, "root", delBase)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidPath)})
		return
	}

	// Verify path exists
	info, err := os.Lstat(absPath)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "path", relPath)})
		return
	}

	if info.Mode()&os.ModeSymlink != 0 {
		// Remove symlink itself, not target
		err = os.Remove(absPath)
	} else if info.IsDir() {
		err = os.RemoveAll(absPath)
	} else {
		err = os.Remove(absPath)
	}

	if err != nil {
		slog.Error("storage.delete_failed", "path", absPath, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToDeleteFile)})
		return
	}

	slog.Info("storage.deleted", "path", relPath)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleUpload uploads a file into the storage data directory.
// Admin-only. Rejects uploads into protected directories (skills, skills-store).
func (h *StorageHandler) handleUpload(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)

	subPath := r.URL.Query().Get("path")
	if strings.Contains(subPath, "..") {
		slog.Warn("security.storage_upload_traversal", "path", subPath)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidPath)})
		return
	}

	// Reject upload into protected directories.
	if subPath != "" && isProtectedPath(subPath) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": i18n.T(locale, i18n.MsgCannotDeleteSkillsDir)})
		return
	}

	// Enforce file size limit.
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

	// Resolve target directory within tenant-scoped data dir.
	base := h.tenantBaseDir(r)
	targetDir := base
	if subPath != "" {
		targetDir = filepath.Join(base, filepath.Clean(subPath))
		if !strings.HasPrefix(targetDir, base) {
			slog.Warn("security.storage_upload_escape", "resolved", targetDir, "root", base)
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidPath)})
			return
		}
	}

	if err := os.MkdirAll(targetDir, 0750); err != nil {
		slog.Error("storage.upload_mkdir_failed", "dir", targetDir, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "failed to create directory")})
		return
	}

	diskPath := filepath.Join(targetDir, origName)

	// Symlink escape check on resolved path.
	realTarget, _ := filepath.EvalSymlinks(targetDir)
	if realTarget == "" {
		realTarget = targetDir
	}
	realBase, _ := filepath.EvalSymlinks(base)
	if realBase == "" {
		realBase = base
	}
	if !strings.HasPrefix(realTarget, realBase) {
		slog.Warn("security.storage_upload_symlink_escape", "target", realTarget, "base", realBase)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidPath)})
		return
	}

	// Write file.
	out, err := os.Create(diskPath)
	if err != nil {
		slog.Error("storage.upload_create_failed", "path", diskPath, "error", err)
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

	// Invalidate size cache for this tenant.
	h.sizeCache.Delete(base)

	relPath := origName
	if subPath != "" {
		relPath = filepath.Join(subPath, origName)
	}

	slog.Info("storage.uploaded", "path", relPath, "size", written)
	writeJSON(w, http.StatusOK, map[string]any{
		"path":     relPath,
		"filename": origName,
		"size":     written,
	})
}

// handleMove moves/renames a file within the storage data directory.
// Admin-only. Rejects moves involving protected directories.
// Query params: ?from=relPath&to=relPath
func (h *StorageHandler) handleMove(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)

	fromRel := r.URL.Query().Get("from")
	toRel := r.URL.Query().Get("to")
	if fromRel == "" || toRel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "from, to")})
		return
	}

	// Reject path traversal in both paths.
	if strings.Contains(fromRel, "..") || strings.Contains(toRel, "..") {
		slog.Warn("security.storage_move_traversal", "from", fromRel, "to", toRel)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidPath)})
		return
	}

	// Reject moves involving protected directories.
	if isProtectedPath(fromRel) || isProtectedPath(toRel) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": i18n.T(locale, i18n.MsgCannotDeleteSkillsDir)})
		return
	}

	base := h.tenantBaseDir(r)

	// Resolve and validate source path.
	srcAbs := filepath.Join(base, filepath.Clean(fromRel))
	if !strings.HasPrefix(srcAbs, base+string(filepath.Separator)) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidPath)})
		return
	}
	srcReal, err := filepath.EvalSymlinks(srcAbs)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgFileNotFound)})
		return
	}
	baseReal, _ := filepath.EvalSymlinks(base)
	if baseReal == "" {
		baseReal = base
	}
	if !strings.HasPrefix(srcReal, baseReal+string(filepath.Separator)) {
		slog.Warn("security.storage_move_src_escape", "resolved", srcReal, "base", baseReal)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidPath)})
		return
	}

	// Resolve and validate destination path.
	destAbs := filepath.Join(base, filepath.Clean(toRel))
	if !strings.HasPrefix(destAbs, base+string(filepath.Separator)) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidPath)})
		return
	}
	// Ensure destination parent exists.
	destDir := filepath.Dir(destAbs)
	destDirReal, _ := filepath.EvalSymlinks(destDir)
	if destDirReal == "" {
		destDirReal = destDir
	}
	if !strings.HasPrefix(destDirReal+string(filepath.Separator), baseReal+string(filepath.Separator)) {
		slog.Warn("security.storage_move_dest_escape", "resolved", destDirReal, "base", baseReal)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidPath)})
		return
	}
	if err := os.MkdirAll(destDir, 0750); err != nil {
		slog.Error("storage.move_mkdir_failed", "dir", destDir, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "failed to create directory")})
		return
	}

	// Prevent overwriting existing file.
	if _, err := os.Stat(destAbs); err == nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "a file with that name already exists at the destination"})
		return
	}

	// Atomic move.
	if err := os.Rename(srcAbs, destAbs); err != nil {
		slog.Error("storage.move_failed", "from", fromRel, "to", toRel, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "failed to move file")})
		return
	}

	slog.Info("storage.moved", "from", fromRel, "to", toRel)
	writeJSON(w, http.StatusOK, map[string]any{
		"from": fromRel,
		"to":   toRel,
	})
}
