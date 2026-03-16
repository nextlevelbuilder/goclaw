package http

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
)

// StorageHandler provides HTTP endpoints for browsing and managing
// files inside the ~/.goclaw/ data directory.
// Skills directories are browsable (read-only) but deletion is blocked.
type StorageHandler struct {
	baseDir string // resolved absolute path to ~/.goclaw/
	token   string

	// sizeCache caches the total storage size for 60 minutes.
	sizeCache struct {
		mu       sync.Mutex
		total    int64
		files    int
		cachedAt time.Time
	}
}

// NewStorageHandler creates a handler for workspace storage management.
func NewStorageHandler(baseDir, token string) *StorageHandler {
	return &StorageHandler{baseDir: baseDir, token: token}
}

// RegisterRoutes registers storage management routes on the given mux.
func (h *StorageHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/storage/files", h.auth(h.handleList))
	mux.HandleFunc("GET /v1/storage/files/{path...}", h.auth(h.handleRead))
	mux.HandleFunc("DELETE /v1/storage/files/{path...}", h.auth(h.handleDelete))
	mux.HandleFunc("GET /v1/storage/size", h.auth(h.handleSize))
}

func (h *StorageHandler) auth(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth(h.token, "", next)
}

// protectedDirs are top-level directories where deletion is blocked
// (managed separately via the Skills page).
var protectedDirs = []string{"skills", "skills-store"}

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

	rootDir := h.baseDir
	if subPath != "" {
		rootDir = filepath.Join(h.baseDir, filepath.Clean(subPath))
		if !strings.HasPrefix(rootDir, h.baseDir) {
			slog.Warn("security.storage_escape", "resolved", rootDir, "root", h.baseDir)
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
		rel, _ := filepath.Rel(h.baseDir, path)

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
		"baseDir": h.baseDir,
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

	// Check cache
	h.sizeCache.mu.Lock()
	if !h.sizeCache.cachedAt.IsZero() && time.Since(h.sizeCache.cachedAt) < sizeCacheTTL {
		total := h.sizeCache.total
		files := h.sizeCache.files
		h.sizeCache.mu.Unlock()
		writeSizeEvent(w, flusher, map[string]any{"total": total, "files": files, "done": true, "cached": true})
		return
	}
	h.sizeCache.mu.Unlock()

	// Walk and stream progress
	var total int64
	var fileCount int
	lastFlush := time.Now()

	filepath.WalkDir(h.baseDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		// Check client disconnect
		if r.Context().Err() != nil {
			return filepath.SkipAll
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		rel, _ := filepath.Rel(h.baseDir, path)
		if skills.IsSystemArtifact(rel) {
			return nil
		}
		if info, err := d.Info(); err == nil {
			total += info.Size()
			fileCount++
		}
		// Emit progress every 50 files or 200ms
		if fileCount%50 == 0 || time.Since(lastFlush) > 200*time.Millisecond {
			writeSizeEvent(w, flusher, map[string]any{"current": total, "files": fileCount})
			lastFlush = time.Now()
		}
		return nil
	})

	// Update cache
	h.sizeCache.mu.Lock()
	h.sizeCache.total = total
	h.sizeCache.files = fileCount
	h.sizeCache.cachedAt = time.Now()
	h.sizeCache.mu.Unlock()

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

	absPath := filepath.Join(h.baseDir, filepath.Clean(relPath))
	if !strings.HasPrefix(absPath, h.baseDir+string(filepath.Separator)) {
		slog.Warn("security.storage_escape", "resolved", absPath, "root", h.baseDir)
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

	absPath := filepath.Join(h.baseDir, filepath.Clean(relPath))
	if !strings.HasPrefix(absPath, h.baseDir+string(filepath.Separator)) {
		slog.Warn("security.storage_escape", "resolved", absPath, "root", h.baseDir)
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
