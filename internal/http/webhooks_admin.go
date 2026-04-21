package http

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/edition"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// Compile-time assertion: WebhooksAdminHandler must implement routeRegistrar
// (the interface defined in internal/gateway/server.go).
var _ interface{ RegisterRoutes(mux *http.ServeMux) } = (*WebhooksAdminHandler)(nil)

// webhookKinds is the set of valid webhook kinds.
var webhookKinds = map[string]bool{
	"llm":     true,
	"message": true,
}

// WebhooksAdminHandler implements CRUD for webhook registry entries.
// All endpoints are tenant-admin-gated (requireTenantAdmin).
// encKey is the AES-256-GCM encryption key (GOCLAW_ENCRYPTION_KEY); if empty, encrypted_secret
// is stored as "" and HMAC auth requires rotation before it can be used.
type WebhooksAdminHandler struct {
	webhooks store.WebhookStore
	tenants  store.TenantStore
	msgBus   *bus.MessageBus
	encKey   string // AES-256-GCM key for encrypting raw webhook secrets at rest
}

// NewWebhooksAdminHandler creates a handler for webhook admin endpoints.
func NewWebhooksAdminHandler(webhooks store.WebhookStore, tenants store.TenantStore, msgBus *bus.MessageBus) *WebhooksAdminHandler {
	return &WebhooksAdminHandler{
		webhooks: webhooks,
		tenants:  tenants,
		msgBus:   msgBus,
	}
}

// SetEncKey sets the AES-256-GCM encryption key used to encrypt raw webhook secrets at rest.
// Must be called before the first Create/Rotate request; safe to call at startup only.
func (h *WebhooksAdminHandler) SetEncKey(encKey string) {
	h.encKey = encKey
}

// RegisterRoutes registers all webhook admin routes on mux.
// Admin CRUD routes mount for both editions.
// Runtime routes (/v1/webhooks/message, /v1/webhooks/llm) are mounted by phases 05/06
// conditionally: message-kind only if edition.Current().AllowsChannels().
func (h *WebhooksAdminHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/webhooks", h.handleCreate)
	mux.HandleFunc("GET /v1/webhooks", h.handleList)
	mux.HandleFunc("GET /v1/webhooks/{id}", h.handleGet)
	mux.HandleFunc("PATCH /v1/webhooks/{id}", h.handleUpdate)
	mux.HandleFunc("POST /v1/webhooks/{id}/rotate", h.handleRotate)
	mux.HandleFunc("DELETE /v1/webhooks/{id}", h.handleRevoke)
}

// --- Create ---

// createWebhookReq is the request body for POST /v1/webhooks.
type createWebhookReq struct {
	Name            string     `json:"name"`
	Kind            string     `json:"kind"` // "llm" | "message"
	AgentID         *uuid.UUID `json:"agent_id,omitempty"`
	Scopes          []string   `json:"scopes,omitempty"`
	ChannelID       *uuid.UUID `json:"channel_id,omitempty"`
	RateLimitPerMin int        `json:"rate_limit_per_min,omitempty"`
	IPAllowlist     []string   `json:"ip_allowlist,omitempty"`
	RequireHMAC     bool       `json:"require_hmac,omitempty"`
	LocalhostOnly   bool       `json:"localhost_only,omitempty"`
}

