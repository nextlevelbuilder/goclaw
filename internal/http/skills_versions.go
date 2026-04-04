package http

import (
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/skills"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// fileEntry represents a file or directory in a skill version directory.
type fileEntry struct {
	Path  string `json:"path"`
	Name  string `json:"name"`
	IsDir bool   `json:"isDir"`
	Size  int64  `json:"size"`
}

// walkSkillFiles returns all files/dirs under root, skipping system artifacts and symlinks.
func walkSkillFiles(root string) []fileEntry {
	var files []fileEntry
	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if rel == "." {
			return nil
		}
		if skills.IsSystemArtifact(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
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
	return files
}

// skillSlugDir derives the slug parent directory from a DB file_path.
// file_path has the form .../slug/version — returns .../slug.
// Returns empty string if filePath is empty or malformed.
func skillSlugDir(filePath string) string {
	if filePath == "" {
		return ""
	}
	return filepath.Dir(filePath)
}

// handleListVersions returns all available version numbers for a skill.
func (h *SkillsHandler) handleListVersions(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "skill")})
		return
	}

	filePath, _, currentVersion, _, ok := h.skills.GetSkillFilePath(r.Context(), id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "skill", id.String())})
		return
	}

	slugDir := skillSlugDir(filePath)
	if slugDir == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"versions": []int{currentVersion},
			"current":  currentVersion,
		})
		return
	}

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
	sort.Ints(versions)
	if len(versions) == 0 {
		versions = []int{currentVersion}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"versions": versions,
		"current":  currentVersion,
	})
}

// handleListFiles returns all files in a skill version directory.
func (h *SkillsHandler) handleListFiles(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "skill")})
		return
	}

	filePath, slug, currentVersion, isSystem, ok := h.skills.GetSkillFilePath(r.Context(), id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "skill", id.String())})
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

	slugDir := skillSlugDir(filePath)
	if slugDir == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgVersionNotFound)})
		return
	}

	versionDir := filepath.Join(slugDir, strconv.Itoa(version))
	if _, err := os.Stat(versionDir); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgVersionNotFound)})
		return
	}

	files := walkSkillFiles(versionDir)

	// Fallback: if managed dir has no files (seeder CopyDir may have failed),
	// try the bundled skills dir — only for system skills to prevent slug collision attacks.
	if len(files) == 0 && isSystem && h.bundledDir != "" {
		bundledDir := filepath.Join(h.bundledDir, slug)
		if _, err := os.Stat(bundledDir); err == nil {
			files = walkSkillFiles(bundledDir)
		}
	}

	if files == nil {
		files = []fileEntry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": files})
}

// handleReadFile reads a single file from a skill version directory.
func (h *SkillsHandler) handleReadFile(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "skill")})
		return
	}

	relPath := r.PathValue("path")
	if relPath == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "path")})
		return
	}
	if strings.Contains(relPath, "..") {
		slog.Warn("security.skill_files_traversal", "path", relPath)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidPath)})
		return
	}

	filePath, slug, currentVersion, isSystem, ok := h.skills.GetSkillFilePath(r.Context(), id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "skill", id.String())})
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

	slugDir := skillSlugDir(filePath)
	if slugDir == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgFileNotFound)})
		return
	}

	versionDir := filepath.Join(slugDir, strconv.Itoa(version))
	absPath := filepath.Join(versionDir, filepath.Clean(relPath))

	// Verify resolved path is within the version directory
	if !strings.HasPrefix(absPath, versionDir+string(filepath.Separator)) {
		slog.Warn("security.skill_files_escape", "resolved", absPath, "root", versionDir)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidPath)})
		return
	}

	// Try reading from managed dir; fall back to bundled dir for system skills.
	data, info, readErr := readSkillFile(absPath)
	if readErr != nil && isSystem && h.bundledDir != "" {
		bundledPath := filepath.Join(h.bundledDir, slug, filepath.Clean(relPath))
		bundledRoot := filepath.Join(h.bundledDir, slug)
		if strings.HasPrefix(bundledPath, bundledRoot+string(filepath.Separator)) {
			data, info, readErr = readSkillFile(bundledPath)
		}
	}
	if readErr != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgFileNotFound)})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"content": string(data),
		"path":    relPath,
		"size":    info.Size(),
	})
}

// readSkillFile reads a file with security checks (symlink rejection, artifact filtering).
// Returns file data, file info, or error.
func readSkillFile(absPath string) ([]byte, os.FileInfo, error) {
	info, err := os.Lstat(absPath)
	if err != nil || info.IsDir() {
		return nil, nil, os.ErrNotExist
	}
	if info.Mode()&os.ModeSymlink != 0 {
		slog.Warn("security.skill_files_symlink", "path", absPath)
		return nil, nil, os.ErrPermission
	}
	rel := filepath.Base(absPath)
	if skills.IsSystemArtifact(rel) {
		return nil, nil, os.ErrNotExist
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, nil, err
	}
	return data, info, nil
}
