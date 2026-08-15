package http

import (
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/skills"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
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

	slugDir := store.SkillSlugDir(filePath)
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

	slugDir := store.SkillSlugDir(filePath)
	if slugDir == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgVersionNotFound)})
		return
	}

	versionDir := filepath.Join(slugDir, strconv.Itoa(version))
	if _, err := os.Stat(versionDir); err != nil {
		if !isSystem || h.bundledDir == "" {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgVersionNotFound)})
			return
		}
		bundledDir := filepath.Join(h.bundledDir, slug)
		if _, err := os.Stat(bundledDir); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgVersionNotFound)})
			return
		}
		files := walkSkillFiles(bundledDir)
		if files == nil {
			files = []fileEntry{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"files": files})
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

	slugDir := store.SkillSlugDir(filePath)
	if slugDir == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgFileNotFound)})
		return
	}

	versionDir := filepath.Join(slugDir, strconv.Itoa(version))
	roots := readableSkillRoots(versionDir, slug, isSystem, h.bundledDir)
	if len(roots) == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgFileNotFound)})
		return
	}

	cleanRelPath := filepath.Clean(relPath)
	var data []byte
	var info os.FileInfo
	var readErr error = os.ErrNotExist
	for _, root := range roots {
		absPath := filepath.Join(root, cleanRelPath)
		if !strings.HasPrefix(absPath, root+string(filepath.Separator)) {
			slog.Warn("security.skill_files_escape", "resolved", absPath, "root", root)
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidPath)})
			return
		}
		data, info, readErr = readSkillFile(absPath)
		if readErr == nil {
			break
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

// handleWriteFile writes a single file's content, creating a new immutable
// version of a managed (non-system) skill — mirroring the skill_manage tool's
// patch action (see internal/tools/skill_manage.go) and the skill evolution
// apply path (applySkillSuggestionPatch in skills_evolution.go): the current
// version directory is copied to a new version directory, the target file is
// updated there, and the skill's DB row is repointed at the new version.
// System/bundled skills are read-only via the API — editing them here would
// silently diverge from the shipped source and is rejected. Historical
// versions remain immutable.
func (h *SkillsHandler) handleWriteFile(w http.ResponseWriter, r *http.Request) {
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

	var body struct {
		Content string `json:"content"`
	}
	if !bindJSON(w, r, locale, &body) {
		return
	}

	_, _, _, isSystem, ok := h.skills.GetSkillFilePath(r.Context(), id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "skill", id.String())})
		return
	}
	if isSystem {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "cannot edit a system skill"})
		return
	}

	// Ownership check (admins bypass) — mirrors handleUpdate/handleDelete.
	auth := resolveAuth(r)
	if !permissions.HasMinRole(auth.Role, permissions.RoleAdmin) {
		userID := store.UserIDFromContext(r.Context())
		if ownerID, found := h.skills.GetSkillOwnerID(r.Context(), id); found && ownerID != userID {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "only the skill owner can perform this action"})
			return
		}
	}

	path, newVersion, err := skills.WriteVersionedFile(r.Context(), h.skills, h.tenantSkillsDir(r), id, relPath, body.Content)
	if err != nil {
		switch {
		case errors.Is(err, skills.ErrSkillFileNotFound):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgFileNotFound)})
		case errors.Is(err, skills.ErrSkillIsSystem):
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "cannot edit a system skill"})
		case errors.Is(err, skills.ErrSkillInvalidPath):
			slog.Warn("security.skill_files_escape", "path", relPath, "skill_id", id.String())
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidPath)})
		default:
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return
	}

	h.emitCacheInvalidate(bus.CacheKindSkills, id.String(), uuid.Nil)
	emitAudit(h.msgBus, r, "skill.file_updated", "skill", id.String())
	writeJSON(w, http.StatusOK, map[string]any{"ok": "true", "path": path, "version": newVersion})
}

func readableSkillRoots(versionDir, slug string, isSystem bool, bundledDir string) []string {
	var roots []string
	if info, err := os.Stat(versionDir); err == nil && info.IsDir() {
		roots = append(roots, versionDir)
	}
	if isSystem && bundledDir != "" {
		bundledRoot := filepath.Join(bundledDir, slug)
		if info, err := os.Stat(bundledRoot); err == nil && info.IsDir() {
			roots = append(roots, bundledRoot)
		}
	}
	return roots
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
