package http

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/oauth"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// OAuthHandler handles OAuth-related HTTP endpoints for web UI.
type OAuthHandler struct {
	token       string // gateway auth token
	provStore   store.ProviderStore
	secretStore store.ConfigSecretsStore
	providerReg *providers.Registry
	msgBus      *bus.MessageBus

	mu      sync.Mutex
	pending *oauth.PendingLogin // active OAuth flow (if any)
}

// NewOAuthHandler creates a handler for OAuth endpoints.
func NewOAuthHandler(token string, provStore store.ProviderStore, secretStore store.ConfigSecretsStore, providerReg *providers.Registry, msgBus *bus.MessageBus) *OAuthHandler {
	return &OAuthHandler{
		token:       token,
		provStore:   provStore,
		secretStore: secretStore,
		providerReg: providerReg,
		msgBus:      msgBus,
	}
}

// RegisterRoutes registers OAuth routes on the given mux.
func (h *OAuthHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/auth/openai/status", h.auth(h.handleStatus))
	mux.HandleFunc("POST /v1/auth/openai/start", h.auth(h.handleStart))
	mux.HandleFunc("POST /v1/auth/openai/callback", h.auth(h.handleManualCallback))
	mux.HandleFunc("POST /v1/auth/openai/logout", h.auth(h.handleLogout))
}

func (h *OAuthHandler) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !tokenMatch(extractBearerToken(r), h.token) {
			locale := extractLocale(r)
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": i18n.T(locale, i18n.MsgUnauthorized)})
			return
		}
		next(w, r)
	}
}

func (h *OAuthHandler) newTokenSource() *oauth.DBTokenSource {
	return oauth.NewDBTokenSource(h.provStore, h.secretStore, oauth.DefaultProviderName)
}

func (h *OAuthHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	ts := h.newTokenSource()
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
		"provider_name": oauth.DefaultProviderName,
	})
}

func (h *OAuthHandler) handleStart(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	h.mu.Lock()
	defer h.mu.Unlock()

	// Already authenticated?
	ts := h.newTokenSource()
	if ts.Exists(r.Context()) {
		if _, err := ts.Token(); err == nil {
			writeJSON(w, http.StatusOK, map[string]any{"status": "already_authenticated"})
			return
		}
	}

	// Shut down any previous pending flow to release port 1455
	if h.pending != nil {
		h.pending.Shutdown()
		h.pending = nil
	}

	pending, err := oauth.StartLoginOpenAI()
	if err != nil {
		slog.Error("oauth.start", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": i18n.T(locale, i18n.MsgInternalError, "failed to start OAuth flow (is port 1455 available?)"),
		})
		return
	}

	h.pending = pending

	// Wait for callback in background, save token when done
	go h.waitForCallback(pending)

	emitAudit(h.msgBus, r, "oauth.login_started", "oauth", "openai")
	writeJSON(w, http.StatusOK, map[string]any{"auth_url": pending.AuthURL})
}

// waitForCallback waits for the OAuth callback and saves the token.
func (h *OAuthHandler) waitForCallback(pending *oauth.PendingLogin) {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	tokenResp, err := pending.Wait(ctx)

	h.mu.Lock()
	if h.pending == pending {
		h.pending = nil
	}
	h.mu.Unlock()

	if err != nil {
		slog.Warn("oauth.callback failed", "error", err)
		return
	}

	if _, err := h.saveAndRegister(ctx, tokenResp); err != nil {
		slog.Error("oauth.save_token", "error", err)
		return
	}

	slog.Info("oauth: OpenAI token saved via web UI callback")
}

func (h *OAuthHandler) handleManualCallback(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	var body struct {
		RedirectURL string `json:"redirect_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.RedirectURL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "redirect_url")})
		return
	}

	h.mu.Lock()
	pending := h.pending
	h.mu.Unlock()

	if pending == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgNoPendingOAuth)})
		return
	}

	tokenResp, err := pending.ExchangeRedirectURL(body.RedirectURL)
	if err != nil {
		slog.Warn("oauth.manual_callback", "error", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	// Shut down the callback server and clear pending
	pending.Shutdown()
	h.mu.Lock()
	if h.pending == pending {
		h.pending = nil
	}
	h.mu.Unlock()

	providerID, err := h.saveAndRegister(r.Context(), tokenResp)
	if err != nil {
		slog.Error("oauth.save_token", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToSaveToken)})
		return
	}

	slog.Info("oauth: OpenAI token saved via manual callback")
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"provider_name": oauth.DefaultProviderName,
		"provider_id":   providerID.String(),
	})
}

func (h *OAuthHandler) handleLogout(w http.ResponseWriter, r *http.Request) {
	ts := h.newTokenSource()
	if err := ts.Delete(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if h.providerReg != nil {
		h.providerReg.Unregister(oauth.DefaultProviderName)
	}

	emitAudit(h.msgBus, r, "oauth.logout", "oauth", "openai")
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged out"})
}

// saveAndRegister persists the OAuth result to DB and registers the CodexProvider in-memory.
func (h *OAuthHandler) saveAndRegister(ctx context.Context, tokenResp *oauth.OpenAITokenResponse) (uuid.UUID, error) {
	ts := h.newTokenSource()
	providerID, err := ts.SaveOAuthResult(ctx, tokenResp)
	if err != nil {
		return uuid.Nil, err
	}

	// Register CodexProvider in-memory for immediate use
	if h.providerReg != nil {
		codex := providers.NewCodexProvider(oauth.DefaultProviderName, ts, "", "")
		h.providerReg.Register(codex)
	}

	return providerID, nil
}
