package http

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// setConnectionCredentialRequest is the body for PUT
// /v1/agents/{id}/connections/{connID}/credential.
type setConnectionCredentialRequest struct {
	// Type is "api_key". OAuth (subscription) credentials are set via the
	// "Log in with Claude" flow, not this endpoint.
	Type   string `json:"type"`
	Secret string `json:"secret"`
}

// handleSetConnectionCredential stores a per-connection API-key credential
// (encrypted). The secret is written to the encrypted credential store keyed by
// (agent, connection) — never into agents.connected_agents, which is returned to
// clients unmasked. delegate_external resolves it at run time.
func (h *AgentsHandler) handleSetConnectionCredential(w http.ResponseWriter, r *http.Request) {
	if h.credStore == nil {
		writeError(w, http.StatusNotImplemented, protocol.ErrInternal, "connected-agent credentials are not available on this deployment")
		return
	}
	agent, status, err := h.lookupAccessibleAgent(r)
	if err != nil {
		writeError(w, status, protocol.ErrNotFound, err.Error())
		return
	}
	connID := r.PathValue("connID")
	conn := findConnectedAgent(agent, connID)
	if conn == nil {
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, "no such connection on this agent")
		return
	}

	var req setConnectionCredentialRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, "invalid request body")
		return
	}
	secret := strings.TrimSpace(req.Secret)
	if secret == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, "secret is required")
		return
	}
	if req.Type != "" && req.Type != "api_key" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, "unsupported credential type (use the login flow for oauth)")
		return
	}
	inject := injectForConnection(conn.Provider, "api_key")
	if inject == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, "this connection does not support an API-key credential")
		return
	}

	if err := h.credStore.Put(r.Context(), store.ConnectedAgentCredential{
		AgentID:      agent.ID,
		ConnectionID: connID,
		Type:         "api_key",
		Inject:       inject,
		Secret:       secret,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, "failed to store credential")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"connection_id":     connID,
		"credential_type":   "api_key",
		"credential_status": "connected",
	})
}

// handleListConnectionCredentials returns the credential status of each of an
// agent's connections — type + "connected" presence, never the secret. Lets the
// UI show "Connected ✓" after a reload.
func (h *AgentsHandler) handleListConnectionCredentials(w http.ResponseWriter, r *http.Request) {
	out := map[string]map[string]string{}
	if h.credStore == nil {
		writeJSON(w, http.StatusOK, map[string]any{"credentials": out})
		return
	}
	agent, status, err := h.lookupAccessibleAgent(r)
	if err != nil {
		writeError(w, status, protocol.ErrNotFound, err.Error())
		return
	}
	for _, c := range agent.ParseConnectedAgents() {
		cred, err := h.credStore.Get(r.Context(), agent.ID, c.ID)
		if err == nil && cred != nil {
			out[c.ID] = map[string]string{"type": cred.Type, "status": "connected"}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"credentials": out})
}

// handleDeleteConnectionCredential removes a connection's stored credential.
func (h *AgentsHandler) handleDeleteConnectionCredential(w http.ResponseWriter, r *http.Request) {
	if h.credStore == nil {
		writeError(w, http.StatusNotImplemented, protocol.ErrInternal, "connected-agent credentials are not available on this deployment")
		return
	}
	agent, status, err := h.lookupAccessibleAgent(r)
	if err != nil {
		writeError(w, status, protocol.ErrNotFound, err.Error())
		return
	}
	connID := r.PathValue("connID")
	if err := h.credStore.Delete(r.Context(), agent.ID, connID); err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, "failed to delete credential")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"connection_id":     connID,
		"credential_status": "none",
	})
}

// findConnectedAgent returns the connection with the given id on the agent.
func findConnectedAgent(agent *store.AgentData, connID string) *config.ConnectedAgentSpec {
	if connID == "" {
		return nil
	}
	conns := agent.ParseConnectedAgents()
	for i := range conns {
		if conns[i].ID == connID {
			return &conns[i]
		}
	}
	return nil
}

// injectForConnection maps (provider, credType) to the sandbox injection
// descriptor. Empty = unsupported combination.
func injectForConnection(provider, credType string) string {
	switch strings.ToLower(provider) {
	case "claude_code", "claude", "claudecode":
		switch credType {
		case "api_key":
			return "env:ANTHROPIC_API_KEY"
		case "oauth":
			return "env:CLAUDE_CODE_OAUTH_TOKEN"
		}
	}
	return ""
}
