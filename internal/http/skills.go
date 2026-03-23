package http

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

const maxSkillUploadSize = 20 << 20 // 20 MB

// SkillsHandler handles skill management HTTP endpoints.
type SkillsHandler struct {
	skills     *pg.PGSkillStore
	baseDir    string // filesystem base for skill content (skills-store/) — master tenant
	dataDir    string // parent data dir for tenant-scoped skill paths
	bundledDir string // original bundled skills dir (fallback for broken managed copies)
	token      string
	msgBus     *bus.MessageBus
}

// NewSkillsHandler creates a handler for skill management endpoints.
func NewSkillsHandler(skills *pg.PGSkillStore, baseDir, dataDir, bundledDir, token string, msgBus *bus.MessageBus) *SkillsHandler {
	return &SkillsHandler{skills: skills, baseDir: baseDir, dataDir: dataDir, bundledDir: bundledDir, token: token, msgBus: msgBus}
}

// tenantSkillsDir returns the skills-store directory scoped to the requesting tenant.
// Master tenant returns h.baseDir unchanged (backward compat).
func (h *SkillsHandler) tenantSkillsDir(r *http.Request) string {
	tid := store.TenantIDFromContext(r.Context())
	slug := store.TenantSlugFromContext(r.Context())
	return config.TenantSkillsStoreDir(h.dataDir, tid, slug)
}

// emitCacheInvalidate broadcasts a cache invalidation event if msgBus is set.
func (h *SkillsHandler) emitCacheInvalidate(kind, key string) {
	if h.msgBus == nil {
		return
	}
	h.msgBus.Broadcast(bus.Event{
		Name:    protocol.EventCacheInvalidate,
		Payload: bus.CacheInvalidatePayload{Kind: kind, Key: key},
	})
}

// RegisterRoutes registers all skill management routes on the given mux.
func (h *SkillsHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/skills", h.authMiddleware(h.handleList))
	mux.HandleFunc("POST /v1/skills/upload", h.authMiddleware(h.handleUpload))
	mux.HandleFunc("GET /v1/skills/{id}", h.authMiddleware(h.handleGet))
	mux.HandleFunc("PUT /v1/skills/{id}", h.authMiddleware(h.handleUpdate))
	mux.HandleFunc("DELETE /v1/skills/{id}", h.authMiddleware(h.handleDelete))
	mux.HandleFunc("POST /v1/skills/{id}/grants/agent", h.authMiddleware(h.handleGrantAgent))
	mux.HandleFunc("DELETE /v1/skills/{id}/grants/agent/{agentID}", h.authMiddleware(h.handleRevokeAgent))
	mux.HandleFunc("POST /v1/skills/{id}/grants/user", h.authMiddleware(h.handleGrantUser))
	mux.HandleFunc("DELETE /v1/skills/{id}/grants/user/{userID}", h.authMiddleware(h.handleRevokeUser))
	mux.HandleFunc("GET /v1/agents/{agentID}/skills", h.authMiddleware(h.handleListAgentSkills))
	mux.HandleFunc("GET /v1/skills/{id}/versions", h.authMiddleware(h.handleListVersions))
	mux.HandleFunc("GET /v1/skills/{id}/files/{path...}", h.authMiddleware(h.handleReadFile))
	mux.HandleFunc("GET /v1/skills/{id}/files", h.authMiddleware(h.handleListFiles))
	// System-level operations: admin + master tenant only.
	// These execute shell commands (pip/npm install) and affect the entire server.
	mux.HandleFunc("POST /v1/skills/rescan-deps", h.adminMiddleware(h.handleRescanDeps))
	mux.HandleFunc("POST /v1/skills/install-deps", h.adminMiddleware(h.handleInstallDeps))
	mux.HandleFunc("POST /v1/skills/install-dep", h.adminMiddleware(h.handleInstallDep))
	mux.HandleFunc("GET /v1/skills/runtimes", h.adminMiddleware(h.handleRuntimes))
	mux.HandleFunc("POST /v1/skills/{id}/toggle", h.adminMiddleware(h.handleToggle))
}

func (h *SkillsHandler) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth(h.token, "", next)
}