// webhookCreateResp is the response for create and rotate — includes raw secret once.
// hmac_signing_key = raw secret itself — callers sign HMAC requests using raw secret bytes.
// The raw secret is encrypted at rest; secret_hash is kept only for bearer-token lookup.
type webhookCreateResp struct {
	ID             uuid.UUID  `json:"id"`
	TenantID       uuid.UUID  `json:"tenant_id"`
	AgentID        *uuid.UUID `json:"agent_id,omitempty"`
	Name           string     `json:"name"`
	Kind           string     `json:"kind"`
	SecretPrefix   string     `json:"secret_prefix"`
	Secret         string     `json:"secret"`           // raw secret — shown ONCE; use this as HMAC key
	HMACSigningKey string     `json:"hmac_signing_key"` // same as Secret — raw bytes for X-GoClaw-Signature
	Scopes         []string   `json:"scopes"`
	ChannelID      *uuid.UUID `json:"channel_id,omitempty"`
	RateLimitPerMin int       `json:"rate_limit_per_min"`
	IPAllowlist    []string   `json:"ip_allowlist"`
	RequireHMAC    bool       `json:"require_hmac"`
	LocalhostOnly  bool       `json:"localhost_only"`
	CreatedAt      time.Time  `json:"created_at"`
}

func (h *WebhooksAdminHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)

	// Auth first — don't leak config state (encKey presence) to unauthenticated callers.
	if !requireTenantAdmin(w, r, h.tenants) {
		slog.Warn("security.webhook.admin_denied", "action", "create", "path", r.URL.Path,
			"user_id", store.UserIDFromContext(r.Context()))
		return
	}

	// Defense-in-depth: primary guard is skip-mount in gateway_http_wiring.go.
	// This secondary guard protects if the handler is ever wired without an encKey
	// (e.g. test harness or future refactor that bypasses the wiring guard).
	if h.encKey == "" {
		slog.Error("security.webhook.admin_no_enc_key", "action", "create")
		writeError(w, http.StatusServiceUnavailable, protocol.ErrInternal, i18n.T(locale, i18n.MsgWebhookEncryptionUnavailable))
		return
	}

	var req createWebhookReq
	if !bindJSON(w, r, locale, &req) {
		return
	}

	// Validate required fields.
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "name"))
		return
	}
	if len(req.Name) > 100 {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "name must be 100 characters or less"))
		return
	}
	if !webhookKinds[req.Kind] {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "kind must be 'llm' or 'message'"))
		return
	}

	// Edition gate: message kind requires channels edition.
	if req.Kind == "message" && !edition.Current().AllowsChannels() {
		writeError(w, http.StatusForbidden, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgInvalidRequest, "message webhooks require Standard edition"))
		return
	}

	// Lite edition: force localhost_only=true for all webhook kinds.
	if !edition.Current().AllowsChannels() {
		req.LocalhostOnly = true
	}

	raw, secretHash, secretPrefix, err := generateWebhookSecret()
	if err != nil {
		slog.Error("webhook.admin.secret_generate_failed", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "secret generation"))
		return
	}

	// Encrypt raw secret at rest. If encKey is empty, encryptedSecret is "" (requires rotation).
	encryptedSecret, encErr := crypto.Encrypt(raw, h.encKey)
	if encErr != nil {
		slog.Error("webhook.admin.secret_encrypt_failed", "error", encErr)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "secret encryption"))
		return
	}

	ctx := r.Context()
	tenantID := store.TenantIDFromContext(ctx)
	now := time.Now()

	wh := &store.WebhookData{
		ID:              store.GenNewID(),
		TenantID:        tenantID,
		AgentID:         req.AgentID,
		Name:            req.Name,
		Kind:            req.Kind,
		SecretPrefix:    secretPrefix,
		SecretHash:      secretHash,
		EncryptedSecret: encryptedSecret,
		Scopes:          req.Scopes,
		ChannelID:       req.ChannelID,
		RateLimitPerMin: req.RateLimitPerMin,
		IPAllowlist:     req.IPAllowlist,
		RequireHMAC:     req.RequireHMAC,
		LocalhostOnly:   req.LocalhostOnly,
		Revoked:         false,
		CreatedBy:       extractUserID(r),
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if wh.Scopes == nil {
		wh.Scopes = []string{}
	}
	if wh.IPAllowlist == nil {
		wh.IPAllowlist = []string{}
	}

	if err := h.webhooks.Create(ctx, wh); err != nil {
		slog.Error("webhook.admin.create_failed", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToCreate, "webhook", "internal error"))
		return
	}

	slog.Info("webhook.created", "id", wh.ID, "tenant_id", tenantID, "actor", wh.CreatedBy, "kind", wh.Kind)
	h.emitCacheInvalidate(wh.ID.String())

	writeJSON(w, http.StatusCreated, webhookCreateResp{
		ID:              wh.ID,
		TenantID:        wh.TenantID,
		AgentID:         wh.AgentID,
		Name:            wh.Name,
		Kind:            wh.Kind,
		SecretPrefix:    wh.SecretPrefix,
		Secret:          raw,
		HMACSigningKey:  raw, // raw secret bytes are the HMAC key (encrypted at rest; decrypted at sign time)
		Scopes:          wh.Scopes,
		ChannelID:       wh.ChannelID,
		RateLimitPerMin: wh.RateLimitPerMin,
		IPAllowlist:     wh.IPAllowlist,
		RequireHMAC:     wh.RequireHMAC,
		LocalhostOnly:   wh.LocalhostOnly,
		CreatedAt:       wh.CreatedAt,
	})
}

