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
	"github.com/nextlevelbuilder/goclaw/internal/audio/minimax"
	"github.com/nextlevelbuilder/goclaw/internal/audio/openai"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// testConnectionRequest is the JSON body for POST /v1/tts/test-connection.
type testConnectionRequest struct {
	Provider  string `json:"provider"`
	APIKey    string `json:"api_key,omitempty"`
	APIBase   string `json:"api_base,omitempty"`
	BaseURL   string `json:"base_url,omitempty"`
	VoiceID   string `json:"voice_id,omitempty"`
	ModelID   string `json:"model_id,omitempty"`
	GroupID   string `json:"group_id,omitempty"` // MiniMax requires group_id
	Rate      string `json:"rate,omitempty"`
	TimeoutMs int    `json:"timeout_ms,omitempty"`
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
}

// providersRequiringAPIKey lists providers that need an API key.
var providersRequiringAPIKey = map[string]bool{
	"openai":     true,
	"elevenlabs": true,
	"minimax":    true,
}

const testConnectionTimeout = 10 * time.Second

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

	// Synthesize short test text.
	synthCtx, cancel := context.WithTimeout(ctx, testConnectionTimeout)
	defer cancel()

	start := time.Now()
	_, err = provider.Synthesize(synthCtx, "test", audio.TTSOptions{Voice: req.VoiceID, Model: req.ModelID})
	dur := time.Since(start)

	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			slog.Warn("tts.test-connection.timeout", "provider", req.Provider, "ms", dur.Milliseconds())
			writeJSON(w, http.StatusGatewayTimeout, testConnectionResponse{
				Success: false, Error: "test timeout",
			})
			return
		}
		slog.Warn("tts.test-connection.failed", "provider", req.Provider, "error", err)
		writeJSON(w, http.StatusBadGateway, testConnectionResponse{
			Success: false, Error: "upstream synthesis failed",
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
	default:
		return nil, fmt.Errorf("unsupported provider: %s", req.Provider)
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
