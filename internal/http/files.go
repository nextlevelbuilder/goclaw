package http

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/edition"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// FilesHandler serves files over HTTP with Bearer token auth.
// Accepts absolute paths — the auth token protects against unauthorized access.
// When an exact path is not found, falls back to searching the workspace for
// generated files by basename (media filenames include timestamps and are globally unique).
type FilesHandler struct {
	workspace string // workspace root for fallback file search
	dataDir   string // data directory root for tenant path validation
}

// NewFilesHandler creates a handler that serves files by absolute path.
// workspace is the root directory used for fallback generated file search.
// dataDir is used for tenant path validation (files must be within tenant's dirs).
func NewFilesHandler(workspace, dataDir string) *FilesHandler {
	return &FilesHandler{workspace: workspace, dataDir: dataDir}
}

// RegisterRoutes registers the file serving route.
func (h *FilesHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/files/{path...}", h.auth(h.handleServe))
	mux.HandleFunc("POST /v1/files/sign", h.handleSign)
}

// handleSign accepts a JSON body with a "path" field (absolute file path),
// returns a signed /v1/files/ URL with ?ft= token. Requires Bearer auth.
func (h *FilesHandler) handleSign(w http.ResponseWriter, r *http.Request) {
	provided := extractBearerToken(r)
	authedReq, ok := requireAuthBearer("", provided, w, r)
	if !ok {
		return
	}
	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(authedReq.Body).Decode(&body); err != nil || body.Path == "" {
		http.Error(w, `{"error":"path required"}`, http.StatusBadRequest)
		return
	}

	// Validate path is within workspace or dataDir before signing.
	// Defense-in-depth: prevents signing tokens for arbitrary system files.
	absPath := filepath.Clean(body.Path)
	if !filepath.IsAbs(absPath) {
		// Windows drive letter path (e.g. "C:\...") — keep as-is, consistent with handleServe.
		if len(absPath) >= 2 && absPath[1] == ':' {
			// already absolute on Windows
		} else {
			absPath = filepath.Clean("/" + absPath)
		}
	}
	sep := string(filepath.Separator)
	if (h.workspace == "" || (!strings.HasPrefix(absPath, h.workspace+sep) && absPath != h.workspace)) &&
		(h.dataDir == "" || (!strings.HasPrefix(absPath, h.dataDir+sep) && absPath != h.dataDir)) {
		slog.Warn("security.files_sign_path_denied", "path", absPath, "workspace", h.workspace, "data_dir", h.dataDir)
		http.Error(w, `{"error":"path outside allowed directories"}`, http.StatusForbidden)
		return
	}

	// Multi-tenant (RBAC): additionally restrict to the requesting tenant's dirs.
	// Prevents tenant A from signing a URL for tenant B's files.
	if edition.Current().RBACEnabled {
		tenantData := config.TenantDataDir(h.dataDir, store.TenantIDFromContext(authedReq.Context()), store.TenantSlugFromContext(authedReq.Context()))
		tenantWs := config.TenantWorkspace(h.workspace, store.TenantIDFromContext(authedReq.Context()), store.TenantSlugFromContext(authedReq.Context()))
		if (!strings.HasPrefix(absPath, tenantData+sep) && absPath != tenantData) &&
			(!strings.HasPrefix(absPath, tenantWs+sep) && absPath != tenantWs) {
			slog.Warn("security.files_sign_tenant_denied", "path", absPath, "tenant_data", tenantData, "tenant_ws", tenantWs)
			http.Error(w, `{"error":"path outside allowed directories"}`, http.StatusForbidden)
			return
		}
	}

	urlPath := "/v1/files/" + strings.TrimPrefix(filepath.Clean(body.Path), "/")
	ft := SignFileToken(urlPath, FileSigningKey(), FileTokenTTL)
	writeJSON(w, http.StatusOK, map[string]string{
		"url": urlPath + "?ft=" + ft,
	})
}

