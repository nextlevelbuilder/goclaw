package http

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type skillAccessResponse struct {
	Skill       skillRef                    `json:"skill"`
	Visibility  string                      `json:"visibility"`
	AgentGrants []store.SkillAgentGrantInfo `json:"agent_grants"`
	UserGrants  []store.SkillUserGrantInfo  `json:"user_grants"`
}

type skillEffectiveAccessResponse struct {
	Skill         skillRef `json:"skill"`
	Accessible    bool     `json:"accessible"`
	Reason        string   `json:"reason"`
	CanManage     bool     `json:"can_manage"`
	PinnedVersion *int     `json:"pinned_version,omitempty"`
}

func (h *SkillsHandler) handleGetSkillAccess(w http.ResponseWriter, r *http.Request) {
	sk, ok := h.lifecycleSkill(w, r, false)
	if !ok {
		return
	}
	agentGrants, err := h.skills.ListAgentGrantsForSkill(r.Context(), mustParseUUID(sk.ID))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	userGrants, err := h.skills.ListUserGrantsForSkill(r.Context(), mustParseUUID(sk.ID))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, skillAccessResponse{
		Skill:       refForSkill(sk),
		Visibility:  sk.Visibility,
		AgentGrants: agentGrants,
		UserGrants:  userGrants,
	})
}

func (h *SkillsHandler) handlePatchSkillAccess(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	sk, ok := h.lifecycleSkill(w, r, false)
	if !ok {
		return
	}
	if sk.IsSystem && !h.requireMasterTenant(w, r) {
		return
	}
	var req struct {
		Mode       string `json:"mode"`
		Visibility string `json:"visibility"`
	}
	if !bindJSON(w, r, locale, &req) {
		return
	}
	visibility := req.Visibility
	if visibility == "" {
		visibility = req.Mode
	}
	if err := skills.ValidateVisibility(visibility); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidVisibility, visibility)})
		return
	}
	visibility = skills.NormalizeVisibility(visibility)
	id := mustParseUUID(sk.ID)
	updateCtx := r.Context()
	if sk.IsSystem {
		updateCtx = store.WithCrossTenant(store.WithTenantID(updateCtx, store.MasterTenantID))
	}
	if err := h.skills.UpdateSkill(updateCtx, id, map[string]any{"visibility": visibility}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	h.skills.BumpVersion()
	h.emitCacheInvalidate(bus.CacheKindSkills, sk.ID, uuid.Nil)
	emitAudit(h.msgBus, r, "skill.access_updated", "skill", sk.ID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "visibility": visibility})
}

func (h *SkillsHandler) handleGetSkillEffectiveAccess(w http.ResponseWriter, r *http.Request) {
	sk, ok := h.lifecycleSkill(w, r, false)
	if !ok {
		return
	}
	agentID, userID, ok := parseEffectiveAccessParams(w, r)
	if !ok {
		return
	}
	if !h.requireTenantUser(w, r, userID) {
		return
	}
	idx, err := h.buildEffectiveAccessIndex(r, agentID, userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	resp := effectiveAccessForSkill(r.Context(), sk, idx, userID)
	writeJSON(w, http.StatusOK, resp)
}

func (h *SkillsHandler) handleListEffectiveAccess(w http.ResponseWriter, r *http.Request) {
	agentID, userID, ok := parseEffectiveAccessParams(w, r)
	if !ok {
		return
	}
	if !h.requireTenantUser(w, r, userID) {
		return
	}
	idx, err := h.buildEffectiveAccessIndex(r, agentID, userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	skills := h.skills.ListAllSkills(r.Context())
	out := make([]skillEffectiveAccessResponse, 0, len(skills))
	for _, sk := range skills {
		out = append(out, effectiveAccessForSkill(r.Context(), sk, idx, userID))
	}
	writeJSON(w, http.StatusOK, map[string]any{"skills": out})
}

func parseEffectiveAccessParams(w http.ResponseWriter, r *http.Request) (uuid.UUID, string, bool) {
	locale := store.LocaleFromContext(r.Context())
	agentID, err := uuid.Parse(r.URL.Query().Get("agent_id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "agent")})
		return uuid.Nil, "", false
	}
	userID := r.URL.Query().Get("user_id")
	if err := store.ValidateUserID(userID); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return uuid.Nil, "", false
	}
	return agentID, userID, true
}

func mustParseUUID(raw string) uuid.UUID {
	id, _ := uuid.Parse(raw)
	return id
}
