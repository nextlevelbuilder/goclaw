package http

import (
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// FilesHandler serves files over HTTP with Bearer token auth.
// Accepts absolute paths — the auth token protects against unauthorized access.
// When an exact path is not found, falls back to searching the workspace for
// generated files by basename (goclaw_gen_* filenames are globally unique).
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
	absPath := filepath.Clean("/" + urlPath)

	// Block access to sensitive system directories
	for _, prefix := range deniedFilePrefixes {
		if strings.HasPrefix(absPath, prefix) {
			slog.Warn("security.files_denied_path", "path", absPath)
			http.Error(w, i18n.T(locale, i18n.MsgInvalidPath), http.StatusForbidden)
			return
		}
	}

	// Tenant isolation: validate absolute path is within tenant's allowed directories.
	// Skip for HMAC file tokens — the path is cryptographically bound in the signature,
	// so it cannot be tampered with. File tokens are generated server-side with the correct path.
	if r.URL.Query().Get("ft") == "" {
		tenantData := config.TenantDataDir(h.dataDir, store.TenantIDFromContext(r.Context()), store.TenantSlugFromContext(r.Context()))
		tenantWs := h.tenantWorkspace(r)
		if !strings.HasPrefix(absPath, tenantData+string(filepath.Separator)) &&
			!strings.HasPrefix(absPath, tenantWs+string(filepath.Separator)) &&
			absPath != tenantData && absPath != tenantWs {
			http.NotFound(w, r)
			return
		}
	}

	info, err := os.Stat(absPath)
	if err != nil || info.IsDir() {
		// Fallback: search workspace for file by basename (handles LLM-hallucinated paths).
		// Generated filenames (goclaw_gen_*) include nanosecond timestamps and are globally unique.
		// Workspace scoped to tenant to prevent cross-tenant file discovery.
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
			// Allow workspace root + known directory structures
			if name == "teams" || name == "generated" || name == "system" || name == ".uploads" || path == workspace {
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

func isNumeric(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(s) > 0
}
