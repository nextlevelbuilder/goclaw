package http

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// MCPUserCredentialsHandler handles per-user MCP credential endpoints.
type MCPUserCredentialsHandler struct {
	store       store.MCPServerStore
	tenantStore store.TenantStore
}

// NewMCPUserCredentialsHandler creates a handler for MCP user credential endpoints.
func NewMCPUserCredentialsHandler(s store.MCPServerStore, ts store.TenantStore) *MCPUserCredentialsHandler {
	return &MCPUserCredentialsHandler{store: s, tenantStore: ts}
}

// RegisterRoutes registers MCP user credential routes.
func (h *MCPUserCredentialsHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("PUT /v1/mcp/servers/{id}/user-credentials", h.auth(h.handleSet))
	mux.HandleFunc("GET /v1/mcp/servers/{id}/user-credentials", h.auth(h.handleGet))
	mux.HandleFunc("DELETE /v1/mcp/servers/{id}/user-credentials", h.auth(h.handleDelete))
}

func (h *MCPUserCredentialsHandler) auth(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth("", next)
}

// resolveTargetUserID returns the effective user ID for credential operations.
// If ?user_id is absent or same as caller, returns callerID (self-service).
// If ?user_id targets another user, checks that caller is system admin or tenant admin/owner.
func (h *MCPUserCredentialsHandler) resolveTargetUserID(r *http.Request, callerID string) (string, int) {
	targetID := r.URL.Query().Get("user_id")
	if targetID == "" || targetID == callerID {
		return callerID, 0
	}

	if err := store.ValidateUserID(targetID); err != nil {
		return "", http.StatusBadRequest
	}

	// System admin can always target another user.
	role := permissions.Role(store.RoleFromContext(r.Context()))
	if permissions.HasMinRole(role, permissions.RoleAdmin) {
		return targetID, 0
	}

	// Tenant admin/owner can target users within their tenant.
	if h.tenantStore != nil {
		tid := store.TenantIDFromContext(r.Context())
		if tid != uuid.Nil {
			callerTenantRole, err := h.tenantStore.GetUserRole(r.Context(), tid, callerID)
			if err == nil && (callerTenantRole == store.TenantRoleOwner || callerTenantRole == store.TenantRoleAdmin) {
				// Verify target user belongs to the same tenant.
				if _, err := h.tenantStore.GetUserRole(r.Context(), tid, targetID); err == nil {
					return targetID, 0
				}
			}
		}
	}

	slog.Warn("security.mcp_credentials_forbidden",
		"caller", callerID,
		"target", targetID,
		"role", string(role),
	)
	return "", http.StatusForbidden
}

func (h *MCPUserCredentialsHandler) handleSet(w http.ResponseWriter, r *http.Request) {
	serverID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid server ID"})
		return
	}

	callerID := store.UserIDFromContext(r.Context())
	if callerID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "user context required"})
		return
	}

	userID, errCode := h.resolveTargetUserID(r, callerID)
	if errCode != 0 {
		writeJSON(w, errCode, map[string]string{"error": httpStatusText(errCode)})
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

	callerID := store.UserIDFromContext(r.Context())
	if callerID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "user context required"})
		return
	}

	userID, errCode := h.resolveTargetUserID(r, callerID)
	if errCode != 0 {
		writeJSON(w, errCode, map[string]string{"error": httpStatusText(errCode)})
		return
	}

	creds, err := h.store.GetUserCredentials(r.Context(), serverID, userID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"has_credentials": false})
		return
	}

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

	callerID := store.UserIDFromContext(r.Context())
	if callerID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "user context required"})
		return
	}

	userID, errCode := h.resolveTargetUserID(r, callerID)
	if errCode != 0 {
		writeJSON(w, errCode, map[string]string{"error": httpStatusText(errCode)})
		return
	}

	if err := h.store.DeleteUserCredentials(r.Context(), serverID, userID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// httpStatusText returns a short error message for common HTTP status codes.
func httpStatusText(code int) string {
	switch code {
	case http.StatusBadRequest:
		return "invalid user_id"
	case http.StatusForbidden:
		return "permission denied: admin or tenant admin required"
	default:
		return http.StatusText(code)
	}
}
