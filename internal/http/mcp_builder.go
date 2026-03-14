package http

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// MCPBuilderHandler handles MCP builder project management HTTP endpoints.
type MCPBuilderHandler struct {
	token        string
	projectsRoot string // {workspace}/mcp-projects/
	templateDir  string // path to templates/mcp-server/
	mcpStore     store.MCPServerStore
}

// NewMCPBuilderHandler creates a handler for MCP builder endpoints.
func NewMCPBuilderHandler(token, projectsRoot, templateDir string) *MCPBuilderHandler {
	return &MCPBuilderHandler{
		token:        token,
		projectsRoot: projectsRoot,
		templateDir:  templateDir,
	}
}

// SetMCPStore sets the MCP server store for server registration.
func (h *MCPBuilderHandler) SetMCPStore(s store.MCPServerStore) {
	h.mcpStore = s
}

// RegisterRoutes registers all MCP builder routes on the given mux.
func (h *MCPBuilderHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/mcp/builder/projects", h.auth(h.handleListProjects))
	mux.HandleFunc("POST /v1/mcp/builder/projects", h.auth(h.handleCreateProject))
	mux.HandleFunc("GET /v1/mcp/builder/projects/{id}/files", h.auth(h.handleListFiles))
	mux.HandleFunc("GET /v1/mcp/builder/projects/{id}/file", h.auth(h.handleGetFile))
	mux.HandleFunc("POST /v1/mcp/builder/projects/{id}/build", h.auth(h.handleBuildProject))
	mux.HandleFunc("POST /v1/mcp/builder/projects/{id}/register", h.auth(h.handleRegisterProject))
}

func (h *MCPBuilderHandler) auth(next http.HandlerFunc) http.HandlerFunc {
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

// --- Project CRUD ---

type createProjectRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type projectInfo struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

func (h *MCPBuilderHandler) handleCreateProject(w http.ResponseWriter, r *http.Request) {
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

	projectDir := filepath.Clean(filepath.Join(h.projectsRoot, userID, req.Name))

	// Path traversal check
	if !strings.HasPrefix(projectDir, filepath.Clean(h.projectsRoot)+string(filepath.Separator)) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidPath)})
		return
	}

	// Check if project already exists
	if _, err := os.Stat(projectDir); err == nil {
		writeJSON(w, http.StatusConflict, map[string]any{"code": "ALREADY_EXISTS", "error": i18n.T(locale, i18n.MsgAlreadyExists, "project", req.Name)})
		return
	}

	// Create project directory
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		slog.Error("mcp_builder.create_project", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToCreate, "project", err.Error())})
		return
	}

	// Copy template files (excluding node_modules)
	if err := copyTemplate(h.templateDir, projectDir); err != nil {
		// Clean up on failure
		os.RemoveAll(projectDir)
		slog.Error("mcp_builder.copy_template", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToCreate, "project", err.Error())})
		return
	}

	// Update package.json name field
	if err := updatePackageName(projectDir, req.Name); err != nil {
		slog.Warn("mcp_builder.update_package_name", "error", err)
		// Non-fatal: project is still usable
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":   req.Name,
		"name": req.Name,
	})
}

func (h *MCPBuilderHandler) handleListProjects(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	userID := store.UserIDFromContext(r.Context())
	if userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgUserIDRequired)})
		return
	}

	userDir := filepath.Clean(filepath.Join(h.projectsRoot, userID))
	if !strings.HasPrefix(userDir, filepath.Clean(h.projectsRoot)+string(filepath.Separator)) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidPath)})
		return
	}
	entries, err := os.ReadDir(userDir)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusOK, map[string]any{"projects": []projectInfo{}})
			return
		}
		slog.Error("mcp_builder.list_projects", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToList, "projects")})
		return
	}

	projects := make([]projectInfo, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		projects = append(projects, projectInfo{
			ID:        entry.Name(),
			Name:      entry.Name(),
			CreatedAt: info.ModTime(),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"projects": projects})
}

// --- Docker Build ---

type buildResult struct {
	Image  string `json:"image,omitempty"`
	Status string `json:"status"`
	Log    string `json:"log"`
}

// dockerBuild runs `docker build -t imageTag projectDir` with a 5-minute timeout.
// Returns the combined output log and any error.
func dockerBuild(ctx context.Context, projectDir, imageTag string) (string, error) {
	buildCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(buildCtx, "docker", "build", "-t", imageTag, projectDir)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

// resolveProjectDir validates projectID, resolves the directory, and performs a path traversal check.
// Returns (cleanDir, projectID, error-written). If error-written is true, the response has already been sent.
func (h *MCPBuilderHandler) resolveProjectDir(w http.ResponseWriter, r *http.Request) (string, string, bool) {
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

	cleanDir := filepath.Clean(filepath.Join(h.projectsRoot, userID, projectID))
	if !strings.HasPrefix(cleanDir, filepath.Clean(h.projectsRoot)+string(filepath.Separator)) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidPath)})
		return "", "", true
	}

	if _, err := os.Stat(cleanDir); os.IsNotExist(err) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "project", projectID)})
		return "", "", true
	}

	return cleanDir, projectID, false
}

