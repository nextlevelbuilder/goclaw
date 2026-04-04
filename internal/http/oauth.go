package http

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/oauth"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// OAuthHandler handles OAuth-related HTTP endpoints for web UI.
//
// Limitation: only one OAuth flow can run at a time because the local callback
// server binds to a fixed port (1455). A second flow attempt within the 6-minute
// window returns HTTP 409. This affects all providers, not just the one being
// authenticated.
type OAuthHandler struct {
	provStore   store.ProviderStore
	secretStore store.ConfigSecretsStore
	providerReg *providers.Registry
	msgBus      *bus.MessageBus

	mu            sync.Mutex
	pending       map[string]*pendingOAuthFlow
	activeFlowKey string // only one active flow at a time (fixed callback port)
}

type pendingOAuthFlow struct {
	login        *oauth.PendingLogin
	cancel       context.CancelFunc
	flowKey      string
	tenantID     uuid.UUID
	userID       string
	providerName string
	displayName  string
	apiBase      string
}

// NewOAuthHandler creates a handler for OAuth endpoints.
func NewOAuthHandler(provStore store.ProviderStore, secretStore store.ConfigSecretsStore, providerReg *providers.Registry, msgBus *bus.MessageBus) *OAuthHandler {
	return &OAuthHandler{
		provStore:   provStore,
		secretStore: secretStore,
		providerReg: providerReg,
		msgBus:      msgBus,
		pending:     make(map[string]*pendingOAuthFlow),
	}
}

// RegisterRoutes registers OAuth routes on the given mux.
func (h *OAuthHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/auth/chatgpt/{provider}/status", h.auth(h.handleStatus))
	mux.HandleFunc("GET /v1/auth/chatgpt/{provider}/quota", h.auth(h.handleQuota))
	mux.HandleFunc("POST /v1/auth/chatgpt/{provider}/start", h.auth(h.handleStart))
	mux.HandleFunc("POST /v1/auth/chatgpt/{provider}/callback", h.auth(h.handleManualCallback))
	mux.HandleFunc("POST /v1/auth/chatgpt/{provider}/logout", h.auth(h.handleLogout))

	mux.HandleFunc("GET /v1/auth/openai/status", h.auth(h.handleStatus))
	mux.HandleFunc("GET /v1/auth/openai/quota", h.auth(h.handleQuota))
	mux.HandleFunc("POST /v1/auth/openai/start", h.auth(h.handleStart))
	mux.HandleFunc("POST /v1/auth/openai/callback", h.auth(h.handleManualCallback))
	mux.HandleFunc("POST /v1/auth/openai/logout", h.auth(h.handleLogout))
}

// auth requires RoleAdmin for all OAuth endpoints.
// Breaking change from pre-#450: previously used requireAuth("", next) which
// allowed any authenticated user including operators. Now only admins can
// manage OAuth providers.
func (h *OAuthHandler) auth(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth(permissions.RoleAdmin, next)
}

func oauthTenantID(ctx context.Context) uuid.UUID {
	if tid := store.TenantIDFromContext(ctx); tid != uuid.Nil {
		return tid
	}
	return store.MasterTenantID
}

func oauthProviderName(r *http.Request) string {
	if provider := r.PathValue("provider"); provider != "" {
		return provider
	}
	return oauth.DefaultProviderName
}

func oauthFlowKey(ctx context.Context, providerName string) string {
	return oauthTenantID(ctx).String() + ":" + store.UserIDFromContext(ctx) + ":" + providerName
}

func (h *OAuthHandler) newTokenSource(ctx context.Context, providerName, displayName, apiBase string) *oauth.DBTokenSource {
	return oauth.NewDBTokenSource(h.provStore, h.secretStore, providerName).
		WithTenantID(oauthTenantID(ctx)).
		WithProviderMeta(displayName, apiBase)
}

func (h *OAuthHandler) ensureOAuthProviderName(ctx context.Context, providerName string) error {
	if h.provStore == nil {
		return nil
	}
	p, err := h.provStore.GetProviderByName(ctx, providerName)
	if err != nil {
		return nil
	}
	if p.ProviderType != store.ProviderChatGPTOAuth {
		return &oauth.ProviderTypeConflictError{
			ProviderName: providerName,
			ProviderType: p.ProviderType,
		}
	}
	return nil
}