// adminMiddleware requires admin role — used for system-level operations
// (rescan deps, install packages, toggle skills) that affect the entire server.
func (h *SkillsHandler) adminMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth(h.token, permissions.RoleAdmin, next)
}

// requireMasterTenant rejects requests from non-master tenants.
// System skill management (install packages, rescan deps) is a server-wide operation
// that should only be accessible to the master tenant or cross-tenant admins.
func (h *SkillsHandler) requireMasterTenant(w http.ResponseWriter, r *http.Request) bool {
	ctx := r.Context()
	if store.IsCrossTenant(ctx) {
		return true
	}
	tid := store.TenantIDFromContext(ctx)
	if tid == store.MasterTenantID {
		return true
	}
	locale := store.LocaleFromContext(ctx)
	writeJSON(w, http.StatusForbidden, map[string]string{
		"error": i18n.T(locale, i18n.MsgPermissionDenied, "system skill management"),
	})
	return false
}

func (h *SkillsHandler) handleList(w http.ResponseWriter, r *http.Request) {
	skills := h.skills.ListSkills(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"skills": skills})
}

func (h *SkillsHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	id := r.PathValue("id")
	skill, ok := h.skills.GetSkill(r.Context(), id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "skill", id)})
		return
	}
	writeJSON(w, http.StatusOK, skill)
}

func (h *SkillsHandler) handleUpdate(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "skill")})
		return
	}

	// Ownership check (admins bypass)
	auth := resolveAuth(r, h.token)
	if !permissions.HasMinRole(auth.Role, permissions.RoleAdmin) {
		userID := store.UserIDFromContext(r.Context())
		if ownerID, found := h.skills.GetSkillOwnerID(r.Context(), id); found && ownerID != userID {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "only the skill owner can perform this action"})
			return
		}
	}

	var updates map[string]any
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return
	}
	// Prevent changing sensitive fields (use /toggle endpoint for enabled)
	delete(updates, "id")
	delete(updates, "owner_id")
	delete(updates, "file_path")
	delete(updates, "is_system")
	delete(updates, "enabled")

	if err := h.skills.UpdateSkill(r.Context(), id, updates); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	h.skills.BumpVersion()
	emitAudit(h.msgBus, r, "skill.updated", "skill", idStr)
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