func (h *MCPBuilderHandler) handleBuildProject(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	cleanDir, projectID, done := h.resolveProjectDir(w, r)
	if done {
		return
	}

	// Verify Dockerfile exists
	if _, err := os.Stat(filepath.Join(cleanDir, "Dockerfile")); os.IsNotExist(err) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgFileNotFound)})
		return
	}

	imageTag := fmt.Sprintf("mcp-%s:latest", projectID)
	output, err := dockerBuild(r.Context(), cleanDir, imageTag)

	if err != nil {
		status := http.StatusUnprocessableEntity
		if r.Context().Err() == context.DeadlineExceeded {
			status = http.StatusGatewayTimeout
		}
		slog.Warn("mcp_builder.build_project", "project", projectID, "error", err)
		writeJSON(w, status, buildResult{Status: "failed", Log: output})
		return
	}

	writeJSON(w, http.StatusOK, buildResult{Image: imageTag, Status: "success", Log: output})
}

// --- Register (Docker build or Bun native + MCP server entry) ---

// isDockerAvailable checks if the Docker CLI is present and the daemon is reachable.
func isDockerAvailable() bool {
	cmd := exec.Command("docker", "info")
	return cmd.Run() == nil
}

func (h *MCPBuilderHandler) handleRegisterProject(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	userID := store.UserIDFromContext(r.Context())

	if h.mcpStore == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgTemplateDirMissing)})
		return
	}

	cleanDir, projectID, done := h.resolveProjectDir(w, r)
	if done {
		return
	}

	// Read package.json for name and description
	pkgData, err := os.ReadFile(filepath.Join(cleanDir, "package.json"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgFileNotFound)})
		return
	}

	var pkg struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(pkgData, &pkg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return
	}
	if pkg.Name == "" {
		pkg.Name = projectID
	}

	var command string
	var argsJSON []byte
	var imageTag string

	if isDockerAvailable() {
		// Docker mode: build image and register with docker run
		imageTag = fmt.Sprintf("mcp-%s:latest", projectID)
		output, err := dockerBuild(r.Context(), cleanDir, imageTag)
		if err != nil {
			status := http.StatusUnprocessableEntity
			if r.Context().Err() == context.DeadlineExceeded {
				status = http.StatusGatewayTimeout
			}
			slog.Warn("mcp_builder.register_build", "project", projectID, "error", err)
			writeJSON(w, status, map[string]any{
				"status": "build_failed",
				"log":    output,
			})
			return
		}
		command = "docker"
		argsJSON, _ = json.Marshal([]string{"run", "--rm", "-i", imageTag})
	} else {
		// Bun native mode: install deps and register with bun run directly
		slog.Info("mcp_builder.register: docker not available, using bun native mode", "project", projectID)

		// Install dependencies if node_modules doesn't exist
		nodeModules := filepath.Join(cleanDir, "node_modules")
		if _, err := os.Stat(nodeModules); os.IsNotExist(err) {
			installCtx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
			defer cancel()
			cmd := exec.CommandContext(installCtx, "bun", "install")
			cmd.Dir = cleanDir
			if out, err := cmd.CombinedOutput(); err != nil {
				slog.Warn("mcp_builder.register_bun_install", "project", projectID, "error", err)
				writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
					"status": "install_failed",
					"log":    string(out),
				})
				return
			}
		}

		entryPoint := filepath.Join(cleanDir, "src", "index.ts")
		command = "bun"
		argsJSON, _ = json.Marshal([]string{"run", entryPoint})
	}

	srv := &store.MCPServerData{
		Name:        pkg.Name,
		DisplayName: pkg.Description,
		Transport:   "stdio",
		Command:     command,
		Args:        argsJSON,
		Enabled:     false,
		CreatedBy:   userID,
	}

	// Check if server with same name exists → update, otherwise create
	existing, err := h.mcpStore.GetServerByName(r.Context(), pkg.Name)
	if err == nil && existing != nil {
		updates := map[string]any{
			"display_name": pkg.Description,
			"transport":    "stdio",
			"command":      command,
			"args":         json.RawMessage(argsJSON),
			"enabled":      false,
		}
		if err := h.mcpStore.UpdateServer(r.Context(), existing.ID, updates); err != nil {
			slog.Error("mcp_builder.register_update", "project", projectID, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError)})
			return
		}
		resp := map[string]any{
			"server_id": existing.ID,
			"name":      pkg.Name,
			"status":    "registered",
			"mode":      command,
		}
		if imageTag != "" {
			resp["image"] = imageTag
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	// Create new server
	if err := h.mcpStore.CreateServer(r.Context(), srv); err != nil {
		slog.Error("mcp_builder.register_create", "project", projectID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError)})
		return
	}

	resp := map[string]any{
		"server_id": srv.ID,
		"name":      pkg.Name,
		"status":    "registered",
		"mode":      command,
	}
	if imageTag != "" {
		resp["image"] = imageTag
	}
	writeJSON(w, http.StatusOK, resp)
}

// --- Helpers ---

// copyTemplate copies the template directory to the destination, excluding node_modules.
func copyTemplate(srcDir, dstDir string) error {
	return filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}

		// Skip excluded directories
		if d.IsDir() {
			switch d.Name() {
			case "node_modules", ".git":
				return filepath.SkipDir
			}
		}

		// Skip lock files
		switch d.Name() {
		case "bun.lockb", "bun.lock":
			return nil
		}

		dstPath := filepath.Join(dstDir, relPath)

		if d.IsDir() {
			return os.MkdirAll(dstPath, 0755)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		return os.WriteFile(dstPath, data, 0644)
	})
}

// updatePackageName updates the "name" field in package.json.
func updatePackageName(projectDir, name string) error {
	pkgPath := filepath.Join(projectDir, "package.json")
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return err
	}

	var pkg map[string]any
	if err := json.Unmarshal(data, &pkg); err != nil {
		return err
	}

	pkg["name"] = name

	out, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(pkgPath, append(out, '\n'), 0644)
}
