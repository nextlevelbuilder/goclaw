package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/audio/edge"
	"github.com/nextlevelbuilder/goclaw/internal/audio/elevenlabs"
	"github.com/nextlevelbuilder/goclaw/internal/audio/gemini"
	"github.com/nextlevelbuilder/goclaw/internal/audio/minimax"
	"github.com/nextlevelbuilder/goclaw/internal/audio/openai"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// testConnectionRequest is the JSON body for POST /v1/tts/test-connection.
type testConnectionRequest struct {
	Provider  string         `json:"provider"`
	APIKey    string         `json:"api_key,omitempty"`
	APIBase   string         `json:"api_base,omitempty"`
	BaseURL   string         `json:"base_url,omitempty"`
	VoiceID   string         `json:"voice_id,omitempty"`
	ModelID   string         `json:"model_id,omitempty"`
	GroupID   string         `json:"group_id,omitempty"` // MiniMax requires group_id
	Rate      string         `json:"rate,omitempty"`
	TimeoutMs int            `json:"timeout_ms,omitempty"`
	Params    map[string]any `json:"params,omitempty"` // provider-specific params blob
}

// testConnectionResponse is the JSON response for POST /v1/tts/test-connection.
type testConnectionResponse struct {
	Success   bool   `json:"success"`
	Provider  string `json:"provider,omitempty"`
	LatencyMs int64  `json:"latency_ms,omitempty"`
	Error     string `json:"error,omitempty"`
}

// supportedTestProviders lists providers that support ephemeral test-connection.
var supportedTestProviders = map[string]bool{
	"openai":     true,
	"elevenlabs": true,
	"edge":       true,
	"minimax":    true,
	"gemini":     true,
}

// providersRequiringAPIKey lists providers that need an API key.
var providersRequiringAPIKey = map[string]bool{
	"openai":     true,
	"elevenlabs": true,
	"minimax":    true,
	"gemini":     true,
}

const defaultTestConnectionTimeoutMs = 120000 // 120s default; req.TimeoutMs > tenant > default

