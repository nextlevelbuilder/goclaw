package http

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	maxFileSize  = 500 * 1024 // 500 KB
	maxTreeDepth = 6
)

// TreeNode represents a file or directory in the project tree.
// JSON shape matches the frontend TreeNode type in ui/web/src/lib/file-helpers.ts.
type TreeNode struct {
	Name      string     `json:"name"`
	Path      string     `json:"path"`
	IsDir     bool       `json:"isDir"`
	Size      int64      `json:"size"`
	TotalSize *int64     `json:"totalSize,omitempty"`
	Protected *bool      `json:"protected,omitempty"`
	Children  []TreeNode `json:"children"`
}

// skipDirs are directory names to exclude from the file tree.
var skipDirs = map[string]bool{
	"node_modules": true,
	".git":         true,
}

// skipFiles are file names to exclude from the file tree.
var skipFiles = map[string]bool{
	"bun.lockb": true,
}

func (h *MCPBuilderHandler) handleListFiles(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	userID := store.UserIDFromContext(r.Context())
	if userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgUserIDRequired)})
		return
	}

	projectID := r.PathValue("id")
	if projectID == "" || !isValidSlug(projectID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidSlug, "project id")})
		return
	}

	projectDir := filepath.Join(h.projectsRoot, userID, projectID)

	// Path traversal check
	cleanDir := filepath.Clean(projectDir)
	if !strings.HasPrefix(cleanDir, filepath.Clean(h.projectsRoot)) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidPath)})
		return
	}

	// Verify project exists
	if _, err := os.Stat(cleanDir); os.IsNotExist(err) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "project", projectID)})
		return
	}

	tree := buildTreeNode(cleanDir, "", 0)
	writeJSON(w, http.StatusOK, map[string]any{"tree": tree})
}

func (h *MCPBuilderHandler) handleGetFile(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	userID := store.UserIDFromContext(r.Context())
	if userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgUserIDRequired)})
		return
	}

	projectID := r.PathValue("id")
	if projectID == "" || !isValidSlug(projectID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidSlug, "project id")})
		return
	}

	filePath := r.URL.Query().Get("path")
	if filePath == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "path")})
		return
	}

	projectDir := filepath.Clean(filepath.Join(h.projectsRoot, userID, projectID))
	fullPath := filepath.Clean(filepath.Join(projectDir, filePath))

	// Path traversal check: resolved path must be under the project directory
	if !strings.HasPrefix(fullPath, projectDir+string(filepath.Separator)) && fullPath != projectDir {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidPath)})
		return
	}

	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgFileNotFound)})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToReadFile)})
		return
	}

	if info.IsDir() {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidPath)})
		return
	}

	if info.Size() > maxFileSize {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgFileTooLarge)})
		return
	}

	data, err := os.ReadFile(fullPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToReadFile)})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"content": string(data),
		"size":    info.Size(),
	})
}

// buildTreeNode recursively walks a directory and returns a sorted list of TreeNodes.
// Directories come first, then files, both sorted alphabetically.
// Skips node_modules, .git, bun.lockb, and respects max depth.
func buildTreeNode(baseDir, relPath string, depth int) []TreeNode {
	if depth >= maxTreeDepth {
		return nil
	}

	currentDir := baseDir
	if relPath != "" {
		currentDir = filepath.Join(baseDir, relPath)
	}

	entries, err := os.ReadDir(currentDir)
	if err != nil {
		return nil
	}

	var dirs, files []TreeNode

	for _, entry := range entries {
		name := entry.Name()

		if entry.IsDir() {
			if skipDirs[name] {
				continue
			}

			childRel := name
			if relPath != "" {
				childRel = relPath + "/" + name
			}

			children := buildTreeNode(baseDir, childRel, depth+1)
			totalSize := computeTotalSize(children)

			dirs = append(dirs, TreeNode{
				Name:      name,
				Path:      childRel,
				IsDir:     true,
				Size:      0,
				TotalSize: &totalSize,
				Children:  children,
			})
		} else {
			if skipFiles[name] {
				continue
			}

			childRel := name
			if relPath != "" {
				childRel = relPath + "/" + name
			}

			var size int64
			if info, err := entry.Info(); err == nil {
				size = info.Size()
			}

			files = append(files, TreeNode{
				Name:     name,
				Path:     childRel,
				IsDir:    false,
				Size:     size,
				Children: []TreeNode{},
			})
		}
	}

	// Sort directories and files alphabetically
	sort.Slice(dirs, func(i, j int) bool {
		return dirs[i].Name < dirs[j].Name
	})
	sort.Slice(files, func(i, j int) bool {
		return files[i].Name < files[j].Name
	})

	// Directories first, then files
	result := make([]TreeNode, 0, len(dirs)+len(files))
	result = append(result, dirs...)
	result = append(result, files...)
	return result
}

// computeTotalSize sums the sizes of all nodes in the tree recursively.
func computeTotalSize(nodes []TreeNode) int64 {
	var total int64
	for _, n := range nodes {
		if n.IsDir {
			if n.TotalSize != nil {
				total += *n.TotalSize
			}
		} else {
			total += n.Size
		}
	}
	return total
}
