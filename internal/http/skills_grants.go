package http

import (
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func (h *SkillsHandler) handleListAgentSkills(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	agentIDStr := r.PathValue("agentID")
	agentID, err := uuid.Parse(agentIDStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "agent")})
		return
	}

	skills, err := h.skills.ListWithGrantStatus(r.Context(), agentID)
	if err != nil {
		slog.Error("failed to list skills with grant status", "agent_id", agentID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToList, "skills")})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"skills": skills})
}

func (h *SkillsHandler) handleListAgentGrants(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	idStr := r.PathValue("id")
	skillID, err := uuid.Parse(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "skill")})
		return
	}

	grants, err := h.skills.ListAgentGrantsForSkill(r.Context(), skillID)
	if err != nil {
		slog.Error("failed to list skill agent grants", "skill_id", skillID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToList, "skill grants")})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"grants": grants})
}

func (h *SkillsHandler) handleListUserGrants(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	idStr := r.PathValue("id")
	skillID, err := uuid.Parse(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "skill")})
		return
	}

	grants, err := h.skills.ListUserGrantsForSkill(r.Context(), skillID)
	if err != nil {
		slog.Error("failed to list skill user grants", "skill_id", skillID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToList, "skill grants")})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"grants": grants})
}

func (h *SkillsHandler) handleGrantAgent(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	userID := store.UserIDFromContext(r.Context())
	idStr := r.PathValue("id")
	skillID, err := uuid.Parse(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "skill")})
		return
	}

	// Ownership check (admins bypass)
	auth := resolveAuth(r)
	if !permissions.HasMinRole(auth.Role, permissions.RoleAdmin) {
		if ownerID, found := h.skills.GetSkillOwnerID(r.Context(), skillID); found && ownerID != userID {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "only the skill owner can perform this action"})
			return
		}
	}

	var req struct {
		AgentID       string `json:"agent_id"`
		Version       int    `json:"version"`
		PinnedVersion int    `json:"pinned_version"`
		CanManage     *bool  `json:"can_manage"`
	}
	if !bindJSON(w, r, locale, &req) {
		return
	}

	agentID, err := uuid.Parse(req.AgentID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "agent")})
		return
	}

	if req.PinnedVersion > 0 {
		req.Version = req.PinnedVersion
	}
	if req.Version <= 0 {
		req.Version = 1
	}

	var grantErr error
	if req.CanManage == nil {
		grantErr = h.skills.GrantToAgent(r.Context(), skillID, agentID, req.Version, userID)
	} else {
		grantErr = h.skills.GrantToAgent(r.Context(), skillID, agentID, req.Version, userID, *req.CanManage)
	}
	if grantErr != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": grantErr.Error()})
		return
	}

	h.skills.BumpVersion()
	h.emitCacheInvalidate(bus.CacheKindSkillGrants, "", uuid.Nil)
	emitAudit(h.msgBus, r, "skill.grant_changed", "skill", idStr)
	writeJSON(w, http.StatusCreated, map[string]string{"ok": "true"})
}

func (h *SkillsHandler) handleRevokeAgent(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	idStr := r.PathValue("id")
	skillID, err := uuid.Parse(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "skill")})
		return
	}

	// Ownership check (admins bypass)
	auth := resolveAuth(r)
	if !permissions.HasMinRole(auth.Role, permissions.RoleAdmin) {
		userID := store.UserIDFromContext(r.Context())
		if ownerID, found := h.skills.GetSkillOwnerID(r.Context(), skillID); found && ownerID != userID {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "only the skill owner can perform this action"})
			return
		}
	}

	agentIDStr := r.PathValue("agentID")
	agentID, err := uuid.Parse(agentIDStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "agent")})
		return
	}

	if err := h.skills.RevokeFromAgent(r.Context(), skillID, agentID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	h.skills.BumpVersion()
	h.emitCacheInvalidate(bus.CacheKindSkillGrants, "", uuid.Nil)
	emitAudit(h.msgBus, r, "skill.grant_changed", "skill", idStr)
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

func (h *SkillsHandler) handleGrantUser(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	userID := store.UserIDFromContext(r.Context())
	idStr := r.PathValue("id")
	skillID, err := uuid.Parse(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "skill")})
		return
	}

	// Ownership check (admins bypass)
	auth := resolveAuth(r)
	if !permissions.HasMinRole(auth.Role, permissions.RoleAdmin) {
		if ownerID, found := h.skills.GetSkillOwnerID(r.Context(), skillID); found && ownerID != userID {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "only the skill owner can perform this action"})
			return
		}
	}

	var req struct {
		UserID string `json:"user_id"`
	}
	if !bindJSON(w, r, locale, &req) {
		return
	}
	if req.UserID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "user_id")})
		return
	}
	if err := store.ValidateUserID(req.UserID); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !h.requireTenantUser(w, r, req.UserID) {
		return
	}

	if err := h.skills.GrantToUser(r.Context(), skillID, req.UserID, userID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	h.skills.BumpVersion()
	h.emitCacheInvalidate(bus.CacheKindSkillGrants, "", uuid.Nil)
	emitAudit(h.msgBus, r, "skill.grant_changed", "skill", idStr)
	writeJSON(w, http.StatusCreated, map[string]string{"ok": "true"})
}

func (h *SkillsHandler) handleRevokeUser(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	idStr := r.PathValue("id")
	skillID, err := uuid.Parse(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "skill")})
		return
	}

	// Ownership check (admins bypass)
	auth := resolveAuth(r)
	if !permissions.HasMinRole(auth.Role, permissions.RoleAdmin) {
		userID := store.UserIDFromContext(r.Context())
		if ownerID, found := h.skills.GetSkillOwnerID(r.Context(), skillID); found && ownerID != userID {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "only the skill owner can perform this action"})
			return
		}
	}

	targetUserID := r.PathValue("userID")
	if err := store.ValidateUserID(targetUserID); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := h.skills.RevokeFromUser(r.Context(), skillID, targetUserID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	h.skills.BumpVersion()
	h.emitCacheInvalidate(bus.CacheKindSkillGrants, "", uuid.Nil)
	emitAudit(h.msgBus, r, "skill.grant_changed", "skill", idStr)
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

func (h *SkillsHandler) requireTenantUser(w http.ResponseWriter, r *http.Request, userID string) bool {
	locale := store.LocaleFromContext(r.Context())
	if h.tenantStore == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": i18n.T(locale, i18n.MsgNotImplemented, "tenant store")})
		return false
	}
	tenantID := store.TenantIDFromContext(r.Context())
	if tenantID == uuid.Nil {
		tenantID = store.MasterTenantID
	}
	role, err := h.tenantStore.GetUserRole(r.Context(), tenantID, userID)
	if err != nil {
		slog.Error("skill_grants: tenant user membership check failed", "tenant_id", tenantID, "target_user_id", userID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToList, "tenant users")})
		return false
	}
	if role == "" {
		slog.Warn("security.skill_grant_user_not_tenant_member",
			"tenant_id", tenantID,
			"target_user_id", userID,
			"user_id", store.UserIDFromContext(r.Context()))
		writeJSON(w, http.StatusForbidden, map[string]string{"error": i18n.T(locale, i18n.MsgPermissionDenied, "tenant user")})
		return false
	}
	return true
}

// --- Helpers ---
