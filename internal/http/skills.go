package http

import (
	"encoding/json"
	"net/http"
	"regexp"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

const maxSkillUploadSize = 20 << 20 // 20 MB

var slugRegexp = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*[a-z0-9]$`)

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
}

func (h *SkillsHandler) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.token != "" {
			if extractBearerToken(r) != h.token {
				locale := extractLocale(r)
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": i18n.T(locale, i18n.MsgUnauthorized)})
				return
			}
		}
		userID := extractUserID(r)
		ctx := store.WithLocale(r.Context(), extractLocale(r))
		if userID != "" {
			ctx = store.WithUserID(ctx, userID)
		}
		r = r.WithContext(ctx)
		next(w, r)
	}
}

func (h *SkillsHandler) handleList(w http.ResponseWriter, r *http.Request) {
	skills := h.skills.ListSkills()
	writeJSON(w, http.StatusOK, map[string]interface{}{"skills": skills})
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

	var updates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return
	}
	// Prevent changing sensitive fields
	delete(updates, "id")
	delete(updates, "owner_id")
	delete(updates, "file_path")
	delete(updates, "is_system")

	if err := h.skills.UpdateSkill(id, updates); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	h.skills.BumpVersion()
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

	if err := h.skills.DeleteSkill(id); err != nil {
		if err.Error() == "cannot delete system skill" {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "cannot delete system skill"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	h.skills.BumpVersion()
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

// handleRescanDeps re-checks dependencies for all skills (including archived) and updates their status.
// Active skills with missing deps → archived. Archived skills with deps now available → active.
func (h *SkillsHandler) handleRescanDeps(w http.ResponseWriter, r *http.Request) {
	allSkills := h.skills.ListAllSkills()
	updated := 0

	type depResult struct {
		Slug    string   `json:"slug"`
		Status  string   `json:"status"`
		Missing []string `json:"missing,omitempty"`
	}
	var results []depResult

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

		if ok && sk.Status == "archived" {
			// Deps now available → re-activate
			_ = h.skills.UpdateSkill(id, map[string]any{"status": "active"})
			results = append(results, depResult{Slug: sk.Slug, Status: "active"})
			updated++
		} else if !ok && sk.Status == "active" {
			// Deps missing → archive
			_ = h.skills.UpdateSkill(id, map[string]any{"status": "archived"})
			results = append(results, depResult{Slug: sk.Slug, Status: "archived", Missing: missing})
			updated++
		} else if !ok {
			results = append(results, depResult{Slug: sk.Slug, Status: sk.Status, Missing: missing})
		} else {
			results = append(results, depResult{Slug: sk.Slug, Status: "ok"})
		}
	}

	if updated > 0 {
		h.skills.BumpVersion()
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"updated": updated,
		"results": results,
	})
}
