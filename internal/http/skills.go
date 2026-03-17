package http

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
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
	skills  *pg.PGSkillStore
	baseDir string // filesystem base for skill content
	token   string
	msgBus  *bus.MessageBus
}

// NewSkillsHandler creates a handler for skill management endpoints.
func NewSkillsHandler(skills *pg.PGSkillStore, baseDir, token string, msgBus *bus.MessageBus) *SkillsHandler {
	return &SkillsHandler{skills: skills, baseDir: baseDir, token: token, msgBus: msgBus}
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
	mux.HandleFunc("POST /v1/skills/rescan-deps", h.authMiddleware(h.handleRescanDeps))
	mux.HandleFunc("POST /v1/skills/install-deps", h.authMiddleware(h.handleInstallDeps))
	mux.HandleFunc("POST /v1/skills/install-dep", h.authMiddleware(h.handleInstallDep))
	mux.HandleFunc("GET /v1/skills/runtimes", h.authMiddleware(h.handleRuntimes))
	mux.HandleFunc("POST /v1/skills/{id}/toggle", h.authMiddleware(h.handleToggle))
}

func (h *SkillsHandler) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth(h.token, "", next)
}

func (h *SkillsHandler) handleList(w http.ResponseWriter, r *http.Request) {
	skills := h.skills.ListSkills()
	writeJSON(w, http.StatusOK, map[string]any{"skills": skills})
}

func (h *SkillsHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	id := r.PathValue("id")
	skill, ok := h.skills.GetSkill(id)
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
		if ownerID, found := h.skills.GetSkillOwnerID(id); found && ownerID != userID {
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

	if err := h.skills.UpdateSkill(id, updates); err != nil {
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
		if ownerID, found := h.skills.GetSkillOwnerID(id); found && ownerID != userID {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "only the skill owner can perform this action"})
			return
		}
	}

	if err := h.skills.DeleteSkill(id); err != nil {
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
	allSkills := h.skills.ListAllSkills()
	for _, sk := range allSkills {
		if !sk.IsSystem {
			continue
		}
		dir, exists := dirs[sk.Slug]
		if !exists {
			continue
		}
		m := skills.ScanSkillDeps(dir)
		if m == nil || m.IsEmpty() {
			continue
		}
		ok, miss := skills.CheckSkillDeps(m)
		id, err := uuid.Parse(sk.ID)
		if err != nil {
			continue
		}
		if ok && sk.Status == "archived" {
			_ = h.skills.UpdateSkill(id, map[string]any{"status": "active"})
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
	allSkills := h.skills.ListAllSkills()

	for _, sk := range allSkills {
		manifest := skills.ScanSkillDeps(sk.BaseDir)
		if manifest == nil || manifest.IsEmpty() {
			results = append(results, depResult{Slug: sk.Slug, Status: "ok"})
			continue
		}

		ok, missing := skills.CheckSkillDeps(manifest)
		id, err := uuid.Parse(sk.ID)
		if err != nil {
			continue
		}

		_ = h.skills.StoreMissingDeps(id, missing)

		switch {
		case ok && sk.Status == "archived":
			_ = h.skills.UpdateSkill(id, map[string]any{"status": "active"})
			results = append(results, depResult{Slug: sk.Slug, Status: "active"})
			updated++
		case !ok && sk.Status == "active":
			_ = h.skills.UpdateSkill(id, map[string]any{"status": "archived"})
			results = append(results, depResult{Slug: sk.Slug, Status: "archived", Missing: missing})
			updated++
		case !ok:
			results = append(results, depResult{Slug: sk.Slug, Status: sk.Status, Missing: missing})
		default:
			results = append(results, depResult{Slug: sk.Slug, Status: "ok"})
		}
	}

	if updated > 0 {
		h.skills.BumpVersion()
	}
	return updated, results
}

// handleRescanDeps re-checks dependencies for all skills (including archived) and updates their status.
func (h *SkillsHandler) handleRescanDeps(w http.ResponseWriter, r *http.Request) {
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

	if err := h.skills.ToggleSkill(id, body.Enabled); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	newStatus := ""
	if body.Enabled {
		// Re-check deps for this skill so its status reflects reality after being re-enabled.
		sk, ok := h.skills.GetSkillByID(id)
		if ok {
			manifest := skills.ScanSkillDeps(sk.BaseDir)
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
			_ = h.skills.UpdateSkill(id, map[string]any{"status": newStatus})
		}
	}

	h.skills.BumpVersion()
	emitAudit(h.msgBus, r, "skill.toggled", "skill", idStr)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "enabled": body.Enabled, "status": newStatus})
}
