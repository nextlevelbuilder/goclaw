package http

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// handleListInstances returns all user instances for a predefined agent.
func (h *AgentsHandler) handleListInstances(w http.ResponseWriter, r *http.Request) {
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
		writeJSON(w, http.StatusForbidden, map[string]string{"error": i18n.T(locale, i18n.MsgOwnerOnly, "view instances")})
		return
	}

	instances, err := h.agents.ListUserInstances(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"instances": instances})
}

// handleGetInstanceFiles returns user context files for a specific instance.
func (h *AgentsHandler) handleGetInstanceFiles(w http.ResponseWriter, r *http.Request) {
	callerID := store.UserIDFromContext(r.Context())
	locale := store.LocaleFromContext(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "agent")})
		return
	}
	instanceUserID := r.PathValue("userID")
	if instanceUserID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "userID")})
		return
	}

	ag, err := h.agents.GetByID(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "agent", id.String())})
		return
	}
	if callerID != "" && ag.OwnerID != callerID && !h.isOwnerUser(callerID) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": i18n.T(locale, i18n.MsgOwnerOnly, "view instance files")})
		return
	}

	files, err := h.agents.GetUserContextFiles(r.Context(), id, instanceUserID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"files": files})
}

// handleSetInstanceFile updates a user context file for a specific instance.
func (h *AgentsHandler) handleSetInstanceFile(w http.ResponseWriter, r *http.Request) {
	callerID := store.UserIDFromContext(r.Context())
	locale := store.LocaleFromContext(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "agent")})
		return
	}
	instanceUserID := r.PathValue("userID")
	fileName := r.PathValue("fileName")
	if instanceUserID == "" || fileName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "userID and fileName")})
		return
	}

	// Only USER.md can be edited via this endpoint — other files are managed by the agent itself.
	if fileName != "USER.md" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, "only USER.md can be edited via this endpoint")})
		return
	}

	ag, err := h.agents.GetByID(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "agent", id.String())})
		return
	}
	if callerID != "" && ag.OwnerID != callerID && !h.isOwnerUser(callerID) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": i18n.T(locale, i18n.MsgOwnerOnly, "edit instance files")})
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1MB limit
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, err.Error())})
		return
	}
	var payload struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return
	}

	// Ensure user profile exists (creates row if needed, e.g. admin adds contact manually).
	if err := h.agents.EnsureUserProfile(r.Context(), id, instanceUserID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if err := h.agents.SetUserContextFile(r.Context(), id, instanceUserID, fileName, payload.Content); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Invalidate caches so the agent picks up the change immediately
	h.emitCacheInvalidate(bus.CacheKindBootstrap, id.String())

	emitAudit(h.msgBus, r, "agent_instance.file_set", "agent_instance", id.String())
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// handleUpdateInstanceMetadata updates metadata for a user instance.
func (h *AgentsHandler) handleUpdateInstanceMetadata(w http.ResponseWriter, r *http.Request) {
	callerID := store.UserIDFromContext(r.Context())
	locale := store.LocaleFromContext(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "agent")})
		return
	}
	instanceUserID := r.PathValue("userID")
	if instanceUserID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "userID")})
		return
	}

	ag, err := h.agents.GetByID(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "agent", id.String())})
		return
	}
	if callerID != "" && ag.OwnerID != callerID && !h.isOwnerUser(callerID) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": i18n.T(locale, i18n.MsgOwnerOnly, "edit instance metadata")})
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, err.Error())})
		return
	}
	var payload struct {
		Metadata map[string]string `json:"metadata"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return
	}
	if len(payload.Metadata) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "metadata")})
		return
	}

	if err := h.agents.UpdateUserProfileMetadata(r.Context(), id, instanceUserID, payload.Metadata); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	emitAudit(h.msgBus, r, "agent_instance.metadata_updated", "agent_instance", id.String())
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}