// --- List ---

func (h *WebhooksAdminHandler) handleList(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)

	if !requireTenantAdmin(w, r, h.tenants) {
		slog.Warn("security.webhook.admin_denied", "action", "list", "path", r.URL.Path,
			"user_id", store.UserIDFromContext(r.Context()))
		return
	}

	// Optional ?agent_id= filter.
	var f store.WebhookListFilter
	if agentIDStr := r.URL.Query().Get("agent_id"); agentIDStr != "" {
		aid, err := uuid.Parse(agentIDStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "agent_id"))
			return
		}
		f.AgentID = &aid
	}

	rows, err := h.webhooks.List(r.Context(), f)
	if err != nil {
		slog.Error("webhook.admin.list_failed", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToList, "webhooks"))
		return
	}
	if rows == nil {
		rows = []store.WebhookData{}
	}
	writeJSON(w, http.StatusOK, rows)
}

// --- Get ---

func (h *WebhooksAdminHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)

	if !requireTenantAdmin(w, r, h.tenants) {
		slog.Warn("security.webhook.admin_denied", "action", "get", "path", r.URL.Path,
			"user_id", store.UserIDFromContext(r.Context()))
		return
	}

	id, ok := parseWebhookID(w, r, locale)
	if !ok {
		return
	}

	wh, err := h.webhooks.GetByID(r.Context(), id)
	if err != nil || wh == nil {
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "webhook", id.String()))
		return
	}

	// Cross-tenant isolation: GetByID is tenant-scoped via context, but verify explicitly.
	tenantID := store.TenantIDFromContext(r.Context())
	if !store.IsOwnerRole(r.Context()) && wh.TenantID != tenantID {
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "webhook", id.String()))
		return
	}

	writeJSON(w, http.StatusOK, wh)
}

// --- Update ---

// updateWebhookReq is the request body for PATCH /v1/webhooks/{id}.
// All fields are optional; omitted fields are not changed.
type updateWebhookReq struct {
	Name            *string    `json:"name,omitempty"`
	Scopes          []string   `json:"scopes,omitempty"`
	ChannelID       *uuid.UUID `json:"channel_id,omitempty"`
	RateLimitPerMin *int       `json:"rate_limit_per_min,omitempty"`
	IPAllowlist     []string   `json:"ip_allowlist,omitempty"`
	RequireHMAC     *bool      `json:"require_hmac,omitempty"`
	LocalhostOnly   *bool      `json:"localhost_only,omitempty"`
}