func (h *FilesHandler) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Priority 1: short-lived signed file token (?ft=) — decoupled from gateway token.
		if ft := r.URL.Query().Get("ft"); ft != "" {
			path := "/v1/files/" + r.PathValue("path")
			if VerifyFileToken(ft, path, FileSigningKey()) {
				next(w, r)
				return
			}
			http.Error(w, "invalid or expired file token", http.StatusUnauthorized)
			return
		}
		// Priority 2: Bearer header (API clients only).
		provided := extractBearerToken(r)
		authedReq, ok := requireAuthBearer("", provided, w, r)
		if !ok {
			return
		}
		next(w, authedReq)
	}
}

// deniedFilePrefixes blocks access to sensitive system directories.
// Defense-in-depth: the auth token is the primary barrier, but restricting
// known-sensitive paths limits damage if a token leaks.
var deniedFilePrefixes = []string{
	"/etc/", "/proc/", "/sys/", "/dev/",
	"/root/", "/boot/", "/run/",
	"/var/run/", "/var/log/",
}

func (h *FilesHandler) handleServe(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	urlPath := r.PathValue("path")
	if urlPath == "" {
		http.Error(w, i18n.T(locale, i18n.MsgRequired, "path"), http.StatusBadRequest)
		return
	}

	// Prevent path traversal
	if strings.Contains(urlPath, "..") {
		slog.Warn("security.files_traversal", "path", urlPath)
		http.Error(w, i18n.T(locale, i18n.MsgInvalidPath), http.StatusBadRequest)
		return
	}

	// URL path is the absolute path with leading "/" stripped (e.g. "app/.goclaw/workspace/file.png")
	// Windows drive letter: "C:/Users/..." → use directly without prepending "/"
	var absPath string
	if len(urlPath) >= 2 && urlPath[1] == ':' {
		absPath = filepath.Clean(urlPath)
	} else {
		absPath = filepath.Clean("/" + urlPath)
	}

	// Block access to sensitive system directories
	for _, prefix := range deniedFilePrefixes {
		if strings.HasPrefix(absPath, prefix) {
			slog.Warn("security.files_denied_path", "path", absPath)
			http.Error(w, i18n.T(locale, i18n.MsgInvalidPath), http.StatusForbidden)
			return
		}
	}

	// Defense-in-depth: validate workspace/dataDir boundary even for signed file tokens.
	// The token cryptographically binds the URL path, but we also verify the resolved
	// absolute path stays within allowed directories to limit blast radius of any
	// bug in the signing flow.
	if r.URL.Query().Get("ft") != "" {
		sep := string(filepath.Separator)
		inWorkspace := h.workspace != "" && (strings.HasPrefix(absPath, h.workspace+sep) || absPath == h.workspace)
		inDataDir := h.dataDir != "" && (strings.HasPrefix(absPath, h.dataDir+sep) || absPath == h.dataDir)
		if !inWorkspace && !inDataDir {
			slog.Warn("security.files_ft_path_denied", "path", absPath, "workspace", h.workspace, "data_dir", h.dataDir)
			http.NotFound(w, r)
			return
		}
	}

	// Path isolation: validate file path is within allowed directories.
	if r.URL.Query().Get("ft") == "" {
		allowed := false

		// Always allow files within workspace root and data dir root.
		// These are the two top-level directories that contain all user files.
		sep := string(filepath.Separator)
		if h.workspace != "" && (strings.HasPrefix(absPath, h.workspace+sep) || absPath == h.workspace) {
			allowed = true
		}
		if !allowed && h.dataDir != "" && (strings.HasPrefix(absPath, h.dataDir+sep) || absPath == h.dataDir) {
			allowed = true
		}

		// Multi-tenant (standard edition): additionally restrict to tenant-scoped subdirectories.
		if allowed && edition.Current().RBACEnabled {
			tenantData := config.TenantDataDir(h.dataDir, store.TenantIDFromContext(r.Context()), store.TenantSlugFromContext(r.Context()))
			tenantWs := h.tenantWorkspace(r)
			if !strings.HasPrefix(absPath, tenantData+sep) &&
				!strings.HasPrefix(absPath, tenantWs+sep) &&
				absPath != tenantData && absPath != tenantWs {
				allowed = false
			}
		}

		if !allowed {
			slog.Warn("security.files_path_denied", "path", absPath, "workspace", h.workspace, "data_dir", h.dataDir)
			http.NotFound(w, r)
			return
		}
	}

	info, err := os.Stat(absPath)
	if err != nil && !os.IsNotExist(err) {
		http.NotFound(w, r)
		return
	}
	// Fuzzy match: generated files have timestamp suffixes (e.g. "file_20260326-232559_269000.png")
	// but LLM may reference them without timestamp (e.g. "file.png"). Try prefix match in same dir.
	if err != nil {
		if resolved := fuzzyMatchInDir(absPath); resolved != "" {
			absPath = resolved
			info, err = os.Stat(absPath)
		}
	}
	if err != nil || info.IsDir() {
		// For ft= signed requests, the path is cryptographically bound — no fallback search.
		// Searching the global workspace could cross tenant boundaries if a same-basename
		// file exists in another tenant's directory.
		if r.URL.Query().Get("ft") != "" {
			http.NotFound(w, r)
			return
		}
		// Fallback: search workspace for file by basename (handles LLM-hallucinated paths).
		// Generated media filenames include timestamps and are globally unique.
		// Scoped to tenant workspace (bearer auth always has tenant context).
		ws := h.tenantWorkspace(r)
		if resolved := h.findInWorkspace(ws, filepath.Base(absPath)); resolved != "" {
			absPath = resolved
			info, _ = os.Stat(absPath)
		} else {
			http.NotFound(w, r)
			return
		}
	}

	// Set Content-Type from extension
	ext := filepath.Ext(absPath)
	ct := mime.TypeByExtension(ext)
	if ct != "" {
		w.Header().Set("Content-Type", ct)
	}

	// Trigger browser download with original filename when ?download=true
	if r.URL.Query().Get("download") == "true" {
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filepath.Base(absPath)))
	}

	http.ServeFile(w, r, absPath)
}