func (h *SkillsHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "skill")})
		return
	}

	// Ownership check (admins bypass)
	auth := resolveAuth(r, h.token)
	if !permissions.HasMinRole(auth.Role, permissions.RoleAdmin) {
		userID := store.UserIDFromContext(r.Context())
		if ownerID, found := h.skills.GetSkillOwnerID(r.Context(), id); found && ownerID != userID {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "only the skill owner can perform this action"})
			return
		}
	}

	if err := h.skills.DeleteSkill(r.Context(), id); err != nil {
		if err.Error() == "cannot delete system skill" {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "cannot delete system skill"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	h.skills.BumpVersion()
	emitAudit(h.msgBus, r, "skill.deleted", "skill", idStr)
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

// handleInstallDeps installs missing dependencies for all system skills, then re-checks status.
func (h *SkillsHandler) handleInstallDeps(w http.ResponseWriter, r *http.Request) {
	if !h.requireMasterTenant(w, r) {
		return
	}
	dirs := h.skills.ListSystemSkillDirs()
	if len(dirs) == 0 {
		writeJSON(w, http.StatusOK, map[string]string{"message": "no system skills"})
		return
	}

	manifest, missing := skills.AggregateMissingDeps(dirs)
	if len(missing) == 0 {
		writeJSON(w, http.StatusOK, map[string]string{"message": "all deps satisfied"})
		return
	}

	if h.msgBus != nil {
		h.msgBus.Broadcast(bus.Event{
			Name:    protocol.EventSkillDepsInstalling,
			Payload: map[string]any{"count": len(missing)},
		})
	}

	result, err := skills.InstallDeps(r.Context(), manifest, missing)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Re-check all system skills and update status after install
	allSkills := h.skills.ListAllSkills(r.Context())
	for _, sk := range allSkills {
		if !sk.IsSystem {
			continue
		}
		dir, exists := dirs[sk.Slug]
		if !exists {
			continue
		}
		m := h.scanWithFallback(sk)
		if m == nil || m.IsEmpty() {
			_ = dir // dir was used for direct scan; fallback uses sk.BaseDir
			continue
		}
		ok, miss := skills.CheckSkillDeps(m)
		id, err := uuid.Parse(sk.ID)
		if err != nil {
			continue
		}
		if ok && sk.Status == "archived" {
			_ = h.skills.UpdateSkill(r.Context(), id, map[string]any{"status": "active"})
			h.skills.BumpVersion()
		}
		status := "active"
		if !ok {
			status = "archived"
		}
		if h.msgBus != nil {
			h.msgBus.Broadcast(bus.Event{
				Name: protocol.EventSkillDepsChecked,
				Payload: map[string]any{
					"slug":    sk.Slug,
					"status":  status,
					"missing": miss,
				},
			})
		}
	}

	if h.msgBus != nil {
		h.msgBus.Broadcast(bus.Event{
			Name:    protocol.EventSkillDepsInstalled,
			Payload: result,
		})
	}

	writeJSON(w, http.StatusOK, result)
}

// handleInstallDep installs a single dependency and re-checks all skill statuses.
// Body: {"dep": "pip:openpyxl"}
func (h *SkillsHandler) handleInstallDep(w http.ResponseWriter, r *http.Request) {
	if !h.requireMasterTenant(w, r) {
		return
	}
	var body struct {
		Dep string `json:"dep"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Dep == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "dep required"})
		return
	}

	if h.msgBus != nil {
		h.msgBus.Broadcast(bus.Event{
			Name:    protocol.EventSkillDepItemInstalling,
			Payload: map[string]any{"dep": body.Dep},
		})
	}

	ok, errMsg := skills.InstallSingleDep(r.Context(), body.Dep)

	if h.msgBus != nil {
		payload := map[string]any{"dep": body.Dep, "ok": ok}
		if errMsg != "" {
			payload["error"] = errMsg
		}
		h.msgBus.Broadcast(bus.Event{
			Name:    protocol.EventSkillDepItemInstalled,
			Payload: payload,
		})
	}

	if ok {
		h.rescanAndUpdate()
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": ok, "error": errMsg})
}

type depResult struct {
	Slug    string   `json:"slug"`
	Status  string   `json:"status"`
	Missing []string `json:"missing,omitempty"`
}

// rescanAndUpdate re-checks all skills and updates their status + missing deps in DB.
func (h *SkillsHandler) rescanAndUpdate() (updated int, results []depResult) {
	allSkills := h.skills.ListAllSkills(store.WithCrossTenant(context.Background()))

	for _, sk := range allSkills {
		manifest := h.scanWithFallback(sk)

		id, err := uuid.Parse(sk.ID)
		if err != nil {
			continue
		}

		if manifest == nil || manifest.IsEmpty() {
			// No deps needed — if archived, recover to active and clear stale deps.
			if sk.Status == "archived" {
				_ = h.skills.StoreMissingDeps(id, nil)
				_ = h.skills.UpdateSkill(store.WithCrossTenant(context.Background()), id, map[string]any{"status": "active"})
				results = append(results, depResult{Slug: sk.Slug, Status: "active"})
				updated++
				slog.Debug("rescan: recovered archived skill (no deps)", "slug", sk.Slug)
			} else {
				results = append(results, depResult{Slug: sk.Slug, Status: "ok"})
			}
			continue
		}

		ok, missing := skills.CheckSkillDeps(manifest)
		_ = h.skills.StoreMissingDeps(id, missing)

		switch {
		case ok && sk.Status == "archived":
			_ = h.skills.UpdateSkill(store.WithCrossTenant(context.Background()), id, map[string]any{"status": "active"})
			results = append(results, depResult{Slug: sk.Slug, Status: "active"})
			updated++
		case !ok && sk.Status == "active":
			_ = h.skills.UpdateSkill(store.WithCrossTenant(context.Background()), id, map[string]any{"status": "archived"})
			results = append(results, depResult{Slug: sk.Slug, Status: "archived", Missing: missing})
			updated++
		case !ok:
			results = append(results, depResult{Slug: sk.Slug, Status: sk.Status, Missing: missing})
		default:
			results = append(results, depResult{Slug: sk.Slug, Status: "ok"})
		}

		slog.Debug("rescan: checked skill", "slug", sk.Slug, "ok", ok, "missing", len(missing))
	}

	if updated > 0 {
		h.skills.BumpVersion()
	}
	return updated, results
}

// scanWithFallback scans skill deps from the managed dir, falling back to the
// bundled dir if the managed copy's scripts/ directory is missing or empty.
// If a fallback scan succeeds, re-copies the bundled scripts to the managed dir.
func (h *SkillsHandler) scanWithFallback(sk store.SkillInfo) *skills.SkillManifest {
	manifest := skills.ScanSkillDeps(sk.BaseDir)
	if manifest != nil && !manifest.IsEmpty() {
		return manifest
	}

	// Fallback: try bundled dir for system skills whose managed copy is broken.
	if !sk.IsSystem || h.bundledDir == "" {
		return manifest
	}

	managedScripts := filepath.Join(sk.BaseDir, "scripts")
	if _, err := os.Stat(managedScripts); err == nil {
		// scripts/ exists in managed dir but scanner found nothing — not a copy issue.
		return manifest
	}

	bundledSkillDir := filepath.Join(h.bundledDir, sk.Slug)
	bundledManifest := skills.ScanSkillDeps(bundledSkillDir)
	if bundledManifest == nil || bundledManifest.IsEmpty() {
		return manifest
	}

	slog.Warn("rescan: managed scripts/ missing, using bundled fallback",
		"slug", sk.Slug, "managed", sk.BaseDir, "bundled", bundledSkillDir)

	// Re-copy bundled scripts to managed dir so future scans work without fallback.
	bundledScripts := filepath.Join(bundledSkillDir, "scripts")
	if err := skills.CopyDir(bundledScripts, managedScripts); err != nil {
		slog.Error("rescan: failed to re-copy bundled scripts", "slug", sk.Slug, "error", err)
	}

	return bundledManifest
}

// handleRescanDeps re-checks dependencies for all skills (including archived) and updates their status.
func (h *SkillsHandler) handleRescanDeps(w http.ResponseWriter, r *http.Request) {
	if !h.requireMasterTenant(w, r) {
		return
	}
	updated, results := h.rescanAndUpdate()
	writeJSON(w, http.StatusOK, map[string]any{
		"updated": updated,
		"results": results,
	})
}

// handleRuntimes returns the availability and version of prerequisite runtimes (python3, node, etc.).
func (h *SkillsHandler) handleRuntimes(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, skills.CheckRuntimes())
}

// handleToggle enables or disables a skill.
// Body: {"enabled": bool}
// When enabling: re-checks deps and updates status to "active" or "archived" accordingly.
func (h *SkillsHandler) handleToggle(w http.ResponseWriter, r *http.Request) {
	if !h.requireMasterTenant(w, r) {
		return
	}
	locale := store.LocaleFromContext(r.Context())
	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "skill")})
		return
	}

	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return
	}

	if err := h.skills.ToggleSkill(r.Context(), id, body.Enabled); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	newStatus := ""
	if body.Enabled {
		// Re-check deps for this skill so its status reflects reality after being re-enabled.
		sk, ok := h.skills.GetSkillByID(r.Context(), id)
		if ok {
			manifest := h.scanWithFallback(sk)
			if manifest != nil && !manifest.IsEmpty() {
				depOk, missing := skills.CheckSkillDeps(manifest)
				_ = h.skills.StoreMissingDeps(id, missing)
				if depOk {
					newStatus = "active"
				} else {
					newStatus = "archived"
				}
			} else {
				newStatus = "active"
			}
			_ = h.skills.UpdateSkill(r.Context(), id, map[string]any{"status": newStatus})
		}
	}

	h.skills.BumpVersion()
	emitAudit(h.msgBus, r, "skill.toggled", "skill", idStr)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "enabled": body.Enabled, "status": newStatus})
}