func writeOAuthProviderConflict(w http.ResponseWriter, err error) bool {
	var conflict *oauth.ProviderTypeConflictError
	if !errors.As(err, &conflict) {
		return false
	}
	writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	return true
}

func (h *OAuthHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	providerName := oauthProviderName(r)
	if !isValidSlug(providerName) {
		locale := extractLocale(r)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidSlug, "provider")})
		return
	}
	if err := h.ensureOAuthProviderName(r.Context(), providerName); err != nil {
		writeOAuthProviderConflict(w, err)
		return
	}

	ts := h.newTokenSource(r.Context(), providerName, "", "")
	if !ts.Exists(r.Context()) {
		writeJSON(w, http.StatusOK, map[string]any{"authenticated": false})
		return
	}

	if _, err := ts.Token(); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"authenticated": false,
			"error":         "token invalid or expired",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"provider_name": providerName,
	})
}

func (h *OAuthHandler) handleStart(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	providerName := oauthProviderName(r)
	if !isValidSlug(providerName) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidSlug, "provider")})
		return
	}

	var body struct {
		DisplayName string `json:"display_name"`
		APIBase     string `json:"api_base"`
	}
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
			return
		}
	}
	if err := h.ensureOAuthProviderName(r.Context(), providerName); err != nil {
		writeOAuthProviderConflict(w, err)
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// Already authenticated?
	ts := h.newTokenSource(r.Context(), providerName, body.DisplayName, body.APIBase)
	if ts.Exists(r.Context()) {
		if _, err := ts.Token(); err == nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"status":        "already_authenticated",
				"provider_name": providerName,
			})
			return
		}
	}

	flowKey := oauthFlowKey(r.Context(), providerName)
	if pending := h.pending[flowKey]; pending != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"auth_url":      pending.login.AuthURL,
			"provider_name": providerName,
		})
		return
	}
	if h.activeFlowKey != "" && h.activeFlowKey != flowKey {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "another OAuth flow is already active on this server",
		})
		return
	}

	pending, err := oauth.StartLoginOpenAI()
	if err != nil {
		slog.Error("oauth.start", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": i18n.T(locale, i18n.MsgInternalError, "failed to start OAuth flow (is port 1455 available?)"),
		})
		return
	}

	waitCtx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	flow := &pendingOAuthFlow{
		login:        pending,
		cancel:       cancel,
		flowKey:      flowKey,
		tenantID:     oauthTenantID(r.Context()),
		userID:       store.UserIDFromContext(r.Context()),
		providerName: providerName,
		displayName:  body.DisplayName,
		apiBase:      body.APIBase,
	}
	h.pending[flowKey] = flow
	h.activeFlowKey = flowKey

	// Wait for callback in background, save token when done
	go h.waitForCallback(waitCtx, flow)

	emitAudit(h.msgBus, r, "oauth.login_started", "oauth", "openai")
	writeJSON(w, http.StatusOK, map[string]any{
		"auth_url":      pending.AuthURL,
		"provider_name": providerName,
	})
}

// waitForCallback waits for the OAuth callback and saves the token.
func (h *OAuthHandler) waitForCallback(ctx context.Context, flow *pendingOAuthFlow) {
	tokenResp, err := flow.login.Wait(ctx)

	h.mu.Lock()
	if h.pending[flow.flowKey] == flow {
		delete(h.pending, flow.flowKey)
	}
	if h.activeFlowKey == flow.flowKey {
		h.activeFlowKey = ""
	}
	h.mu.Unlock()

	if err != nil {
		if errors.Is(err, context.Canceled) {
			slog.Info("oauth flow canceled", "provider", flow.providerName)
			return
		}
		slog.Warn("oauth.callback failed", "error", err)
		return
	}

	saveCtx := store.WithTenantID(context.Background(), flow.tenantID)
	if _, err := h.saveAndRegister(saveCtx, flow.providerName, flow.displayName, flow.apiBase, tokenResp); err != nil {
		slog.Error("oauth.save_token", "error", err)
		return
	}

	slog.Info("oauth: OpenAI token saved via web UI callback", "provider", flow.providerName)
}

