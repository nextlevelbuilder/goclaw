package http

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/audio/elevenlabs"
	"github.com/nextlevelbuilder/goclaw/internal/audio/minimax"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// VoicesHandler serves GET /v1/voices and POST /v1/voices/refresh.
// provider is typed as audio.VoiceListProvider so any provider can be injected.
type VoicesHandler struct {
	cache       *audio.VoiceCache
	provider    audio.VoiceListProvider // nil when resolved at request time from stores
	secretStore store.ConfigSecretsStore
	tenantStore store.TenantStore
}

// NewVoicesHandler creates a handler that resolves the provider at request time
// from config_secrets. Use NewVoicesHandlerWithProvider for tests.
func NewVoicesHandler(cache *audio.VoiceCache, secretStore store.ConfigSecretsStore, tenantStore store.TenantStore) *VoicesHandler {
	return &VoicesHandler{cache: cache, secretStore: secretStore, tenantStore: tenantStore}
}

// NewVoicesHandlerWithProvider creates a handler with a pre-built provider.
// Accepts audio.VoiceListProvider so any provider (ElevenLabs, MiniMax, mock) can be injected.
// Primarily used in tests.
func NewVoicesHandlerWithProvider(cache *audio.VoiceCache, p audio.VoiceListProvider) *VoicesHandler {
	return &VoicesHandler{cache: cache, provider: p}
}

// RegisterRoutes wires the voices endpoints onto mux.
func (h *VoicesHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/voices", requireAuth("", h.handleList))
	mux.HandleFunc("POST /v1/voices/refresh", requireAuth(permissions.RoleAdmin, h.handleRefresh))
}

// handleList serves GET /v1/voices — returns cached list or fetches live.
func (h *VoicesHandler) handleList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID := store.TenantIDFromContext(ctx)
	locale := store.LocaleFromContext(ctx)

	if voices, ok := h.cache.Get(tenantID); ok {
		writeJSON(w, http.StatusOK, map[string]any{"voices": voices})
		return
	}

	p, err := h.resolveProvider(r, tenantID)
	if err != nil {
		slog.Warn("voices: no provider configured", "tenant_id", tenantID, "error", err)
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error": i18n.T(locale, i18n.MsgVoicesListFailed, err.Error()),
		})
		return
	}

	voices, err := p.ListVoices(ctx)
	if err != nil {
		slog.Warn("voices: list failed", "tenant_id", tenantID, "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error": i18n.T(locale, i18n.MsgTtsMiniMaxVoicesFailed, err.Error()),
		})
		return
	}

	h.cache.Set(tenantID, voices)
	writeJSON(w, http.StatusOK, map[string]any{"voices": voices})
}

// handleRefresh serves POST /v1/voices/refresh — admin-only, forces a live
// refetch by invalidating the tenant's cache entry.
func (h *VoicesHandler) handleRefresh(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID := store.TenantIDFromContext(ctx)
	locale := store.LocaleFromContext(ctx)

	h.cache.Invalidate(tenantID)

	p, err := h.resolveProvider(r, tenantID)
	if err != nil {
		slog.Warn("voices: no provider on refresh", "tenant_id", tenantID, "error", err)
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error": i18n.T(locale, i18n.MsgVoicesListFailed, err.Error()),
		})
		return
	}

	voices, err := p.ListVoices(ctx)
	if err != nil {
		slog.Warn("voices: refresh fetch failed", "tenant_id", tenantID, "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error": i18n.T(locale, i18n.MsgVoicesListFailed, err.Error()),
		})
		return
	}

	h.cache.Set(tenantID, voices)
	writeJSON(w, http.StatusOK, map[string]any{"voices": voices})
}

// resolveProvider returns the VoiceListProvider for this request.
// Priority: injected provider (test/pre-built) > query-param ?provider > secret store lookup (elevenlabs default).
func (h *VoicesHandler) resolveProvider(r *http.Request, tenantID uuid.UUID) (audio.VoiceListProvider, error) {
	if h.provider != nil {
		return h.provider, nil
	}
	if h.secretStore == nil {
		return nil, fmt.Errorf("no voice provider configured")
	}

	providerName := r.URL.Query().Get("provider")
	if providerName == "" {
		providerName = "elevenlabs" // backward-compatible default
	}

	switch providerName {
	case "minimax":
		apiKey, err := h.secretStore.Get(r.Context(), "tts.minimax.api_key")
		if err != nil || apiKey == "" {
			return nil, fmt.Errorf("MiniMax API key not found for tenant %s", tenantID)
		}
		apiBase, _ := h.secretStore.Get(r.Context(), "tts.minimax.api_base")
		return minimax.NewVoiceLister(apiKey, apiBase, 15000, tenantID), nil

	case "elevenlabs":
		apiKey, err := h.secretStore.Get(r.Context(), "tts.elevenlabs.api_key")
		if err != nil || apiKey == "" {
			return nil, fmt.Errorf("ElevenLabs API key not found for tenant %s", tenantID)
		}
		return elevenlabs.NewTTSProvider(elevenlabs.Config{APIKey: apiKey}), nil

	default:
		return nil, fmt.Errorf("unsupported voice provider: %s", providerName)
	}
}
