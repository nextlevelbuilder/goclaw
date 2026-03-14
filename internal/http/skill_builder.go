package http

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

const maxSkillDirSize = 20 << 20 // 20 MB

// SkillBuilderHandler handles Skill Builder project management HTTP endpoints.
type SkillBuilderHandler struct {
	token     string
	skillsDir string          // skills-store/ directory
	skills    *pg.PGSkillStore
	loader    *skills.Loader
}

// NewSkillBuilderHandler creates a handler for Skill Builder endpoints.
func NewSkillBuilderHandler(token, skillsDir string, skillStore *pg.PGSkillStore, loader *skills.Loader) *SkillBuilderHandler {
	return &SkillBuilderHandler{
		token:     token,
		skillsDir: skillsDir,
		skills:    skillStore,
		loader:    loader,
	}
}

// RegisterRoutes registers all Skill Builder routes on the given mux.
func (h *SkillBuilderHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/skill/builder/projects", h.auth(h.handleCreateProject))
	mux.HandleFunc("GET /v1/skill/builder/projects/{id}/files", h.auth(h.handleListFiles))
	mux.HandleFunc("GET /v1/skill/builder/projects/{id}/file", h.auth(h.handleGetFile))
	mux.HandleFunc("POST /v1/skill/builder/projects/{id}/publish", h.auth(h.handlePublishProject))
}

func (h *SkillBuilderHandler) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.token != "" {
			if !tokenMatch(extractBearerToken(r), h.token) {
				locale := extractLocale(r)
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": i18n.T(locale, i18n.MsgUnauthorized)})
				return
			}
		}
		userID := extractUserID(r)
		role := resolveHTTPRole(r, h.token)
		ctx := store.WithLocale(r.Context(), extractLocale(r))
		ctx = store.WithRole(ctx, role)
		if userID != "" {
			ctx = store.WithUserID(ctx, userID)
		}
		r = r.WithContext(ctx)
		next(w, r)
	}
}

// resolveSkillProjectDir validates projectID, resolves the directory, and performs a path traversal check.
// Returns (cleanDir, projectID, error-written). If error-written is true, the response has already been sent.
func (h *SkillBuilderHandler) resolveSkillProjectDir(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	locale := store.LocaleFromContext(r.Context())
	userID := store.UserIDFromContext(r.Context())
	if userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgUserIDRequired)})
		return "", "", true
	}

	projectID := r.PathValue("id")
	if projectID == "" || !isValidSlug(projectID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidSlug, "project id")})
		return "", "", true
	}

	cleanDir := filepath.Clean(filepath.Join(h.skillsDir, projectID))
	if !strings.HasPrefix(cleanDir, filepath.Clean(h.skillsDir)+string(filepath.Separator)) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidPath)})
		return "", "", true
	}

	if _, err := os.Stat(cleanDir); os.IsNotExist(err) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "project", projectID)})
		return "", "", true
	}

	return cleanDir, projectID, false
}

// --- Create Project ---

func (h *SkillBuilderHandler) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	userID := store.UserIDFromContext(r.Context())
	if userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgUserIDRequired)})
		return
	}

	var req createProjectRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return
	}

	if req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "name")})
		return
	}
	if !isValidSlug(req.Name) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidSlug, "name")})
		return
	}
	if len(req.Name) > 64 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidSlug, "name")})
		return
	}

	// Skills are stored directly in skills-store/<name>/ (not per-user)
	projectDir := filepath.Clean(filepath.Join(h.skillsDir, req.Name))

	// Path traversal check
	if !strings.HasPrefix(projectDir, filepath.Clean(h.skillsDir)+string(filepath.Separator)) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidPath)})
		return
	}

	// Check if project already exists
	if _, err := os.Stat(projectDir); err == nil {
		writeJSON(w, http.StatusConflict, map[string]any{"code": "ALREADY_EXISTS", "error": i18n.T(locale, i18n.MsgAlreadyExists, "project", req.Name)})
		return
	}

	// Create empty project directory (no template copy)
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		slog.Error("skill_builder.create_project", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToCreate, "project", err.Error())})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":   req.Name,
		"name": req.Name,
	})
}

// --- List Files ---

func (h *SkillBuilderHandler) handleListFiles(w http.ResponseWriter, r *http.Request) {
	cleanDir, _, done := h.resolveSkillProjectDir(w, r)
	if done {
		return
	}

	tree := buildTreeNode(cleanDir, "", 0)
	writeJSON(w, http.StatusOK, map[string]any{"tree": tree})
}

// --- Get File ---