func (h *OAuthHandler) handleManualCallback(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	providerName := oauthProviderName(r)
	if !isValidSlug(providerName) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidSlug, "provider")})
		return
	}

	var body struct {
		RedirectURL string `json:"redirect_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.RedirectURL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "redirect_url")})
		return
	}
	if err := h.ensureOAuthProviderName(r.Context(), providerName); err != nil {
		writeOAuthProviderConflict(w, err)
		return
	}

	h.mu.Lock()
	pending := h.pending[oauthFlowKey(r.Context(), providerName)]
	h.mu.Unlock()

	if pending == nil || pending.providerName != providerName || pending.tenantID != oauthTenantID(r.Context()) || pending.userID != store.UserIDFromContext(r.Context()) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgNoPendingOAuth)})
		return
	}

	tokenResp, err := pending.login.ExchangeRedirectURL(body.RedirectURL)
	if err != nil {
		slog.Warn("oauth.manual_callback", "error", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	// Shut down the callback server and clear pending
	pending.cancel()
	pending.login.Shutdown()
	h.mu.Lock()
	if h.pending[pending.flowKey] == pending {
		delete(h.pending, pending.flowKey)
	}
	if h.activeFlowKey == pending.flowKey {
		h.activeFlowKey = ""
	}
	h.mu.Unlock()

	providerID, err := h.saveAndRegister(r.Context(), providerName, pending.displayName, pending.apiBase, tokenResp)
	if err != nil {
		if writeOAuthProviderConflict(w, err) {
			return
		}
		slog.Error("oauth.save_token", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToSaveToken)})
		return
	}

	slog.Info("oauth: OpenAI token saved via manual callback")
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"provider_name": providerName,
		"provider_id":   providerID.String(),
	})
}

func (h *OAuthHandler) handleLogout(w http.ResponseWriter, r *http.Request) {
	providerName := oauthProviderName(r)
	if !isValidSlug(providerName) {
		locale := extractLocale(r)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidSlug, "provider")})
		return
	}
	if err := h.ensureOAuthProviderName(r.Context(), providerName); err != nil {
		writeOAuthProviderConflict(w, err)
		return
	}

	ts := h.newTokenSource(r.Context(), providerName, "", "")
	if err := ts.Delete(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if h.providerReg != nil {
		tid := store.TenantIDFromContext(r.Context())
		if tid == uuid.Nil {
			tid = store.MasterTenantID
		}
		h.providerReg.UnregisterForTenant(tid, providerName)
	}

	emitAudit(h.msgBus, r, "oauth.logout", "oauth", "openai")
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged out"})
}

// saveAndRegister persists the OAuth result to DB and registers the CodexProvider in-memory.
func (h *OAuthHandler) saveAndRegister(ctx context.Context, providerName, displayName, apiBase string, tokenResp *oauth.OpenAITokenResponse) (uuid.UUID, error) {
	ts := h.newTokenSource(ctx, providerName, displayName, apiBase)
	providerID, err := ts.SaveOAuthResult(ctx, tokenResp)
	if err != nil {
		return uuid.Nil, err
	}

	// Register CodexProvider in-memory for immediate use
	if h.providerReg != nil {
		tid := oauthTenantID(ctx)
		providerAPIBase := apiBase
		codex := providers.NewCodexProvider(providerName, ts, providerAPIBase, "")
		if h.provStore != nil {
			providerCtx := store.WithTenantID(ctx, tid)
			if providerData, err := h.provStore.GetProviderByName(providerCtx, providerName); err == nil {
				if providerData.APIBase != "" {
					providerAPIBase = providerData.APIBase
					codex = providers.NewCodexProvider(providerName, ts, providerAPIBase, "")
				}
				if oauthSettings := store.ParseChatGPTOAuthProviderSettings(providerData.Settings); oauthSettings != nil {
					codex.WithRoutingDefaults(oauthSettings.CodexPool.Strategy, oauthSettings.CodexPool.ExtraProviderNames)
				}
			}
		}
		h.providerReg.RegisterForTenant(tid, codex)
	}

	return providerID, nil
}