func (h *WebhooksAdminHandler) handleUpdate(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)

	if !requireTenantAdmin(w, r, h.tenants) {
		slog.Warn("security.webhook.admin_denied", "action", "update", "path", r.URL.Path,
			"user_id", store.UserIDFromContext(r.Context()))
		return
	}

	id, ok := parseWebhookID(w, r, locale)
	if !ok {
		return
	}

	ctx := r.Context()

	// Verify ownership before mutating.
	wh, err := h.webhooks.GetByID(ctx, id)
	if err != nil || wh == nil {
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "webhook", id.String()))
		return
	}
	tenantID := store.TenantIDFromContext(ctx)
	if !store.IsOwnerRole(ctx) && wh.TenantID != tenantID {
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "webhook", id.String()))
		return
	}

	var req updateWebhookReq
	if !bindJSON(w, r, locale, &req) {
		return
	}

	updates := make(map[string]any)
	if req.Name != nil {
		if *req.Name == "" {
			writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "name"))
			return
		}
		if len(*req.Name) > 100 {
			writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "name must be 100 characters or less"))
			return
		}
		updates["name"] = *req.Name
	}
	if req.Scopes != nil {
		updates["scopes"] = req.Scopes
	}
	if req.ChannelID != nil {
		updates["channel_id"] = *req.ChannelID
	}
	if req.RateLimitPerMin != nil {
		updates["rate_limit_per_min"] = *req.RateLimitPerMin
	}
	if req.IPAllowlist != nil {
		updates["ip_allowlist"] = req.IPAllowlist
	}
	if req.RequireHMAC != nil {
		updates["require_hmac"] = *req.RequireHMAC
	}
	if req.LocalhostOnly != nil {
		// Lite edition: cannot unset localhost_only.
		if !*req.LocalhostOnly && !edition.Current().AllowsChannels() {
			writeError(w, http.StatusForbidden, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgInvalidRequest, "localhost_only cannot be disabled on Lite edition"))
			return
		}
		updates["localhost_only"] = *req.LocalhostOnly
	}

	if len(updates) == 0 {
		// Nothing to update — return current state.
		writeJSON(w, http.StatusOK, wh)
		return
	}

	if err := h.webhooks.Update(ctx, id, updates); err != nil {
		slog.Error("webhook.admin.update_failed", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToUpdate, "webhook", "internal error"))
		return
	}

	slog.Info("webhook.updated", "id", id, "tenant_id", tenantID, "actor", extractUserID(r))

	// Re-fetch to return updated state.
	updated, err := h.webhooks.GetByID(ctx, id)
	if err != nil || updated == nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "fetch updated webhook"))
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// --- Rotate Secret ---

func (h *WebhooksAdminHandler) handleRotate(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)

	// Auth first — don't leak config state (encKey presence) to unauthenticated callers.
	if !requireTenantAdmin(w, r, h.tenants) {
		slog.Warn("security.webhook.admin_denied", "action", "rotate", "path", r.URL.Path,
			"user_id", store.UserIDFromContext(r.Context()))
		return
	}

	// Defense-in-depth: same guard as handleCreate — encryption key must be present
	// before we generate and persist a new secret.
	if h.encKey == "" {
		slog.Error("security.webhook.admin_no_enc_key", "action", "rotate")
		writeError(w, http.StatusServiceUnavailable, protocol.ErrInternal, i18n.T(locale, i18n.MsgWebhookEncryptionUnavailable))
		return
	}

	id, ok := parseWebhookID(w, r, locale)
	if !ok {
		return
	}

	ctx := r.Context()

	// Verify ownership before mutating.
	wh, err := h.webhooks.GetByID(ctx, id)
	if err != nil || wh == nil {
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "webhook", id.String()))
		return
	}
	tenantID := store.TenantIDFromContext(ctx)
	if !store.IsOwnerRole(ctx) && wh.TenantID != tenantID {
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "webhook", id.String()))
		return
	}

	raw, newHash, newPrefix, err := generateWebhookSecret()
	if err != nil {
		slog.Error("webhook.admin.secret_generate_failed", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "secret generation"))
		return
	}

	newEncryptedSecret, encErr := crypto.Encrypt(raw, h.encKey)
	if encErr != nil {
		slog.Error("webhook.admin.secret_encrypt_failed", "error", encErr)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "secret encryption"))
		return
	}

	if err := h.webhooks.RotateSecret(ctx, id, newHash, newPrefix, newEncryptedSecret); err != nil {
		slog.Error("webhook.admin.rotate_failed", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "rotate secret"))
		return
	}

	slog.Info("webhook.rotated", "id", id, "tenant_id", tenantID, "actor", extractUserID(r))

	// Invalidate the cache so the middleware picks up the new hash immediately.
	h.emitCacheInvalidate(id.String())

	writeJSON(w, http.StatusOK, map[string]any{
		"id":               id,
		"secret":           raw,    // new raw secret — shown ONCE; use as HMAC key
		"hmac_signing_key": raw,    // same as secret; raw bytes are HMAC key (encrypted at rest)
		"secret_prefix":    newPrefix,
	})
}

