package http

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/edition"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
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

// webhookLLMTester runs a server-side test invocation of an llm-kind webhook using the
// admin's already-authorized session (no webhook secret required). Implemented by *WebhookLLMHandler.
type webhookLLMTester interface {
	RunTest(ctx context.Context, wh *store.WebhookData, input, model string) (*webhookLLMSyncResp, error)
}

// webhookMsgTester runs a server-side test invocation of a message-kind webhook. Implemented by
// *WebhookMessageHandler. nil on Lite edition (channels unavailable) — handleTest guards on nil.
type webhookMsgTester interface {
	RunTest(ctx context.Context, wh *store.WebhookData, req webhookMessageReq) (*webhookMessageResp, error)
}

// WebhooksAdminHandler implements CRUD for webhook registry entries.
// All endpoints are tenant-admin-gated (requireTenantAdmin).
// encKey is the AES-256-GCM encryption key (GOCLAW_ENCRYPTION_KEY); if empty, encrypted_secret
// is stored as "" and HMAC auth requires rotation before it can be used.
type WebhooksAdminHandler struct {
	webhooks  store.WebhookStore
	calls     store.WebhookCallStore
	tenants   store.TenantStore
	msgBus    *bus.MessageBus
	encKey    string // AES-256-GCM key for encrypting raw webhook secrets at rest
	llmTester webhookLLMTester
	msgTester webhookMsgTester
}

// NewWebhooksAdminHandler creates a handler for webhook admin endpoints.
// calls may be nil — the delivery-history endpoint returns 503 when unset.
func NewWebhooksAdminHandler(webhooks store.WebhookStore, calls store.WebhookCallStore, tenants store.TenantStore, msgBus *bus.MessageBus) *WebhooksAdminHandler {
	return &WebhooksAdminHandler{
		webhooks: webhooks,
		calls:    calls,
		tenants:  tenants,
		msgBus:   msgBus,
	}
}

// SetEncKey sets the AES-256-GCM encryption key used to encrypt raw webhook secrets at rest.
// Must be called before the first Create/Rotate request; safe to call at startup only.
func (h *WebhooksAdminHandler) SetEncKey(encKey string) {
	h.encKey = encKey
}

// SetTesters wires the runtime invocation handlers used by POST /v1/webhooks/{id}/test.
// Concrete pointer params (not interfaces) so callers can pass typed-nil safely: a nil
// *WebhookMessageHandler on Lite leaves msgTester as a true nil interface. Safe to call at startup only.
func (h *WebhooksAdminHandler) SetTesters(llm *WebhookLLMHandler, msg *WebhookMessageHandler) {
	if llm != nil {
		h.llmTester = llm
	}
	if msg != nil {
		h.msgTester = msg
	}
}

// RegisterRoutes registers all webhook admin routes on mux.
// Admin CRUD routes mount for both editions.
// Runtime routes (/v1/webhooks/message, /v1/webhooks/llm) are mounted by phases 05/06
// conditionally: message-kind only if edition.Current().AllowsChannels().
func (h *WebhooksAdminHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/webhooks", h.requireAdmin(h.handleCreate))
	mux.HandleFunc("GET /v1/webhooks", h.requireAdmin(h.handleList))
	mux.HandleFunc("GET /v1/webhooks/{id}", h.requireAdmin(h.handleGet))
	mux.HandleFunc("PATCH /v1/webhooks/{id}", h.requireAdmin(h.handleUpdate))
	mux.HandleFunc("POST /v1/webhooks/{id}/rotate", h.requireAdmin(h.handleRotate))
	mux.HandleFunc("DELETE /v1/webhooks/{id}", h.requireAdmin(h.handleRevoke))
	mux.HandleFunc("GET /v1/webhooks/{id}/calls", h.requireAdmin(h.handleListCalls))
	mux.HandleFunc("GET /v1/webhooks/{id}/calls/{callId}", h.requireAdmin(h.handleGetCall))
	mux.HandleFunc("POST /v1/webhooks/{id}/test", h.requireAdmin(h.handleTest))
}