// tenantWorkspace resolves the workspace scoped to the requesting tenant.
func (h *FilesHandler) tenantWorkspace(r *http.Request) string {
	tid := store.TenantIDFromContext(r.Context())
	slug := store.TenantSlugFromContext(r.Context())
	return config.TenantWorkspace(h.workspace, tid, slug)
}

// findInWorkspace searches the workspace directory tree for a file by basename.
// Returns the absolute path if found, empty string otherwise.
// Searches team directories including generated/ and system/ subdirs.
func (h *FilesHandler) findInWorkspace(workspace, basename string) string {
	if workspace == "" || basename == "" {
		return ""
	}
	var found string
	_ = filepath.WalkDir(workspace, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return filepath.SkipDir
		}
		if d.IsDir() {
			name := d.Name()
			// Allow workspace root
			if path == workspace {
				return nil
			}
			// Allow direct children of workspace root (agent workspace dirs like "quill", "goclaw")
			if filepath.Dir(path) == workspace {
				return nil
			}
			// Allow known directory structures
			if name == "teams" || name == "generated" || name == "system" || name == "ws" || name == ".uploads" || name == "tenants" {
				return nil
			}
			// Allow date directories (e.g. 2026-03-20)
			if len(name) == 10 && name[4] == '-' {
				return nil
			}
			// Allow team/user ID directories (UUIDs, numeric IDs)
			if strings.Contains(name, "-") || isNumeric(name) {
				return nil
			}
			return filepath.SkipDir
		}
		if d.Name() == basename {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

// fuzzyMatchInDir handles LLM-hallucinated filenames missing timestamp suffixes.
// E.g. requested "file.png" matches "file_20260326-232559_269000.png" in the same directory.
func fuzzyMatchInDir(absPath string) string {
	dir := filepath.Dir(absPath)
	base := filepath.Base(absPath)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext) // "smart-home-cover"

	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		// Match: starts with stem, has same extension, has timestamp between
		// e.g. "smart-home-cover_20260326-232444_269000.png"
		if strings.HasPrefix(name, stem) && strings.HasSuffix(name, ext) && name != base {
			return filepath.Join(dir, name)
		}
	}
	return ""
}

func isNumeric(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(s) > 0
}

