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

var filesAfterOpenHookForTest func(string)

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

	absPath := absoluteFilePath(body.Path)
	file, _, _, ok := h.openValidatedFile(authedReq, absPath, false)
	if !ok {
		http.Error(w, `{"error":"path outside allowed directories"}`, http.StatusForbidden)
		return
	}
	_ = file.Close()

	urlPath := fileURLPath(absPath)
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

	absPath := absoluteFilePath(urlPath)

	// Block access to sensitive system directories
	if hasDeniedFilePrefix(absPath) {
		slog.Warn("security.files_denied_path", "path", absPath)
		http.Error(w, i18n.T(locale, i18n.MsgInvalidPath), http.StatusForbidden)
		return
	}

	signed := r.URL.Query().Get("ft") != ""
	if !h.lexicallyAllowsFilePath(r, absPath, signed) {
		slog.Warn("security.files_path_denied", "path", absPath, "workspace", h.workspace, "data_dir", h.dataDir)
		http.NotFound(w, r)
		return
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
		if signed {
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
	file, realPath, fileInfo, ok := h.openValidatedFile(r, absPath, signed)
	if !ok {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	absPath = realPath

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

	http.ServeContent(w, r, filepath.Base(absPath), fileInfo.ModTime(), file)
}

func absoluteFilePath(path string) string {
	absPath := filepath.Clean(path)
	if filepath.IsAbs(absPath) {
		return absPath
	}
	// Windows drive letter path (e.g. "C:\...") is absolute to this handler.
	if len(absPath) >= 2 && absPath[1] == ':' {
		return absPath
	}
	return filepath.Clean(string(filepath.Separator) + absPath)
}

func fileURLPath(absPath string) string {
	return "/v1/files/" + strings.TrimPrefix(filepath.Clean(absPath), string(filepath.Separator))
}

func hasDeniedFilePrefix(path string) bool {
	cleaned := filepath.Clean(path)
	for _, prefix := range deniedFilePrefixes {
		root := filepath.Clean(prefix)
		if pathWithinDir(cleaned, root) {
			return true
		}
	}
	return false
}

func configuredFileRoot(root string) string {
	if root == "" {
		return ""
	}
	return filepath.Clean(root)
}

func canonicalFileRoots(roots []string) []string {
	out := make([]string, 0, len(roots))
	for _, root := range roots {
		if root = configuredFileRoot(root); root != "" {
			out = append(out, evalSymlinkOrClean(root))
		}
	}
	return out
}

func (h *FilesHandler) requestFileRoots(r *http.Request, signed bool, absPath string) []string {
	if signed {
		return []string{
			inferredScopedFileRoot(h.workspace, absPath),
			inferredScopedFileRoot(h.dataDir, absPath),
		}
	}
	if edition.Current().RBACEnabled {
		return []string{
			config.TenantWorkspace(h.workspace, store.TenantIDFromContext(r.Context()), store.TenantSlugFromContext(r.Context())),
			config.TenantDataDir(h.dataDir, store.TenantIDFromContext(r.Context()), store.TenantSlugFromContext(r.Context())),
		}
	}
	return []string{h.workspace, h.dataDir}
}

func inferredScopedFileRoot(base, absPath string) string {
	base = configuredFileRoot(base)
	if base == "" || !pathWithinDir(filepath.Clean(absPath), base) {
		return ""
	}
	tenantsRoot := filepath.Join(base, "tenants")
	if !pathWithinDir(filepath.Clean(absPath), tenantsRoot) || filepath.Clean(absPath) == tenantsRoot {
		return base
	}
	rel, err := filepath.Rel(tenantsRoot, filepath.Clean(absPath))
	if err != nil {
		return ""
	}
	first, _, _ := strings.Cut(rel, string(filepath.Separator))
	if first == "" || first == "." || first == ".." {
		return ""
	}
	return filepath.Join(tenantsRoot, first)
}

func filePathWithinAnyRoot(path string, roots []string) bool {
	for _, root := range roots {
		if root != "" && pathWithinDir(filepath.Clean(path), filepath.Clean(root)) {
			return true
		}
	}
	return false
}

func (h *FilesHandler) lexicallyAllowsFilePath(r *http.Request, absPath string, signed bool) bool {
	return filePathWithinAnyRoot(absPath, h.requestFileRoots(r, signed, absPath))
}

func (h *FilesHandler) openValidatedFile(r *http.Request, absPath string, signed bool) (*os.File, string, os.FileInfo, bool) {
	file, err := os.Open(absPath)
	if err != nil {
		return nil, "", nil, false
	}
	if filesAfterOpenHookForTest != nil {
		filesAfterOpenHookForTest(absPath)
	}

	realPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		slog.Warn("security.files_path_unresolved", "path", absPath, "error", err)
		_ = file.Close()
		return nil, "", nil, false
	}
	realPath = filepath.Clean(realPath)
	if hasDeniedFilePrefix(realPath) {
		slog.Warn("security.files_realpath_denied", "path", absPath, "resolved", realPath)
		_ = file.Close()
		return nil, "", nil, false
	}
	roots := canonicalFileRoots(h.requestFileRoots(r, signed, absPath))
	if !filePathWithinAnyRoot(realPath, roots) {
		slog.Warn("security.files_realpath_escape", "path", absPath, "resolved", realPath, "roots", roots)
		_ = file.Close()
		return nil, "", nil, false
	}
	realInfo, err := os.Stat(realPath)
	if err != nil {
		_ = file.Close()
		return nil, "", nil, false
	}
	fileInfo, err := file.Stat()
	if err != nil {
		_ = file.Close()
		slog.Warn("security.files_open_race", "path", realPath, "error", err)
		return nil, "", nil, false
	}
	if fileInfo.IsDir() || realInfo.IsDir() || !os.SameFile(realInfo, fileInfo) {
		_ = file.Close()
		slog.Warn("security.files_open_race", "path", realPath)
		return nil, "", nil, false
	}
	return file, realPath, fileInfo, true
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