func (h *WebhooksAdminHandler) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if role := permissions.Role(store.RoleFromContext(r.Context())); role != "" {
			if !permissions.HasMinRole(role, permissions.RoleAdmin) {
				writeJSON(w, http.StatusForbidden, map[string]string{
					"error": i18n.T(store.LocaleFromContext(r.Context()), i18n.MsgPermissionDenied, r.URL.Path+" requires "+string(permissions.RoleAdmin)+" role"),
				})
				return
			}
			next(w, r)
			return
		}
		requireAuth(permissions.RoleAdmin, next)(w, r)
	}
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
	ID              uuid.UUID  `json:"id"`
	TenantID        uuid.UUID  `json:"tenant_id"`
	AgentID         *uuid.UUID `json:"agent_id,omitempty"`
	Name            string     `json:"name"`
	Kind            string     `json:"kind"`
	SecretPrefix    string     `json:"secret_prefix"`
	Secret          string     `json:"secret"`           // raw secret — shown ONCE; use this as HMAC key
	HMACSigningKey  string     `json:"hmac_signing_key"` // same as Secret — raw bytes for X-GoClaw-Signature
	Scopes          []string   `json:"scopes"`
	ChannelID       *uuid.UUID `json:"channel_id,omitempty"`
	RateLimitPerMin int        `json:"rate_limit_per_min"`
	IPAllowlist     []string   `json:"ip_allowlist"`
	RequireHMAC     bool       `json:"require_hmac"`
	LocalhostOnly   bool       `json:"localhost_only"`
	CreatedAt       time.Time  `json:"created_at"`
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

	f := store.WebhookListFilter{
		Query:          r.URL.Query().Get("q"),
		IncludeRevoked: r.URL.Query().Get("include_revoked") == "true",
		Limit:          webhookListDefaultLimit,
	}
	if agentIDStr := r.URL.Query().Get("agent_id"); agentIDStr != "" {
		aid, err := uuid.Parse(agentIDStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "agent_id"))
			return
		}
		f.AgentID = &aid
	}
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, perr := strconv.Atoi(l); perr == nil && n > 0 {
			if n > webhookListMaxLimit {
				n = webhookListMaxLimit
			}
			f.Limit = n
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if n, perr := strconv.Atoi(o); perr == nil && n >= 0 {
			f.Offset = n
		}
	}

	rows, err := h.webhooks.List(r.Context(), f)
	if err != nil {
		slog.Error("webhook.admin.list_failed", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToList, "webhooks"))
		return
	}
	total, err := h.webhooks.Count(r.Context(), f)
	if err != nil {
		slog.Error("webhook.admin.count_failed", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToList, "webhooks"))
		return
	}
	if rows == nil {
		rows = []store.WebhookData{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":  rows,
		"total":  total,
		"limit":  f.Limit,
		"offset": f.Offset,
	})
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
		"secret":           raw, // new raw secret — shown ONCE; use as HMAC key
		"hmac_signing_key": raw, // same as secret; raw bytes are HMAC key (encrypted at rest)
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

// --- List Calls (delivery history) ---

