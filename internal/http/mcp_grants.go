package http

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func (h *MCPHandler) handleGrantAgent(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	serverID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "server")})
		return
	}

	var req struct {
		AgentID   string          `json:"agent_id"`
		ToolAllow json.RawMessage `json:"tool_allow,omitempty"`
		ToolDeny  json.RawMessage `json:"tool_deny,omitempty"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return
	}

	agentID, err := uuid.Parse(req.AgentID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "agent")})
		return
	}

	grant := store.MCPAgentGrant{
		ServerID:  serverID,
		AgentID:   agentID,
		Enabled:   true,
		ToolAllow: req.ToolAllow,
		ToolDeny:  req.ToolDeny,
		GrantedBy: store.UserIDFromContext(r.Context()),
	}

	if err := h.store.GrantToAgent(r.Context(), &grant); err != nil {
		slog.Error("mcp.grant_agent", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	h.emitCacheInvalidate()
	emitAudit(h.msgBus, r, "mcp_server.agent_granted", "mcp_server", serverID.String())
	writeJSON(w, http.StatusCreated, map[string]string{"status": "granted"})
}

func (h *MCPHandler) handleRevokeAgent(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	serverID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "server")})
		return
	}

	agentID, err := uuid.Parse(r.PathValue("agentID"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "agent")})
		return
	}

	if err := h.store.RevokeFromAgent(r.Context(), serverID, agentID); err != nil {
		slog.Error("mcp.revoke_agent", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	h.emitCacheInvalidate()
	emitAudit(h.msgBus, r, "mcp_server.agent_revoked", "mcp_server", serverID.String())
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

func (h *MCPHandler) handleListAgentGrants(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	agentID, err := uuid.Parse(r.PathValue("agentID"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "agent")})
		return
	}

	grants, err := h.store.ListAgentGrants(r.Context(), agentID)
	if err != nil {
		slog.Error("mcp.list_agent_grants", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"grants": grants})
}

func (h *MCPHandler) handleListServerGrants(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	serverID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "server")})
		return
	}

	grants, err := h.store.ListServerGrants(r.Context(), serverID)
	if err != nil {
		slog.Error("mcp.list_server_grants", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToList, "grants")})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"grants": grants})
}

func (h *MCPHandler) handleGrantUser(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	serverID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "server")})
		return
	}

	var req struct {
		UserID    string          `json:"user_id"`
		ToolAllow json.RawMessage `json:"tool_allow,omitempty"`
		ToolDeny  json.RawMessage `json:"tool_deny,omitempty"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
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

	grant := store.MCPUserGrant{
		ServerID:  serverID,
		UserID:    req.UserID,
		Enabled:   true,
		ToolAllow: req.ToolAllow,
		ToolDeny:  req.ToolDeny,
		GrantedBy: store.UserIDFromContext(r.Context()),
	}

	if err := h.store.GrantToUser(r.Context(), &grant); err != nil {
		slog.Error("mcp.grant_user", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	h.emitCacheInvalidate()
	emitAudit(h.msgBus, r, "mcp_server.user_granted", "mcp_server", serverID.String())
	writeJSON(w, http.StatusCreated, map[string]string{"status": "granted"})
}

func (h *MCPHandler) handleRevokeUser(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	serverID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "server")})
		return
	}

	targetUserID := r.PathValue("userID")
	if err := store.ValidateUserID(targetUserID); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if err := h.store.RevokeFromUser(r.Context(), serverID, targetUserID); err != nil {
		slog.Error("mcp.revoke_user", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	h.emitCacheInvalidate()
	emitAudit(h.msgBus, r, "mcp_server.user_revoked", "mcp_server", serverID.String())
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}
