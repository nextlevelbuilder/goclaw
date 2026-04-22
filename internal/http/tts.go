package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/audio/elevenlabs"
	"github.com/nextlevelbuilder/goclaw/internal/audio/gemini"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// TTSHandler handles POST /v1/tts/synthesize — converts text to audio via a
// configured TTS provider and returns raw audio bytes with the appropriate MIME type.
type TTSHandler struct {
	mu            sync.RWMutex
	manager       *audio.Manager
	rateLimiter   func(string) bool        // per-IP/token rate limit check (nil = no limit)
	systemConfigs store.SystemConfigStore  // per-tenant TTS settings
	configSecrets store.ConfigSecretsStore // per-tenant TTS secrets
}

// NewTTSHandler creates a TTSHandler backed by the given audio.Manager.
func NewTTSHandler(mgr *audio.Manager) *TTSHandler {
	return &TTSHandler{manager: mgr}
}

// SetRateLimiter injects the rate limiter function (reused from the server's global limiter).
func (h *TTSHandler) SetRateLimiter(fn func(string) bool) { h.rateLimiter = fn }

// SetStores injects stores for per-tenant TTS config lookup.
func (h *TTSHandler) SetStores(sc store.SystemConfigStore, cs store.ConfigSecretsStore) {
	h.systemConfigs = sc
	h.configSecrets = cs
}

// UpdateManager swaps the underlying manager (hot-reload safe).
func (h *TTSHandler) UpdateManager(mgr *audio.Manager) {
	if mgr == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.manager = mgr
}

// RegisterRoutes wires TTS endpoints onto mux with RoleOperator auth.
func (h *TTSHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/tts/synthesize",
		requireAuth(permissions.RoleOperator, h.handleSynthesize))
	mux.HandleFunc("POST /v1/tts/test-connection",
		requireAuth(permissions.RoleOperator, h.handleTestConnection))
	h.registerCapabilitiesRoute(mux)
}

// synthesizeRequest is the JSON body for POST /v1/tts/synthesize.
type synthesizeRequest struct {
	Text     string `json:"text"`
	Provider string `json:"provider,omitempty"`
	VoiceID  string `json:"voice_id,omitempty"`
	ModelID  string `json:"model_id,omitempty"`
}

const (
	maxSynthesizeBodyBytes = 4 << 10 // 4KB — enough for 500 chars + metadata
	maxSynthesizeTextChars = 500
	synthesizeTimeout      = 15 * time.Second
)