// webhookCallResp is the trimmed delivery-history record returned to the admin UI.
// Raw request_payload is omitted (may be large / hold caller data); response is the
// already-truncated (≤32 KB) audit body and is safe to surface for debugging.
type webhookCallResp struct {
	ID            uuid.UUID  `json:"id"`
	DeliveryID    uuid.UUID  `json:"delivery_id"`
	Mode          string     `json:"mode"`
	Status        string     `json:"status"`
	Attempts      int        `json:"attempts"`
	NextAttemptAt *time.Time `json:"next_attempt_at,omitempty"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	LastError     *string    `json:"last_error,omitempty"`
	Response      string     `json:"response,omitempty"`
}

const (
	webhookListDefaultLimit = 20
	webhookListMaxLimit     = 200
)

const (
	webhookCallsDefaultLimit = 20
	webhookCallsMaxLimit     = 200
)

func (h *WebhooksAdminHandler) handleListCalls(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)

	if !requireTenantAdmin(w, r, h.tenants) {
		slog.Warn("security.webhook.admin_denied", "action", "list_calls", "path", r.URL.Path,
			"user_id", store.UserIDFromContext(r.Context()))
		return
	}

	if h.calls == nil {
		writeError(w, http.StatusServiceUnavailable, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "call store unavailable"))
		return
	}

	id, ok := parseWebhookID(w, r, locale)
	if !ok {
		return
	}

	ctx := r.Context()

	// Verify ownership before exposing call history (same pattern as handleGet).
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

	f := store.WebhookCallListFilter{WebhookID: &id, Limit: webhookCallsDefaultLimit}
	if s := r.URL.Query().Get("status"); s != "" {
		f.Status = s
	}
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, perr := strconv.Atoi(l); perr == nil && n > 0 {
			if n > webhookCallsMaxLimit {
				n = webhookCallsMaxLimit
			}
			f.Limit = n
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if n, perr := strconv.Atoi(o); perr == nil && n >= 0 {
			f.Offset = n
		}
	}

	rows, err := h.calls.List(ctx, f)
	if err != nil {
		slog.Error("webhook.admin.list_calls_failed", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToList, "webhook calls"))
		return
	}
	total, err := h.calls.Count(ctx, f)
	if err != nil {
		slog.Error("webhook.admin.count_calls_failed", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToList, "webhook calls"))
		return
	}

	out := make([]webhookCallResp, 0, len(rows))
	for i := range rows {
		c := &rows[i]
		out = append(out, webhookCallResp{
			ID:            c.ID,
			DeliveryID:    c.DeliveryID,
			Mode:          c.Mode,
			Status:        c.Status,
			Attempts:      c.Attempts,
			NextAttemptAt: c.NextAttemptAt,
			StartedAt:     c.StartedAt,
			CompletedAt:   c.CompletedAt,
			CreatedAt:     c.CreatedAt,
			LastError:     c.LastError,
			Response:      string(c.Response),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":  out,
		"total":  total,
		"limit":  f.Limit,
		"offset": f.Offset,
	})
}

// --- Get Call (single delivery detail) ---

// webhookCallDetailResp is the full delivery record for GET /v1/webhooks/{id}/calls/{callId}.
// Unlike the list DTO it includes request_payload (canonical audit body) and the full response,
// plus callback_url / idempotency_key — for debugging a single invocation.
type webhookCallDetailResp struct {
	ID             uuid.UUID  `json:"id"`
	WebhookID      uuid.UUID  `json:"webhook_id"`
	AgentID        *uuid.UUID `json:"agent_id,omitempty"`
	DeliveryID     uuid.UUID  `json:"delivery_id"`
	IdempotencyKey *string    `json:"idempotency_key,omitempty"`
	Mode           string     `json:"mode"`
	Status         string     `json:"status"`
	CallbackURL    *string    `json:"callback_url,omitempty"`
	Attempts       int        `json:"attempts"`
	NextAttemptAt  *time.Time `json:"next_attempt_at,omitempty"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	LastError      *string    `json:"last_error,omitempty"`
	RequestPayload string     `json:"request_payload,omitempty"`
	Response       string     `json:"response,omitempty"`
}

func (h *WebhooksAdminHandler) handleGetCall(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)

	if !requireTenantAdmin(w, r, h.tenants) {
		slog.Warn("security.webhook.admin_denied", "action", "get_call", "path", r.URL.Path,
			"user_id", store.UserIDFromContext(r.Context()))
		return
	}

	if h.calls == nil {
		writeError(w, http.StatusServiceUnavailable, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "call store unavailable"))
		return
	}

	id, ok := parseWebhookID(w, r, locale)
	if !ok {
		return
	}
	callID, err := uuid.Parse(r.PathValue("callId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "call"))
		return
	}

	ctx := r.Context()

	// Verify webhook ownership first (same pattern as handleGet/handleListCalls).
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

	// GetByID is tenant-scoped; also verify the call belongs to THIS webhook.
	c, err := h.calls.GetByID(ctx, callID)
	if err != nil || c == nil || c.WebhookID != id {
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "webhook call", callID.String()))
		return
	}
	if !store.IsOwnerRole(ctx) && c.TenantID != tenantID {
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "webhook call", callID.String()))
		return
	}

	writeJSON(w, http.StatusOK, webhookCallDetailResp{
		ID:             c.ID,
		WebhookID:      c.WebhookID,
		AgentID:        c.AgentID,
		DeliveryID:     c.DeliveryID,
		IdempotencyKey: c.IdempotencyKey,
		Mode:           c.Mode,
		Status:         c.Status,
		CallbackURL:    c.CallbackURL,
		Attempts:       c.Attempts,
		NextAttemptAt:  c.NextAttemptAt,
		StartedAt:      c.StartedAt,
		CompletedAt:    c.CompletedAt,
		CreatedAt:      c.CreatedAt,
		LastError:      c.LastError,
		RequestPayload: string(c.RequestPayload),
		Response:       string(c.Response),
	})
}

