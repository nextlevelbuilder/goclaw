package http

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func (h *AgentsHandler) handleListShares(w http.ResponseWriter, r *http.Request) {
	userID := store.UserIDFromContext(r.Context())
	locale := store.LocaleFromContext(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "agent")})
		return
	}

	// Only owner can list shares
	ag, err := h.agents.GetByID(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "agent", id.String())})
		return
	}
	if userID != "" && ag.OwnerID != userID && !h.isOwnerUser(userID) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": i18n.T(locale, i18n.MsgOwnerOnly, "view shares")})
		return
	}

	shares, err := h.agents.ListShares(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"shares": shares})
}

func (h *AgentsHandler) handleShare(w http.ResponseWriter, r *http.Request) {
	userID := store.UserIDFromContext(r.Context())
	locale := store.LocaleFromContext(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "agent")})
		return
	}

	// Only owner can share
	ag, err := h.agents.GetByID(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "agent", id.String())})
		return
	}
	if userID != "" && ag.OwnerID != userID && !h.isOwnerUser(userID) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": i18n.T(locale, i18n.MsgOwnerOnly, "share agent")})
		return
	}

	var req struct {
		UserID string `json:"user_id"`
		Role   string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, err.Error())})
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
	if req.Role == "" {
		req.Role = "user"
	}

	if err := h.agents.ShareAgent(r.Context(), id, req.UserID, req.Role, userID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	emitAudit(h.msgBus, r, "agent.shared", "agent", id.String())
	writeJSON(w, http.StatusCreated, map[string]string{"ok": "true"})
}

func (h *AgentsHandler) handleRevokeShare(w http.ResponseWriter, r *http.Request) {
	userID := store.UserIDFromContext(r.Context())
	locale := store.LocaleFromContext(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "agent")})
		return
	}

	// Only owner can revoke shares
	ag, err := h.agents.GetByID(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "agent", id.String())})
		return
	}
	if userID != "" && ag.OwnerID != userID && !h.isOwnerUser(userID) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": i18n.T(locale, i18n.MsgOwnerOnly, "revoke shares")})
		return
	}

	targetUserID := r.PathValue("userID")
	if err := store.ValidateUserID(targetUserID); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := h.agents.RevokeShare(r.Context(), id, targetUserID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	emitAudit(h.msgBus, r, "agent.share_revoked", "agent", id.String())
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

func (h *AgentsHandler) handleRegenerate(w http.ResponseWriter, r *http.Request) {
	userID := store.UserIDFromContext(r.Context())
	locale := store.LocaleFromContext(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "agent")})
		return
	}

	// Only owner can regenerate
	ag, err := h.agents.GetByID(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "agent", id.String())})
		return
	}
	if userID != "" && ag.OwnerID != userID && !h.isOwnerUser(userID) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": i18n.T(locale, i18n.MsgOwnerOnly, "regenerate agent")})
		return
	}
	if ag.Status == store.AgentStatusSummoning {
		writeJSON(w, http.StatusConflict, map[string]string{"error": i18n.T(locale, i18n.MsgAlreadySummoning)})
		return
	}
	if h.summoner == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": i18n.T(locale, i18n.MsgSummoningUnavailable)})
		return
	}

	var req struct {
		Prompt string `json:"prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, err.Error())})
		return
	}
	if req.Prompt == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "prompt")})
		return
	}

	// Set status to summoning
	if err := h.agents.Update(r.Context(), id, map[string]any{"status": store.AgentStatusSummoning}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	go h.summoner.RegenerateAgent(id, ag.Provider, ag.Model, req.Prompt)

	emitAudit(h.msgBus, r, "agent.regenerated", "agent", id.String())
	writeJSON(w, http.StatusAccepted, map[string]string{"ok": "true", "status": store.AgentStatusSummoning})
}

// handleResummon re-runs SummonAgent from scratch using the original description.
// Used when initial summoning failed (e.g. wrong model) and user wants to retry.
func (h *AgentsHandler) handleResummon(w http.ResponseWriter, r *http.Request) {
	userID := store.UserIDFromContext(r.Context())
	locale := store.LocaleFromContext(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "agent")})
		return
	}

	ag, err := h.agents.GetByID(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "agent", id.String())})
		return
	}
	if userID != "" && ag.OwnerID != userID && !h.isOwnerUser(userID) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": i18n.T(locale, i18n.MsgOwnerOnly, "resummon agent")})
		return
	}
	if ag.Status == store.AgentStatusSummoning {
		writeJSON(w, http.StatusConflict, map[string]string{"error": i18n.T(locale, i18n.MsgAlreadySummoning)})
		return
	}
	if h.summoner == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": i18n.T(locale, i18n.MsgSummoningUnavailable)})
		return
	}

	description := extractDescription(ag.OtherConfig)
	if description == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgNoDescription)})
		return
	}

	if err := h.agents.Update(r.Context(), id, map[string]any{"status": store.AgentStatusSummoning}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	go h.summoner.SummonAgent(id, ag.Provider, ag.Model, description)

	emitAudit(h.msgBus, r, "agent.resummoned", "agent", id.String())
	writeJSON(w, http.StatusAccepted, map[string]string{"ok": "true", "status": store.AgentStatusSummoning})
}

// extractDescription pulls the description string from other_config JSONB.
func extractDescription(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var cfg map[string]interface{}
	if json.Unmarshal(raw, &cfg) != nil {
		return ""
	}
	desc, _ := cfg["description"].(string)
	return desc
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
