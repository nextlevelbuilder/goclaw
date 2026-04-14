package http

import (
	"net/http"
	"regexp"

	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// validPkgName allows alphanumeric, hyphens, underscores, dots, @, / (for scoped npm).
// Rejects names starting with - to prevent argument injection.
var validPkgName = regexp.MustCompile(`^[a-zA-Z0-9@][a-zA-Z0-9._+\-/@]*$`)

// PackagesHandler handles runtime package management HTTP endpoints.
type PackagesHandler struct{}

// NewPackagesHandler creates a handler for package management endpoints.
func NewPackagesHandler() *PackagesHandler {
	return &PackagesHandler{}
}

// RegisterRoutes registers all package management routes on the given mux.
func (h *PackagesHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/packages", h.readAuth(h.handleList))
	mux.HandleFunc("POST /v1/packages/install", h.adminAuth(h.handleInstall))
	mux.HandleFunc("POST /v1/packages/uninstall", h.adminAuth(h.handleUninstall))
	mux.HandleFunc("GET /v1/packages/runtimes", h.readAuth(h.handleRuntimes))
	mux.HandleFunc("GET /v1/shell-deny-groups", h.readAuth(h.handleDenyGroups))
}

// readAuth allows viewer+ for read operations.
func (h *PackagesHandler) readAuth(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth("", next)
}

// adminAuth requires admin role for write operations (install/uninstall).
// Prevents agents from calling these endpoints even if they obtain the gateway token,
// since agent requests via browser pairing only get operator role.
func (h *PackagesHandler) adminAuth(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth(permissions.RoleAdmin, next)
}

// handleList returns all installed packages grouped by category (system/pip/npm).
func (h *PackagesHandler) handleList(w http.ResponseWriter, r *http.Request) {
	pkgs := skills.ListInstalledPackages(r.Context())
	writeJSON(w, http.StatusOK, pkgs)
}

// parseAndValidatePackage reads and validates a package name from the request body.
// Returns the validated package string or writes an error response and returns empty.
func parseAndValidatePackage(w http.ResponseWriter, r *http.Request) string {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var body struct {
		Package string `json:"package"`
	}
	if !bindJSON(w, r, extractLocale(r), &body) {
		return ""
	}
	if body.Package == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "package required"})
		return ""
	}

	// Strip prefix for validation, then validate the bare package name.
	name := body.Package
	for _, prefix := range []string{"pip:", "npm:"} {
		if len(name) > len(prefix) && name[:len(prefix)] == prefix {
			name = name[len(prefix):]
			break
		}
	}
	if !validPkgName.MatchString(name) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid package name"})
		return ""
	}

	return body.Package
}

// handleInstall installs a single package.
// Body: {"package": "github-cli"} or {"package": "pip:pandas"} or {"package": "npm:typescript"}
//
// Phase 0b hotfix: server-wide package installation (pip/npm/apk) must be
// restricted to master-scope callers. Non-master tenant admins previously
// reached this handler because the adminAuth middleware only checks role, not
// tenant scope — a supply-chain vector (CRITICAL-2 in the audit report).
func (h *PackagesHandler) handleInstall(w http.ResponseWriter, r *http.Request) {
	if !requireMasterScope(w, r) {
		return
	}
	pkg := parseAndValidatePackage(w, r)
	if pkg == "" {
		return
	}
	ok, errMsg := skills.InstallSingleDep(r.Context(), pkg)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": errMsg})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleUninstall removes a single package.
// Body: {"package": "github-cli"} or {"package": "pip:pandas"} or {"package": "npm:typescript"}
//
// Phase 0b hotfix: same master-scope guard as handleInstall — uninstall can
// break system skills, causing server-wide DoS for every tenant.
func (h *PackagesHandler) handleUninstall(w http.ResponseWriter, r *http.Request) {
	if !requireMasterScope(w, r) {
		return
	}
	pkg := parseAndValidatePackage(w, r)
	if pkg == "" {
		return
	}
	ok, errMsg := skills.UninstallPackage(r.Context(), pkg)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": errMsg})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleRuntimes returns the availability of prerequisite runtimes.
func (h *PackagesHandler) handleRuntimes(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, skills.CheckRuntimes())
}

// handleDenyGroups returns all registered shell deny groups with name, description, and default state.
func (h *PackagesHandler) handleDenyGroups(w http.ResponseWriter, _ *http.Request) {
	type groupInfo struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Default     bool   `json:"default"`
	}
	groups := make([]groupInfo, 0, len(tools.DenyGroupRegistry))
	for _, name := range tools.DenyGroupNames() {
		g := tools.DenyGroupRegistry[name]
		groups = append(groups, groupInfo{
			Name:        g.Name,
			Description: g.Description,
			Default:     g.Default,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"groups": groups})
}
