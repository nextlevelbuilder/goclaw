package http

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// GatewayUsersHandler handles gateway user management endpoints.
type GatewayUsersHandler struct {
	users store.GatewayUserStore
	token string // gateway token for admin auth
}

// NewGatewayUsersHandler creates a handler for gateway user management endpoints.
func NewGatewayUsersHandler(users store.GatewayUserStore, token string) *GatewayUsersHandler {
	return &GatewayUsersHandler{users: users, token: token}
}

// RegisterRoutes registers all gateway user management routes on the given mux.
func (h *GatewayUsersHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/gateway-users", h.adminAuth(h.handleList))
	mux.HandleFunc("POST /v1/gateway-users", h.adminAuth(h.handleCreate))
	mux.HandleFunc("DELETE /v1/gateway-users/{id}", h.adminAuth(h.handleDelete))
}

// adminAuth ensures the caller has admin access.
func (h *GatewayUsersHandler) adminAuth(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth(h.token, permissions.RoleAdmin, next)
}

func (h *GatewayUsersHandler) handleList(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	users, err := h.users.List(r.Context())
	if err != nil {
		slog.Error("gateway_users.list failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToList, "gateway users")})
		return
	}
	if users == nil {
		users = []store.GatewayUserData{}
	}
	writeJSON(w, http.StatusOK, users)
}

func (h *GatewayUsersHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)

	var input struct {
		UserID string `json:"user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return
	}

	if input.UserID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "user_id")})
		return
	}

	if len(input.UserID) > 255 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, "user_id must be 255 characters or less")})
		return
	}

	// Prevent creating users with role "root"
	if input.UserID == "root" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, "cannot create root user via API")})
		return
	}

	// Check if user_id already exists
	if existing, _ := h.users.GetByUserID(r.Context(), input.UserID); existing != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, "user_id already exists")})
		return
	}

	// Generate gateway token
	token, err := generateGatewayToken()
	if err != nil {
		slog.Error("gateway_users.generate_token failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "token generation")})
		return
	}

	user := &store.GatewayUserData{
		ID:           store.GenNewID(),
		UserID:       input.UserID,
		GatewayToken: token,
		Role:         "admin",
	}

	if err := h.users.Create(r.Context(), user); err != nil {
		slog.Error("gateway_users.create failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToCreate, "gateway user", "internal error")})
		return
	}

	InvalidateGatewayUserCache()

	// Return user with token (shown only once)
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":            user.ID,
		"user_id":       user.UserID,
		"gateway_token": user.GatewayToken,
		"role":          user.Role,
		"created_at":    user.CreatedAt,
	})
}

func (h *GatewayUsersHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "gateway user")})
		return
	}

	if err := h.users.Delete(r.Context(), id); err != nil {
		if err.Error() == "cannot delete root user" {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, "cannot delete root user")})
			return
		}
		slog.Error("gateway_users.delete failed", "error", err, "id", idStr)
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "gateway user", idStr)})
		return
	}

	InvalidateGatewayUserCache()
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// generateGatewayToken generates a cryptographically secure token.
func generateGatewayToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "gw_" + hex.EncodeToString(b), nil
}
