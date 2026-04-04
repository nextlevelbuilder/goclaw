package http

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// APIKeysHandler handles API key management endpoints.
type APIKeysHandler struct {
	apiKeys store.APIKeyStore
	msgBus  *bus.MessageBus // for cache invalidation events
}

// NewAPIKeysHandler creates a handler for API key management endpoints.
func NewAPIKeysHandler(apiKeys store.APIKeyStore, msgBus *bus.MessageBus) *APIKeysHandler {
	return &APIKeysHandler{apiKeys: apiKeys, msgBus: msgBus}
}

// RegisterRoutes registers all API key management routes on the given mux.
func (h *APIKeysHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/api-keys", h.adminAuth(h.handleList))
	mux.HandleFunc("POST /v1/api-keys", h.adminAuth(h.handleCreate))
	mux.HandleFunc("POST /v1/api-keys/{id}/revoke", h.adminAuth(h.handleRevoke))
}

// adminAuth ensures the caller has admin access (gateway token or API key with admin scope).
func (h *APIKeysHandler) adminAuth(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth(permissions.RoleAdmin, next)
}

func (h *APIKeysHandler) handleList(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	// HTTP API key list is admin-only (adminAuth middleware), so no owner filter needed.
	keys, err := h.apiKeys.List(r.Context(), "")
	if err != nil {
		slog.Error("api_keys.list failed", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToList, "API keys"))
		return
	}
	if keys == nil {
		keys = []store.APIKeyData{}
	}
	writeJSON(w, http.StatusOK, keys)
}

func (h *APIKeysHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)

	var input struct {
		Name      string   `json:"name"`
		Scopes    []string `json:"scopes"`
		ExpiresIn *int     `json:"expires_in"` // seconds; nil = never
		TenantID  string   `json:"tenant_id"`  // optional UUID; cross-tenant callers may specify or omit (NULL = system key)
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON))
		return
	}

	if input.Name == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "name"))
		return
	}
	if len(input.Name) > 100 {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "name must be 100 characters or less"))
		return
	}
	if len(input.Scopes) == 0 {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "scopes"))
		return
	}

	// Validate scopes
	for _, s := range input.Scopes {
		if !permissions.ValidScope(s) {
			writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "invalid scope: "+s))
			return
		}
	}

	raw, hash, prefix, err := crypto.GenerateAPIKey()
	if err != nil {
		slog.Error("api_keys.generate failed", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "key generation"))
		return
	}

	// Resolve tenant_id based on caller type.
	var tenantID uuid.UUID // uuid.Nil = system-level (NULL in DB)
	if store.IsOwnerRole(r.Context()) {
		if input.TenantID != "" {
			tid, err := uuid.Parse(input.TenantID)
			if err != nil {
				writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "tenant_id"))
				return
			}
			tenantID = tid
		}
		// else: uuid.Nil stays → system-level key
	} else {
		tenantID = store.TenantIDFromContext(r.Context())
	}

	now := time.Now()
	key := &store.APIKeyData{
		ID:        store.GenNewID(),
		Name:      input.Name,
		Prefix:    prefix,
		KeyHash:   hash,
		Scopes:    input.Scopes,
		TenantID:  tenantID,
		CreatedBy: extractUserID(r),
		CreatedAt: now,
		UpdatedAt: now,
	}

	if input.ExpiresIn != nil && *input.ExpiresIn > 0 {
		exp := now.Add(time.Duration(*input.ExpiresIn) * time.Second)
		key.ExpiresAt = &exp
	}

	if err := h.apiKeys.Create(r.Context(), key); err != nil {
		slog.Error("api_keys.create failed", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToCreate, "API key", "internal error"))
		return
	}

	h.emitCacheInvalidate("api_keys", key.ID.String())

	// Return key with raw secret (shown only once)
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":         key.ID,
		"name":       key.Name,
		"prefix":     key.Prefix,
		"key":        raw, // shown only once!
		"scopes":     key.Scopes,
		"expires_at": key.ExpiresAt,
		"created_at": key.CreatedAt,
	})
}

func (h *APIKeysHandler) handleRevoke(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "API key"))
		return
	}

	// HTTP revoke is admin-only (adminAuth middleware), so no owner filter needed.
	if err := h.apiKeys.Revoke(r.Context(), id, ""); err != nil {
		slog.Error("api_keys.revoke failed", "error", err, "id", idStr)
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "API key", idStr))
		return
	}

	h.emitCacheInvalidate("api_keys", idStr)
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

func (h *APIKeysHandler) emitCacheInvalidate(kind, key string) {
	if h.msgBus == nil {
		return
	}
	h.msgBus.Broadcast(bus.Event{
		Name:    protocol.EventCacheInvalidate,
		Payload: bus.CacheInvalidatePayload{Kind: kind, Key: key},
	})
}