// --- Test (server-side invocation) ---

// testWebhookReq is the request body for POST /v1/webhooks/{id}/test.
// For kind=llm: input (+ optional model). For kind=message: channel_name/chat_id/content/media_*.
type testWebhookReq struct {
	// llm fields
	Input string `json:"input,omitempty"`
	Model string `json:"model,omitempty"`
	// message fields
	ChannelName    string `json:"channel_name,omitempty"`
	ChatID         string `json:"chat_id,omitempty"`
	Content        string `json:"content,omitempty"`
	MediaURL       string `json:"media_url,omitempty"`
	MediaCaption   string `json:"media_caption,omitempty"`
	FallbackToText bool   `json:"fallback_to_text,omitempty"`
}

func (h *WebhooksAdminHandler) handleTest(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)

	if !requireTenantAdmin(w, r, h.tenants) {
		slog.Warn("security.webhook.admin_denied", "action", "test", "path", r.URL.Path,
			"user_id", store.UserIDFromContext(r.Context()))
		return
	}

	id, ok := parseWebhookID(w, r, locale)
	if !ok {
		return
	}

	ctx := r.Context()

	// Verify ownership before invoking.
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
	if wh.Revoked {
		writeError(w, http.StatusConflict, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgWebhookRevoked))
		return
	}

	var req testWebhookReq
	if !bindJSON(w, r, locale, &req) {
		return
	}

	switch wh.Kind {
	case "llm":
		if h.llmTester == nil {
			writeError(w, http.StatusServiceUnavailable, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "llm tester unavailable"))
			return
		}
		if wh.AgentID == nil {
			writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgWebhookAgentNotFound))
			return
		}
		if req.Input == "" {
			writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "input"))
			return
		}
		resp, runErr := h.llmTester.RunTest(ctx, wh, req.Input, req.Model)
		if runErr != nil {
			slog.Warn("webhook.admin.test_failed", "id", id, "kind", "llm", "error", runErr)
			writeError(w, http.StatusBadGateway, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, runErr.Error()))
			return
		}
		slog.Info("webhook.tested", "id", id, "tenant_id", tenantID, "kind", "llm", "actor", extractUserID(r))
		writeJSON(w, http.StatusOK, resp)
	case "message":
		if h.msgTester == nil {
			writeError(w, http.StatusForbidden, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgWebhookMessageTestRequiresStandard))
			return
		}
		if wh.ChannelID == nil && req.ChannelName == "" {
			writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "channel_name"))
			return
		}
		if req.ChatID == "" {
			writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "chat_id"))
			return
		}
		if req.Content == "" && req.MediaURL == "" {
			writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "content"))
			return
		}
		if len(req.Content) > webhookContentMaxBytes {
			writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "content exceeds 16 KB limit"))
			return
		}
		resp, runErr := h.msgTester.RunTest(ctx, wh, webhookMessageReq{
			ChannelName:    req.ChannelName,
			ChatID:         req.ChatID,
			Content:        req.Content,
			MediaURL:       req.MediaURL,
			MediaCaption:   req.MediaCaption,
			FallbackToText: req.FallbackToText,
		})
		if runErr != nil {
			slog.Warn("webhook.admin.test_failed", "id", id, "kind", "message", "error", runErr)
			writeError(w, http.StatusBadGateway, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, runErr.Error()))
			return
		}
		slog.Info("webhook.tested", "id", id, "tenant_id", tenantID, "kind", "message", "actor", extractUserID(r))
		writeJSON(w, http.StatusOK, resp)
	default:
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "unknown webhook kind"))
	}
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