// handleTestConnection serves POST /v1/tts/test-connection.
// Creates an ephemeral provider from request credentials and tests synthesis.
func (h *TTSHandler) handleTestConnection(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)

	// Rate limit (same as synthesize).
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

	r.Body = http.MaxBytesReader(w, r.Body, maxSynthesizeBodyBytes)

	var req testConnectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"invalid json: %s"}`, err.Error()), http.StatusBadRequest)
		return
	}

	req.Provider = strings.TrimSpace(req.Provider)
	if req.Provider == "" {
		http.Error(w, `{"error":"provider is required"}`, http.StatusBadRequest)
		return
	}

	if !supportedTestProviders[req.Provider] {
		http.Error(w, fmt.Sprintf(`{"error":"unsupported provider: %s"}`, req.Provider), http.StatusBadRequest)
		return
	}

	// Fall back to saved secrets when the request is testing a previously saved key.
	// Frontend masks stored API keys as "***" — without this fallback, retesting
	// an existing config would force the user to retype the key.
	h.fillMissingTestSecrets(ctx, &req)

	if providersRequiringAPIKey[req.Provider] && (req.APIKey == "" || req.APIKey == "***") {
		http.Error(w, fmt.Sprintf(`{"error":"api_key is required for %s"}`, req.Provider), http.StatusBadRequest)
		return
	}

	if req.APIBase == "" {
		req.APIBase = req.BaseURL
	}

	if err := validateProviderURL(req.APIBase, req.Provider); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadRequest)
		return
	}

	// Create ephemeral provider.
	provider, err := createEphemeralTTSProvider(req)
	if err != nil {
		slog.Warn("tts.test-connection.provider-create-failed", "provider", req.Provider, "error", err)
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadRequest)
		return
	}

	// Synthesize short test text — req.TimeoutMs overrides tenant which overrides default 120s.
	effectiveMs := req.TimeoutMs
	if effectiveMs <= 0 {
		effectiveMs = loadTenantTTSTimeoutMs(ctx, h.systemConfigs)
	}
	if effectiveMs <= 0 {
		effectiveMs = defaultTestConnectionTimeoutMs
	}
	synthCtx, cancel := context.WithTimeout(ctx, time.Duration(effectiveMs)*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err = provider.Synthesize(synthCtx, "test", audio.TTSOptions{Voice: req.VoiceID, Model: req.ModelID, Params: req.Params})
	dur := time.Since(start)

	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			slog.Warn("tts.test-connection.timeout", "provider", req.Provider, "ms", dur.Milliseconds())
			writeJSON(w, http.StatusGatewayTimeout, testConnectionResponse{
				Success: false, Error: "test timeout",
			})
			return
		}
		// Translate Gemini sentinel errors to 422 Unprocessable Entity with i18n message.
		if errors.Is(err, gemini.ErrInvalidVoice) {
			slog.Warn("tts.test-connection.invalid-params", "provider", req.Provider, "error", err)
			writeJSON(w, http.StatusUnprocessableEntity, testConnectionResponse{
				Success: false, Error: i18n.T(locale, i18n.MsgTtsGeminiInvalidVoice, req.VoiceID),
			})
			return
		}
		if errors.Is(err, gemini.ErrSpeakerLimit) {
			slog.Warn("tts.test-connection.invalid-params", "provider", req.Provider, "error", err)
			writeJSON(w, http.StatusUnprocessableEntity, testConnectionResponse{
				Success: false, Error: i18n.T(locale, i18n.MsgTtsGeminiSpeakerLimit),
			})
			return
		}
		if errors.Is(err, gemini.ErrInvalidModel) {
			slog.Warn("tts.test-connection.invalid-params", "provider", req.Provider, "error", err)
			writeJSON(w, http.StatusUnprocessableEntity, testConnectionResponse{
				Success: false, Error: i18n.T(locale, i18n.MsgTtsGeminiInvalidModel, req.ModelID),
			})
			return
		}
		if errors.Is(err, gemini.ErrTextOnlyResponse) {
			slog.Warn("tts.test-connection.text-only", "provider", req.Provider, "error", err)
			writeJSON(w, http.StatusUnprocessableEntity, testConnectionResponse{
				Success: false, Error: i18n.T(locale, i18n.MsgTtsGeminiTextOnly),
			})
			return
		}
		// Surface upstream error to caller — test-connection is a diagnostic
		// endpoint, opacity here just makes debugging harder.
		slog.Warn("tts.test-connection.failed", "provider", req.Provider, "error", err)
		writeJSON(w, http.StatusBadGateway, testConnectionResponse{
			Success: false, Error: err.Error(),
		})
		return
	}

	slog.Info("tts.test-connection.ok", "provider", req.Provider, "ms", dur.Milliseconds())
	writeJSON(w, http.StatusOK, testConnectionResponse{
		Success:   true,
		Provider:  req.Provider,
		LatencyMs: dur.Milliseconds(),
	})
}

// createEphemeralTTSProvider creates a TTS provider from request credentials.
// The provider is ephemeral — not registered in the manager.
func createEphemeralTTSProvider(req testConnectionRequest) (audio.TTSProvider, error) {
	switch req.Provider {
	case "openai":
		return openai.NewProvider(openai.Config{
			APIKey:    req.APIKey,
			APIBase:   req.APIBase,
			Model:     req.ModelID,
			Voice:     req.VoiceID,
			TimeoutMs: req.TimeoutMs,
		}), nil
	case "elevenlabs":
		return elevenlabs.NewTTSProvider(elevenlabs.Config{
			APIKey:    req.APIKey,
			BaseURL:   req.APIBase,
			VoiceID:   req.VoiceID,
			ModelID:   req.ModelID,
			TimeoutMs: req.TimeoutMs,
		}), nil
	case "edge":
		return edge.NewProvider(edge.Config{
			Voice:     req.VoiceID,
			Rate:      req.Rate,
			TimeoutMs: req.TimeoutMs,
		}), nil
	case "minimax":
		return minimax.NewProvider(minimax.Config{
			APIKey:    req.APIKey,
			APIBase:   req.APIBase,
			GroupID:   req.GroupID,
			VoiceID:   req.VoiceID,
			Model:     req.ModelID,
			TimeoutMs: req.TimeoutMs,
		}), nil
	case "gemini":
		return gemini.NewProvider(gemini.Config{
			APIKey:    req.APIKey,
			APIBase:   req.APIBase,
			Voice:     req.VoiceID,
			Model:     req.ModelID,
			TimeoutMs: req.TimeoutMs,
		}), nil
	default:
		return nil, fmt.Errorf("unsupported provider: %s", req.Provider)
	}
}

// fillMissingTestSecrets fills empty/masked secret fields on req with values
// from the tenant's saved config. Lets the frontend re-test a stored API key
// without forcing the user to retype it (the key is masked as "***" in GET).
func (h *TTSHandler) fillMissingTestSecrets(ctx context.Context, req *testConnectionRequest) {
	if h.configSecrets == nil {
		return
	}
	if providersRequiringAPIKey[req.Provider] && (req.APIKey == "" || req.APIKey == "***") {
		if saved, _ := h.configSecrets.Get(ctx, "tts."+req.Provider+".api_key"); saved != "" {
			req.APIKey = saved
		}
	}
	if req.Provider == "minimax" && req.GroupID == "" {
		if saved, _ := h.configSecrets.Get(ctx, "tts.minimax.group_id"); saved != "" {
			req.GroupID = saved
		}
	}
}

func loadTenantTTSTimeoutMs(ctx context.Context, sc store.SystemConfigStore) int {
	if sc == nil {
		return 0
	}
	raw, err := sc.Get(ctx, "tts.timeout_ms")
	if err != nil || raw == "" {
		return 0
	}
	timeoutMs, err := strconv.Atoi(raw)
	if err != nil || timeoutMs <= 0 {
		return 0
	}
	return timeoutMs
}
