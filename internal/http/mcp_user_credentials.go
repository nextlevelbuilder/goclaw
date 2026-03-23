package http

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// MCPUserCredentialsHandler handles per-user MCP credential endpoints.
type MCPUserCredentialsHandler struct {
	store store.MCPServerStore
	token string
}

// NewMCPUserCredentialsHandler creates a handler for MCP user credential endpoints.
func NewMCPUserCredentialsHandler(s store.MCPServerStore, token string) *MCPUserCredentialsHandler {
	return &MCPUserCredentialsHandler{store: s, token: token}
}

// RegisterRoutes registers MCP user credential routes.
func (h *MCPUserCredentialsHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("PUT /v1/mcp/servers/{id}/user-credentials", h.auth(h.handleSet))
	mux.HandleFunc("GET /v1/mcp/servers/{id}/user-credentials", h.auth(h.handleGet))
	mux.HandleFunc("DELETE /v1/mcp/servers/{id}/user-credentials", h.auth(h.handleDelete))
}

func (h *MCPUserCredentialsHandler) auth(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth(h.token, "", next)
}

func (h *MCPUserCredentialsHandler) handleSet(w http.ResponseWriter, r *http.Request) {
	serverID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid server ID"})
		return
	}

	userID := store.UserIDFromContext(r.Context())
	if userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "user context required"})
		return
	}

	var creds store.MCPUserCredentials
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&creds); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if err := h.store.SetUserCredentials(r.Context(), serverID, userID, creds); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (h *MCPUserCredentialsHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	serverID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid server ID"})
		return
	}

	userID := store.UserIDFromContext(r.Context())
	if userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "user context required"})
		return
	}

	creds, err := h.store.GetUserCredentials(r.Context(), serverID, userID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"has_credentials": false})
		return
	}

	// Return masked status (never expose actual credentials)
	writeJSON(w, http.StatusOK, map[string]any{
		"has_credentials": true,
		"has_api_key":     creds.APIKey != "",
		"has_headers":     len(creds.Headers) > 0,
		"has_env":         len(creds.Env) > 0,
	})
}

func (h *MCPUserCredentialsHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	serverID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid server ID"})
		return
	}

	userID := store.UserIDFromContext(r.Context())
	if userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "user context required"})
		return
	}

	if err := h.store.DeleteUserCredentials(r.Context(), serverID, userID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
