package http

import (
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// StorageHandler provides HTTP endpoints for browsing and managing
// files inside the ~/.goclaw/ data directory.
// Skills directories are browsable (read-only) but deletion is blocked.
type StorageHandler struct {
	baseDir string // resolved absolute path to ~/.goclaw/
	token   string
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
}

func (h *StorageHandler) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.token != "" {
			provided := extractBearerToken(r)
			if !tokenMatch(provided, h.token) {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}
		}
		next(w, r)
	}
}

// protectedDirs are top-level directories where deletion is blocked
// (managed separately via the Skills page).
var protectedDirs = []string{"skills", "skills-store"}

func isProtectedPath(rel string) bool {
	top := rel
	if i := strings.IndexByte(rel, filepath.Separator); i >= 0 {
		top = rel[:i]
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

// handleList lists all files and directories under ~/.goclaw/.
// Optional query param ?path= scopes the listing to a subtree.
func (h *StorageHandler) handleList(w http.ResponseWriter, r *http.Request) {
	subPath := r.URL.Query().Get("path")
	if strings.Contains(subPath, "..") {
		slog.Warn("security.storage_traversal", "path", subPath)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path"})
		return
	}

	rootDir := h.baseDir
	if subPath != "" {
		rootDir = filepath.Join(h.baseDir, filepath.Clean(subPath))
		if !strings.HasPrefix(rootDir, h.baseDir) {
			slog.Warn("security.storage_escape", "resolved", rootDir, "root", h.baseDir)
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path"})
			return
		}
	}

	type fileEntry struct {
		Path      string `json:"path"`
		Name      string `json:"name"`
		IsDir     bool   `json:"isDir"`
		Size      int64  `json:"size"`
		TotalSize int64  `json:"totalSize"` // recursive size for directories
		Protected bool   `json:"protected"` // true if deletion is blocked
	}

	// Compute directory sizes via a two-pass approach:
	// 1. Walk and collect entries + accumulate sizes per dir path
	var entries []fileEntry
	dirSizes := make(map[string]int64)

	filepath.WalkDir(rootDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(h.baseDir, path)
		if rel == "." {
			return nil
		}

		// Skip symlinks
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}

		// Skip system artifacts
		if isSystemArtifact(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
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
				// Accumulate file size into all parent directories
				parent := filepath.Dir(rel)
				for parent != "." && parent != "" {
					dirSizes[parent] += info.Size()
					parent = filepath.Dir(parent)
				}
				// Also accumulate to root if listing from base
				if subPath == "" {
					dirSizes["."] += info.Size()
				}
			}
		}

		entry.Protected = isProtectedPath(rel)
		entries = append(entries, entry)
		return nil
	})

	// 2. Assign totalSize to directory entries
	for i := range entries {
		if entries[i].IsDir {
			entries[i].TotalSize = dirSizes[entries[i].Path]
		}
	}

	if entries == nil {
		entries = []fileEntry{}
	}

	// Calculate total size of the root being listed
	var totalSize int64
	if subPath == "" {
		totalSize = dirSizes["."]
	} else {
		rel, _ := filepath.Rel(h.baseDir, rootDir)
		totalSize = dirSizes[rel]
		// If rootDir is the listed subtree, also sum direct files
		for _, e := range entries {
			if !e.IsDir && filepath.Dir(e.Path) == rel {
				totalSize += e.Size
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"files":     entries,
		"totalSize": totalSize,
		"baseDir":   h.baseDir,
	})
}

// handleRead reads a single file's content by relative path.
func (h *StorageHandler) handleRead(w http.ResponseWriter, r *http.Request) {
	relPath := r.PathValue("path")
	if relPath == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path is required"})
		return
	}
	if strings.Contains(relPath, "..") {
		slog.Warn("security.storage_traversal", "path", relPath)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path"})
		return
	}

	absPath := filepath.Join(h.baseDir, filepath.Clean(relPath))
	if !strings.HasPrefix(absPath, h.baseDir+string(filepath.Separator)) {
		slog.Warn("security.storage_escape", "resolved", absPath, "root", h.baseDir)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path"})
		return
	}

	info, err := os.Lstat(absPath)
	if err != nil || info.IsDir() {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "file not found"})
		return
	}
	if info.Mode()&os.ModeSymlink != 0 {
		slog.Warn("security.storage_symlink", "path", absPath)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path"})
		return
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read file"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"content": string(data),
		"path":    relPath,
		"size":    info.Size(),
	})
}

// handleDelete removes a file or directory (recursively).
// Rejects deletion of the root dir and any path inside excluded directories.
func (h *StorageHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	relPath := r.PathValue("path")
	if relPath == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path is required"})
		return
	}
	if strings.Contains(relPath, "..") {
		slog.Warn("security.storage_traversal", "path", relPath)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path"})
		return
	}

	if isProtectedPath(relPath) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "cannot delete skills directories"})
		return
	}

	absPath := filepath.Join(h.baseDir, filepath.Clean(relPath))
	if !strings.HasPrefix(absPath, h.baseDir+string(filepath.Separator)) {
		slog.Warn("security.storage_escape", "resolved", absPath, "root", h.baseDir)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path"})
		return
	}

	// Verify path exists
	info, err := os.Lstat(absPath)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
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
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete"})
		return
	}

	slog.Info("storage.deleted", "path", relPath)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
