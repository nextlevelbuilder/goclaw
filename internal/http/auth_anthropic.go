package http

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const anthropicOAuthProviderName = "anthropic-oauth"

// AnthropicAuthHandler handles Anthropic setup token endpoints.
type AnthropicAuthHandler struct {
	token       string
	provStore   store.ProviderStore
	providerReg *providers.Registry
	msgBus      *bus.MessageBus
}

// NewAnthropicAuthHandler creates a handler for Anthropic auth endpoints.
func NewAnthropicAuthHandler(token string, provStore store.ProviderStore, providerReg *providers.Registry, msgBus *bus.MessageBus) *AnthropicAuthHandler {
	return &AnthropicAuthHandler{
		token:       token,
		provStore:   provStore,
		providerReg: providerReg,
		msgBus:      msgBus,
	}
}

// RegisterRoutes registers Anthropic auth routes on the given mux.
func (h *AnthropicAuthHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/auth/anthropic/token", h.auth(h.handleToken))
	mux.HandleFunc("GET /v1/auth/anthropic/status", h.auth(h.handleStatus))
	mux.HandleFunc("POST /v1/auth/anthropic/logout", h.auth(h.handleLogout))
}

func (h *AnthropicAuthHandler) auth(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth("", next)
}

// handleToken accepts and stores an Anthropic setup token.
func (h *AnthropicAuthHandler) handleToken(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if err := providers.ValidateAnthropicCredential(body.Token); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	settings := providers.SettingsForCredential(body.Token)
	settingsJSON, _ := json.Marshal(settings)

	ctx := r.Context()

	// Check if provider already exists — update it
	existing, err := h.provStore.GetProviderByName(ctx, anthropicOAuthProviderName)
	if err == nil {
		if err := h.provStore.UpdateProvider(ctx, existing.ID, map[string]any{
			"api_key":  body.Token,
			"settings": json.RawMessage(settingsJSON),
			"enabled":  true,
		}); err != nil {
			slog.Error("anthropic.auth: update provider", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update provider"})
			return
		}
	} else {
		// Create new provider
		p := &store.LLMProviderData{
			Name:         anthropicOAuthProviderName,
			DisplayName:  "Anthropic (OAuth Token)",
			ProviderType: store.ProviderAnthropicOAuth,
			APIBase:      "",
			APIKey:       body.Token,
			Enabled:      true,
			Settings:     settingsJSON,
		}
		if err := h.provStore.CreateProvider(ctx, p); err != nil {
			slog.Error("anthropic.auth: create provider", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create provider"})
			return
		}
	}

	// Register provider in-memory for immediate use
	if h.providerReg != nil {
		h.providerReg.Register(providers.NewAnthropicProvider(body.Token))
	}

	emitAudit(h.msgBus, r, "anthropic.auth.token_stored", "auth", "anthropic")
	writeJSON(w, http.StatusOK, map[string]any{
		"status":        "stored",
		"provider_name": anthropicOAuthProviderName,
		"token_type":    settings.TokenType,
	})
}

// handleStatus returns Anthropic auth status.
func (h *AnthropicAuthHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	p, err := h.provStore.GetProviderByName(r.Context(), anthropicOAuthProviderName)
	if err != nil || p.APIKey == "" {
		writeJSON(w, http.StatusOK, map[string]any{"authenticated": false})
		return
	}

	resp := map[string]any{
		"authenticated": true,
		"provider_name": anthropicOAuthProviderName,
	}

	var settings providers.AnthropicTokenSettings
	if len(p.Settings) > 0 {
		_ = json.Unmarshal(p.Settings, &settings)
	}
	resp["token_type"] = settings.TokenType
	if settings.ExpiresAt > 0 {
		expiresAt := time.Unix(settings.ExpiresAt, 0)
		resp["expires_at"] = expiresAt.Format("2006-01-02")
		resp["days_remaining"] = settings.DaysUntilExpiry()
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleLogout removes Anthropic credentials.
func (h *AnthropicAuthHandler) handleLogout(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, err := h.provStore.GetProviderByName(ctx, anthropicOAuthProviderName)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]string{"status": "no credentials found"})
		return
	}

	if err := h.provStore.DeleteProvider(ctx, p.ID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to delete: %v", err)})
		return
	}

	if h.providerReg != nil {
		h.providerReg.Unregister(anthropicOAuthProviderName)
	}

	emitAudit(h.msgBus, r, "anthropic.auth.logout", "auth", "anthropic")
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged out"})
}