// --- Revoke ---

func (h *WebhooksAdminHandler) handleRevoke(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)

	if !requireTenantAdmin(w, r, h.tenants) {
		slog.Warn("security.webhook.admin_denied", "action", "revoke", "path", r.URL.Path,
			"user_id", store.UserIDFromContext(r.Context()))
		return
	}

	id, ok := parseWebhookID(w, r, locale)
	if !ok {
		return
	}

	ctx := r.Context()

	// Verify ownership before revoking.
	wh, err := h.webhooks.GetByID(ctx, id)
	if err != nil || wh == nil {
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "webhook", id.String()))
		return
	}
	tenantID := store.TenantIDFromContext(ctx)
	if !store.IsOwnerRole(ctx) && wh.TenantID != tenantID {
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "webhook", id.String()))
		return
	}

	if err := h.webhooks.Revoke(ctx, id); err != nil {
		slog.Error("webhook.admin.revoke_failed", "error", err, "id", id)
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "webhook", id.String()))
		return
	}

	slog.Info("webhook.revoked", "id", id, "tenant_id", tenantID, "actor", extractUserID(r))

	// Invalidate the cache so the middleware rejects the old secret immediately.
	h.emitCacheInvalidate(id.String())

	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

// --- Helpers ---

// generateWebhookSecret creates a new webhook secret in format "wh_<base32(24 bytes)>".
// Returns (rawSecret, secretHash, secretPrefix, error).
// secretPrefix = first 8 chars of rawSecret (includes "wh_" + start of base32).
// secretHash   = hex(SHA-256(rawSecret)) — stored in DB, used as HMAC signing key.
func generateWebhookSecret() (raw, secretHash, secretPrefix string, err error) {
	b := make([]byte, 24)
	if _, err = rand.Read(b); err != nil {
		return "", "", "", err
	}
	// base32 (no padding) produces 40 chars for 24 bytes.
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)
	raw = "wh_" + encoded // total 43 chars

	h := sha256.Sum256([]byte(raw))
	secretHash = hex.EncodeToString(h[:])

	// First 8 chars of the full raw secret (includes "wh_" + first 5 base32 chars).
	secretPrefix = raw[:8]
	return raw, secretHash, secretPrefix, nil
}

// parseWebhookID parses the {id} path value, writing a 400 on error.
func parseWebhookID(w http.ResponseWriter, r *http.Request, locale string) (uuid.UUID, bool) {
	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "webhook"))
		return uuid.Nil, false
	}
	return id, true
}

// emitCacheInvalidate broadcasts a cache invalidation event for webhook secrets.
// This signals the WebhookAuthMiddleware (phase 03) to drop cached entries.
func (h *WebhooksAdminHandler) emitCacheInvalidate(webhookID string) {
	if h.msgBus == nil {
		return
	}
	h.msgBus.Broadcast(bus.Event{
		Name:    protocol.EventCacheInvalidate,
		Payload: bus.CacheInvalidatePayload{Kind: "webhooks", Key: webhookID},
	})
}
