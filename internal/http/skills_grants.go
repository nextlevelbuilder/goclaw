package http

import (
	"archive/zip"
	"io"
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
		AgentID string `json:"agent_id"`
		Version int    `json:"version"`
	}
	if !bindJSON(w, r, locale, &req) {
		return
	}

	agentID, err := uuid.Parse(req.AgentID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "agent")})
		return
	}

	if req.Version <= 0 {
		req.Version = 1
	}

	if err := h.skills.GrantToAgent(r.Context(), skillID, agentID, req.Version, userID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
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

// --- Helpers ---

func readZipFile(f *zip.File) (string, error) {
	rc, err := f.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