// handleSynthesize serves POST /v1/tts/synthesize.
func (h *TTSHandler) handleSynthesize(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)

	// Rate limit (best-effort — reuses per-IP/token limiter; no per-user bucket).
	if h.rateLimiter != nil {
		key := r.RemoteAddr
		if tok := extractBearerToken(r); tok != "" {
			key = "token:" + tok
		}
		if !h.rateLimiter(key) {
			w.Header().Set("Retry-After", "60")
			http.Error(w, fmt.Sprintf(`{"error":%q}`, i18n.T(locale, i18n.MsgRateLimitExceeded)), http.StatusTooManyRequests)
			return
		}
	}

	// Cap request body to prevent DoS via oversized JSON.
	r.Body = http.MaxBytesReader(w, r.Body, maxSynthesizeBodyBytes)

	var req synthesizeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"invalid json: %s"}`, err.Error()), http.StatusBadRequest)
		return
	}

	req.Text = strings.TrimSpace(req.Text)
	if req.Text == "" {
		http.Error(w, `{"error":"text is required"}`, http.StatusBadRequest)
		return
	}
	if len([]rune(req.Text)) > maxSynthesizeTextChars {
		http.Error(w, fmt.Sprintf(`{"error":"text exceeds %d chars"}`, maxSynthesizeTextChars), http.StatusBadRequest)
		return
	}

	// Try per-tenant TTS config first, fall back to global manager.
	p, name, tenantParams, err := h.resolveTenantProvider(ctx, req.Provider)
	if err != nil {
		// Tenant config lookup failed, try global manager
		h.mu.RLock()
		mgr := h.manager
		h.mu.RUnlock()

		name = req.Provider
		if name == "" {
			name = mgr.PrimaryProvider()
		}
		if name == "" {
			http.Error(w, `{"error":"no tts provider configured"}`, http.StatusNotFound)
			return
		}
		var ok bool
		p, ok = mgr.GetProvider(name)
		if !ok {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, "provider not found: "+name), http.StatusNotFound)
			return
		}
	}

	// ElevenLabs model validation — rejects unknown model IDs with allowlist error.
	if name == "elevenlabs" && req.ModelID != "" {
		if err := elevenlabs.ValidateModel(req.ModelID); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusUnprocessableEntity)
			return
		}
	}

	// Validate tenant params against the provider's capability schema (Finding #3).
	// Rejects unknown keys and out-of-range values before forwarding to the provider.
	if len(tenantParams) > 0 {
		if dp, ok := p.(audio.DescribableProvider); ok {
			caps := dp.Capabilities()
			if err := audio.ValidateParams(caps.Params, tenantParams); err != nil {
				slog.Warn("tts.synthesize.invalid-params", "provider", name, "error", err)
				var ukErr audio.ErrTTSParamUnknownKey
				var orErr audio.ErrTTSParamOutOfRange
				switch {
				case errors.As(err, &ukErr):
					msg := i18n.T(locale, i18n.MsgTtsParamUnknownKey, ukErr.Key)
					http.Error(w, fmt.Sprintf(`{"error":%q}`, msg), http.StatusUnprocessableEntity)
				case errors.As(err, &orErr):
					msg := i18n.T(locale, i18n.MsgTtsParamOutOfRange, orErr.Key, orErr.Val, orErr.Min, orErr.Max)
					http.Error(w, fmt.Sprintf(`{"error":%q}`, msg), http.StatusUnprocessableEntity)
				default:
					http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusUnprocessableEntity)
				}
				return
			}
		}
	}

	// Synthesize with a 15-second deadline.
	synthCtx, cancel := context.WithTimeout(ctx, synthesizeTimeout)
	defer cancel()

	opts := audio.TTSOptions{Voice: req.VoiceID, Model: req.ModelID, Params: tenantParams}
	// Restore saved multi-speaker defaults (Gemini) so persisted config reactivates
	// at runtime instead of silently falling through to single-voice mode.
	if speakers := loadSavedSpeakers(ctx, h.systemConfigs, name); len(speakers) > 0 {
		opts.Speakers = speakers
	}
	start := time.Now()
	result, err := p.Synthesize(synthCtx, req.Text, opts)
	dur := time.Since(start)

	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			slog.Warn("tts.synthesize.timeout", "provider", name, "ms", dur.Milliseconds())
			http.Error(w, `{"error":"synthesis timeout"}`, http.StatusGatewayTimeout)
			return
		}
		// Translate Gemini sentinel errors to 422 Unprocessable Entity with i18n message.
		if errors.Is(err, gemini.ErrInvalidVoice) {
			slog.Warn("tts.synthesize.invalid-params", "provider", name, "error", err)
			msg := i18n.T(locale, i18n.MsgTtsGeminiInvalidVoice, req.VoiceID)
			http.Error(w, fmt.Sprintf(`{"error":%q}`, msg), http.StatusUnprocessableEntity)
			return
		}
		if errors.Is(err, gemini.ErrSpeakerLimit) {
			slog.Warn("tts.synthesize.invalid-params", "provider", name, "error", err)
			msg := i18n.T(locale, i18n.MsgTtsGeminiSpeakerLimit)
			http.Error(w, fmt.Sprintf(`{"error":%q}`, msg), http.StatusUnprocessableEntity)
			return
		}
		if errors.Is(err, gemini.ErrInvalidModel) {
			slog.Warn("tts.synthesize.invalid-params", "provider", name, "error", err)
			msg := i18n.T(locale, i18n.MsgTtsGeminiInvalidModel, req.ModelID)
			http.Error(w, fmt.Sprintf(`{"error":%q}`, msg), http.StatusUnprocessableEntity)
			return
		}
		// Surface upstream error to caller — opaque "upstream synthesis failed"
		// makes the test playground useless for debugging provider config.
		slog.Warn("tts.synthesize.failed", "provider", name, "error", err)
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadGateway)
		return
	}

	slog.Info("tts.synthesize.ok", "provider", name, "bytes", len(result.Audio), "ms", dur.Milliseconds())
	w.Header().Set("Content-Type", result.MimeType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(result.Audio)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(result.Audio)
}

// resolveTenantProvider attempts to create a TTS provider from tenant-specific config.
// Performs dual-read: reads legacy flat keys AND tts.{p}.params JSON blob.
// Blob wins on key conflict; legacy flat keys fill gaps.
// Returns (nil, "", nil, error) if tenant has no TTS config — caller should fall back to global manager.
func (h *TTSHandler) resolveTenantProvider(ctx context.Context, explicitProvider string) (audio.TTSProvider, string, map[string]any, error) {
	if h.systemConfigs == nil || h.configSecrets == nil {
		return nil, "", nil, fmt.Errorf("stores not configured")
	}

	// Get tenant's configured provider
	providerName := explicitProvider
	if providerName == "" {
		var err error
		providerName, err = h.systemConfigs.Get(ctx, "tts.provider")
		if err != nil || providerName == "" {
			return nil, "", nil, fmt.Errorf("no tenant tts provider")
		}
	}

	// Build ephemeral provider from tenant config
	req := testConnectionRequest{Provider: providerName, TimeoutMs: loadTenantTTSTimeoutMs(ctx, h.systemConfigs)}

	switch providerName {
	case "openai":
		if key, _ := h.configSecrets.Get(ctx, "tts.openai.api_key"); key != "" {
			req.APIKey = key
		} else {
			return nil, "", nil, fmt.Errorf("no api key")
		}
		req.APIBase, _ = h.systemConfigs.Get(ctx, "tts.openai.api_base")
		req.VoiceID, _ = h.systemConfigs.Get(ctx, "tts.openai.voice")
		req.ModelID, _ = h.systemConfigs.Get(ctx, "tts.openai.model")
		req.Params = loadParamsBlob(ctx, h.systemConfigs, "tts.openai.params")

	case "elevenlabs":
		if key, _ := h.configSecrets.Get(ctx, "tts.elevenlabs.api_key"); key != "" {
			req.APIKey = key
		} else {
			return nil, "", nil, fmt.Errorf("no api key")
		}
		req.APIBase, _ = h.systemConfigs.Get(ctx, "tts.elevenlabs.api_base")
		req.VoiceID, _ = h.systemConfigs.Get(ctx, "tts.elevenlabs.voice")
		req.ModelID, _ = h.systemConfigs.Get(ctx, "tts.elevenlabs.model")
		req.Params = loadParamsBlob(ctx, h.systemConfigs, "tts.elevenlabs.params")

	case "minimax":
		if key, _ := h.configSecrets.Get(ctx, "tts.minimax.api_key"); key != "" {
			req.APIKey = key
		} else {
			return nil, "", nil, fmt.Errorf("no api key")
		}
		req.GroupID, _ = h.configSecrets.Get(ctx, "tts.minimax.group_id")
		req.APIBase, _ = h.systemConfigs.Get(ctx, "tts.minimax.api_base")
		req.VoiceID, _ = h.systemConfigs.Get(ctx, "tts.minimax.voice")
		req.ModelID, _ = h.systemConfigs.Get(ctx, "tts.minimax.model")
		req.Params = loadParamsBlob(ctx, h.systemConfigs, "tts.minimax.params")

	case "edge":
		req.VoiceID, _ = h.systemConfigs.Get(ctx, "tts.edge.voice")
		req.Rate, _ = h.systemConfigs.Get(ctx, "tts.edge.rate")
		req.Params = loadParamsBlob(ctx, h.systemConfigs, "tts.edge.params")

	case "gemini":
		if key, _ := h.configSecrets.Get(ctx, "tts.gemini.api_key"); key != "" {
			req.APIKey = key
		} else {
			return nil, "", nil, fmt.Errorf("no api key")
		}
		req.APIBase, _ = h.systemConfigs.Get(ctx, "tts.gemini.api_base")
		req.VoiceID, _ = h.systemConfigs.Get(ctx, "tts.gemini.voice")
		req.ModelID, _ = h.systemConfigs.Get(ctx, "tts.gemini.model")
		req.Params = loadParamsBlob(ctx, h.systemConfigs, "tts.gemini.params")

	default:
		return nil, "", nil, fmt.Errorf("unsupported provider: %s", providerName)
	}

	provider, err := createEphemeralTTSProvider(req)
	if err != nil {
		return nil, "", nil, err
	}

	slog.Debug("tts: using tenant provider", "provider", providerName, "tenant", store.TenantIDFromContext(ctx))
	return provider, providerName, req.Params, nil
}

// paramsMaxBytes is the maximum byte length accepted for a stored params blob.
// Finding #6: cap at 16 KB to prevent oversized JSON DoS.
const paramsMaxBytes = 16 * 1024

// loadParamsBlob reads a JSON blob from system_configs and returns it as map[string]any.
// Returns nil when the key is absent, the value exceeds 16 KB, or the value is not valid JSON.
// Never logs the raw value — logs length only on parse failure (Finding #6).
func loadParamsBlob(ctx context.Context, sc store.SystemConfigStore, key string) map[string]any {
	raw, err := sc.Get(ctx, key)
	if err != nil || raw == "" {
		return nil
	}
	if len(raw) > paramsMaxBytes {
		slog.Warn("tts: params blob exceeds 16 KB limit, ignoring", "key", key, "length", len(raw))
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		slog.Warn("tts: invalid params blob", "key", key, "length", len(raw))
		return nil
	}
	return m
}

// loadSavedSpeakers reads the per-tenant multi-speaker JSON blob for providers
// that persist speaker configuration (currently only Gemini). Returns nil when
// no config exists or decode fails — caller falls back to single-voice mode.
func loadSavedSpeakers(ctx context.Context, sc store.SystemConfigStore, providerName string) []audio.SpeakerVoice {
	if sc == nil || providerName != "gemini" {
		return nil
	}
	raw, err := sc.Get(ctx, "tts.gemini.speakers")
	if err != nil || raw == "" {
		return nil
	}
	var speakers []audio.SpeakerVoice
	if err := json.Unmarshal([]byte(raw), &speakers); err != nil {
		return nil
	}
	return speakers
}