func (h *SkillBuilderHandler) handleGetFile(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	cleanDir, _, done := h.resolveSkillProjectDir(w, r)
	if done {
		return
	}

	filePath := r.URL.Query().Get("path")
	if filePath == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "path")})
		return
	}

	fullPath := filepath.Clean(filepath.Join(cleanDir, filePath))

	// Path traversal check: resolved path must be under the project directory
	if !strings.HasPrefix(fullPath, cleanDir+string(filepath.Separator)) && fullPath != cleanDir {
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

// --- Publish ---

func (h *SkillBuilderHandler) handlePublishProject(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	userID := store.UserIDFromContext(r.Context())

	cleanDir, _, done := h.resolveSkillProjectDir(w, r)
	if done {
		return
	}

	// Read + validate SKILL.md
	skillPath := filepath.Join(cleanDir, "SKILL.md")
	content, err := os.ReadFile(skillPath)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgFileNotFound)})
		return
	}
	if len(strings.TrimSpace(string(content))) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "SKILL.md content")})
		return
	}

	// Parse frontmatter
	name, description, slug, frontmatter := skills.ParseSkillFrontmatter(string(content))
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "SKILL.md frontmatter name")})
		return
	}
	if slug == "" {
		slug = skills.Slugify(name)
	}
	if !skills.SlugRegexp.MatchString(slug) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidSlug, "slug")})
		return
	}

	// Check system skill conflict
	if h.skills.IsSystemSkill(slug) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgAlreadyExists, "system skill", slug)})
		return
	}

	// Compute hash + size
	hasher := sha256.New()
	hasher.Write(content)
	fileHash := fmt.Sprintf("%x", hasher.Sum(nil))

	fileSize, err := skillDirSize(cleanDir)
	if err != nil {
		slog.Error("skill_builder.publish_dir_size", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, err.Error())})
		return
	}
	if fileSize > maxSkillDirSize {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("skill directory exceeds size limit (%d MB)", maxSkillDirSize>>20)})
		return
	}

	// Version + destination
	version := h.skills.GetNextVersion(slug)
	destDir := filepath.Join(h.skillsDir, slug, fmt.Sprintf("%d", version))
	if err := os.MkdirAll(destDir, 0755); err != nil {
		slog.Error("skill_builder.publish_mkdir", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToCreate, "version directory", err.Error())})
		return
	}

	// Copy skill directory to versioned destination
	if err := copySkillDirForPublish(cleanDir, destDir); err != nil {
		slog.Error("skill_builder.publish_copy", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, err.Error())})
		return
	}

	// Insert into DB
	if userID == "" {
		userID = "system"
	}
	desc := description
	params := pg.SkillCreateParams{
		Name:        name,
		Slug:        slug,
		Description: &desc,
		OwnerID:     userID,
		Visibility:  "private",
		Version:     version,
		FilePath:    destDir,
		FileSize:    fileSize,
		FileHash:    &fileHash,
		Frontmatter: frontmatter,
	}

	id, err := h.skills.CreateSkillManaged(r.Context(), params)
	if err != nil {
		slog.Error("skill_builder.publish_create", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, err.Error())})
		return
	}

	slog.Info("skill_builder: skill published", "id", id, "slug", slug, "version", version, "owner", userID)

	// Bump loader cache
	if h.loader != nil {
		h.loader.BumpVersion()
	}

	// Scan dependencies
	var depsWarning string
	manifest := skills.ScanSkillDeps(destDir)
	if manifest != nil && !manifest.IsEmpty() {
		ok, missing := skills.CheckSkillDeps(manifest)
		if !ok {
			_ = h.skills.StoreMissingDeps(id, missing)
			depsWarning = skills.FormatMissing(missing)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"skill_id":     id.String(),
		"name":         name,
		"slug":         slug,
		"version":      version,
		"status":       "published",
		"deps_warning": depsWarning,
	})
}

// --- Helpers ---

// skillDirSize returns total size of all files in a directory.
func skillDirSize(path string) (int64, error) {
	var total int64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total, err
}

// copySkillDirForPublish recursively copies src to dst, skipping symlinks and system artifacts.
func copySkillDirForPublish(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		// Security: skip path traversal
		if strings.Contains(rel, "..") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip symlinks
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}

		// Skip system artifacts
		if skills.IsSystemArtifact(rel) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		destPath := filepath.Join(dst, rel)

		if info.IsDir() {
			return os.MkdirAll(destPath, 0755)
		}

		// Copy file
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(destPath, data, 0644)
	})
}
