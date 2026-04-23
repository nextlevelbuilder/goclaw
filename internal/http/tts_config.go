package http

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// TTSConfigHandler handles per-tenant TTS configuration.
// Unlike config.patch (master-scope), this allows tenant admins to configure TTS.
type TTSConfigHandler struct {
	systemConfigs store.SystemConfigStore
	configSecrets store.ConfigSecretsStore
}

// NewTTSConfigHandler creates a handler for per-tenant TTS config.
func NewTTSConfigHandler(sc store.SystemConfigStore, cs store.ConfigSecretsStore) *TTSConfigHandler {
	return &TTSConfigHandler{systemConfigs: sc, configSecrets: cs}
}

// RegisterRoutes wires TTS config endpoints onto mux with RoleAdmin auth.
func (h *TTSConfigHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/tts/config", requireAuth(permissions.RoleAdmin, h.handleGet))
	mux.HandleFunc("POST /v1/tts/config", requireAuth(permissions.RoleAdmin, h.handleSave))
}

// ttsConfigResponse is the response for GET /v1/tts/config.
type ttsConfigResponse struct {
	Provider   string                    `json:"provider"`
	Auto       string                    `json:"auto"`
	Mode       string                    `json:"mode"`
	MaxLength  int                       `json:"max_length"`
	TimeoutMs  int                       `json:"timeout_ms"`
	OpenAI     ttsProviderConfigResponse `json:"openai"`
	ElevenLabs ttsProviderConfigResponse `json:"elevenlabs"`
	Edge       ttsProviderConfigResponse `json:"edge"`
	MiniMax    ttsProviderConfigResponse `json:"minimax"`
}

type ttsProviderConfigResponse struct {
	APIKey  string `json:"api_key,omitempty"` // masked
	APIBase string `json:"api_base,omitempty"`
	BaseURL string `json:"base_url,omitempty"`
	Voice   string `json:"voice,omitempty"`
	VoiceID string `json:"voice_id,omitempty"`
	Model   string `json:"model,omitempty"`
	ModelID string `json:"model_id,omitempty"`
	GroupID string `json:"group_id,omitempty"`
	Enabled *bool  `json:"enabled,omitempty"`
	Rate    string `json:"rate,omitempty"`
}

// handleGet returns TTS config for the current tenant.
func (h *TTSConfigHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		http.Error(w, `{"error":"tenant context required"}`, http.StatusBadRequest)
		return
	}

	resp := ttsConfigResponse{
		Auto:      "off",
		Mode:      "final",
		MaxLength: 1500,
		TimeoutMs: 30000,
	}

	// Load from system_configs
	if h.systemConfigs != nil {
		if v, _ := h.systemConfigs.Get(ctx, "tts.provider"); v != "" {
			resp.Provider = v
		}
		if v, _ := h.systemConfigs.Get(ctx, "tts.auto"); v != "" {
			resp.Auto = v
		}
		if v, _ := h.systemConfigs.Get(ctx, "tts.mode"); v != "" {
			resp.Mode = v
		}
		if v, _ := h.systemConfigs.Get(ctx, "tts.max_length"); v != "" {
			if ml, err := strconv.Atoi(v); err == nil && ml > 0 {
				resp.MaxLength = ml
			}
		}
		if v, _ := h.systemConfigs.Get(ctx, "tts.timeout_ms"); v != "" {
			if timeoutMs, err := strconv.Atoi(v); err == nil && timeoutMs > 0 {
				resp.TimeoutMs = timeoutMs
			}
		}
		// Provider-specific non-secrets
		if v, _ := h.systemConfigs.Get(ctx, "tts.openai.api_base"); v != "" {
			resp.OpenAI.APIBase = v
		}
		if v, _ := h.systemConfigs.Get(ctx, "tts.openai.voice"); v != "" {
			resp.OpenAI.Voice = v
		}
		if v, _ := h.systemConfigs.Get(ctx, "tts.openai.model"); v != "" {
			resp.OpenAI.Model = v
		}
		if v, _ := h.systemConfigs.Get(ctx, "tts.elevenlabs.api_base"); v != "" {
			resp.ElevenLabs.APIBase = v
			resp.ElevenLabs.BaseURL = v
		}
		if v, _ := h.systemConfigs.Get(ctx, "tts.elevenlabs.voice"); v != "" {
			resp.ElevenLabs.Voice = v
			resp.ElevenLabs.VoiceID = v
		}
		if v, _ := h.systemConfigs.Get(ctx, "tts.elevenlabs.model"); v != "" {
			resp.ElevenLabs.Model = v
			resp.ElevenLabs.ModelID = v
		}
		if v, _ := h.systemConfigs.Get(ctx, "tts.edge.voice"); v != "" {
			resp.Edge.Voice = v
			resp.Edge.VoiceID = v
		}
		if v, _ := h.systemConfigs.Get(ctx, "tts.edge.rate"); v != "" {
			resp.Edge.Rate = v
		}
		if v, _ := h.systemConfigs.Get(ctx, "tts.edge.enabled"); v != "" {
			enabled := v == "true"
			resp.Edge.Enabled = &enabled
		}
		if v, _ := h.systemConfigs.Get(ctx, "tts.minimax.api_base"); v != "" {
			resp.MiniMax.APIBase = v
		}
		if v, _ := h.systemConfigs.Get(ctx, "tts.minimax.voice"); v != "" {
			resp.MiniMax.Voice = v
			resp.MiniMax.VoiceID = v
		}
		if v, _ := h.systemConfigs.Get(ctx, "tts.minimax.model"); v != "" {
			resp.MiniMax.Model = v
			resp.MiniMax.ModelID = v
		}
	}

	// Load secrets (masked)
	if h.configSecrets != nil {
		if v, _ := h.configSecrets.Get(ctx, "tts.openai.api_key"); v != "" {
			resp.OpenAI.APIKey = "***"
		}
		if v, _ := h.configSecrets.Get(ctx, "tts.elevenlabs.api_key"); v != "" {
			resp.ElevenLabs.APIKey = "***"
		}
		if v, _ := h.configSecrets.Get(ctx, "tts.minimax.api_key"); v != "" {
			resp.MiniMax.APIKey = "***"
		}
		if v, _ := h.configSecrets.Get(ctx, "tts.minimax.group_id"); v != "" {
			resp.MiniMax.GroupID = v // not a secret, but stored with secrets for grouping
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// ttsConfigSaveRequest is the request body for POST /v1/tts/config.
type ttsConfigSaveRequest struct {
	Provider   string                  `json:"provider"`
	Auto       string                  `json:"auto"`
	Mode       string                  `json:"mode"`
	MaxLength  int                     `json:"max_length"`
	TimeoutMs  int                     `json:"timeout_ms"`
	OpenAI     *ttsProviderSaveRequest `json:"openai,omitempty"`
	ElevenLabs *ttsProviderSaveRequest `json:"elevenlabs,omitempty"`
	Edge       *ttsProviderSaveRequest `json:"edge,omitempty"`
	MiniMax    *ttsProviderSaveRequest `json:"minimax,omitempty"`
}

type ttsProviderSaveRequest struct {
	APIKey  string `json:"api_key,omitempty"`
	APIBase string `json:"api_base,omitempty"`
	BaseURL string `json:"base_url,omitempty"`
	Voice   string `json:"voice,omitempty"`
	VoiceID string `json:"voice_id,omitempty"`
	Model   string `json:"model,omitempty"`
	ModelID string `json:"model_id,omitempty"`
	GroupID string `json:"group_id,omitempty"`
	Enabled *bool  `json:"enabled,omitempty"`
	Rate    string `json:"rate,omitempty"`
}

// handleSave saves TTS config for the current tenant.
func (h *TTSConfigHandler) handleSave(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		http.Error(w, `{"error":"tenant context required"}`, http.StatusBadRequest)
		return
	}

	var req ttsConfigSaveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"invalid json: %s"}`, err.Error()), http.StatusBadRequest)
		return
	}

	// Save to system_configs (non-secrets)
	if h.systemConfigs != nil {
		set := func(key, val, label string) bool {
			return saveOrFail(w, ctx, h.systemConfigs.Set, key, val, label)
		}
		if req.Provider != "" && !set("tts.provider", req.Provider, "provider") {
			return
		}
		if req.Auto != "" && !set("tts.auto", req.Auto, "auto") {
			return
		}
		if req.Mode != "" && !set("tts.mode", req.Mode, "mode") {
			return
		}
		if req.MaxLength > 0 && !set("tts.max_length", strconv.Itoa(req.MaxLength), "max_length") {
			return
		}
		if req.TimeoutMs > 0 && !set("tts.timeout_ms", strconv.Itoa(req.TimeoutMs), "timeout_ms") {
			return
		}

		// Provider-specific non-secrets
		if req.OpenAI != nil {
			if v := req.OpenAI.resolvedAPIBase(); v != "" && !set("tts.openai.api_base", v, "openai api_base") {
				return
			}
			if v := req.OpenAI.resolvedVoice(); v != "" && !set("tts.openai.voice", v, "openai voice") {
				return
			}
			if v := req.OpenAI.resolvedModel(); v != "" && !set("tts.openai.model", v, "openai model") {
				return
			}
		}
		if req.ElevenLabs != nil {
			if v := req.ElevenLabs.resolvedAPIBase(); v != "" && !set("tts.elevenlabs.api_base", v, "elevenlabs api_base") {
				return
			}
			if v := req.ElevenLabs.resolvedVoice(); v != "" && !set("tts.elevenlabs.voice", v, "elevenlabs voice") {
				return
			}
			if v := req.ElevenLabs.resolvedModel(); v != "" && !set("tts.elevenlabs.model", v, "elevenlabs model") {
				return
			}
		}
		if req.Edge != nil {
			if v := req.Edge.resolvedVoice(); v != "" && !set("tts.edge.voice", v, "edge voice") {
				return
			}
			if req.Edge.Rate != "" && !set("tts.edge.rate", req.Edge.Rate, "edge rate") {
				return
			}
			if req.Edge.Enabled != nil && !set("tts.edge.enabled", strconv.FormatBool(*req.Edge.Enabled), "edge enabled") {
				return
			}
		}
		if req.MiniMax != nil {
			if v := req.MiniMax.resolvedAPIBase(); v != "" && !set("tts.minimax.api_base", v, "minimax api_base") {
				return
			}
			if v := req.MiniMax.resolvedVoice(); v != "" && !set("tts.minimax.voice", v, "minimax voice") {
				return
			}
			if v := req.MiniMax.resolvedModel(); v != "" && !set("tts.minimax.model", v, "minimax model") {
				return
			}
		}
	}

	// Save secrets (only if not masked)
	if h.configSecrets != nil {
		set := func(key, val, label string) bool {
			return saveOrFail(w, ctx, h.configSecrets.Set, key, val, label)
		}
		if req.OpenAI != nil && req.OpenAI.APIKey != "" && req.OpenAI.APIKey != "***" {
			if !set("tts.openai.api_key", req.OpenAI.APIKey, "openai api_key") {
				return
			}
		}
		if req.ElevenLabs != nil && req.ElevenLabs.APIKey != "" && req.ElevenLabs.APIKey != "***" {
			if !set("tts.elevenlabs.api_key", req.ElevenLabs.APIKey, "elevenlabs api_key") {
				return
			}
		}
		if req.MiniMax != nil {
			if req.MiniMax.APIKey != "" && req.MiniMax.APIKey != "***" {
				if !set("tts.minimax.api_key", req.MiniMax.APIKey, "minimax api_key") {
					return
				}
			}
			if req.MiniMax.GroupID != "" {
				if !set("tts.minimax.group_id", req.MiniMax.GroupID, "minimax group_id") {
					return
				}
			}
		}
	}

	slog.Info("tts.config: saved", "tenant", tid, "provider", req.Provider)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

// NewTenantTTSResolver creates a resolver for per-tenant TTS providers.
// Used by audio.Manager for channels TTS auto-apply.
func NewTenantTTSResolver(sc store.SystemConfigStore, cs store.ConfigSecretsStore) audio.TenantTTSResolver {
	return func(ctx context.Context) (audio.TTSProvider, string, audio.AutoMode, error) {
		if sc == nil || cs == nil {
			return nil, "", "", fmt.Errorf("stores not configured")
		}

		// Get tenant's configured provider
		providerName, err := sc.Get(ctx, "tts.provider")
		if err != nil || providerName == "" {
			return nil, "", "", fmt.Errorf("no tenant tts provider")
		}

		// Get auto mode
		autoStr, _ := sc.Get(ctx, "tts.auto")
		auto := audio.AutoMode(autoStr)
		if auto == "" {
			auto = audio.AutoOff
		}

		// Build ephemeral provider from tenant config
		req := testConnectionRequest{Provider: providerName, TimeoutMs: loadTenantTTSTimeoutMs(ctx, sc)}

		switch providerName {
		case "openai":
			if key, _ := cs.Get(ctx, "tts.openai.api_key"); key != "" {
				req.APIKey = key
			} else {
				return nil, "", "", fmt.Errorf("no api key")
			}
			req.APIBase, _ = sc.Get(ctx, "tts.openai.api_base")
			req.VoiceID, _ = sc.Get(ctx, "tts.openai.voice")
			req.ModelID, _ = sc.Get(ctx, "tts.openai.model")

		case "elevenlabs":
			if key, _ := cs.Get(ctx, "tts.elevenlabs.api_key"); key != "" {
				req.APIKey = key
			} else {
				return nil, "", "", fmt.Errorf("no api key")
			}
			req.APIBase, _ = sc.Get(ctx, "tts.elevenlabs.api_base")
			req.VoiceID, _ = sc.Get(ctx, "tts.elevenlabs.voice")
			req.ModelID, _ = sc.Get(ctx, "tts.elevenlabs.model")

		case "minimax":
			if key, _ := cs.Get(ctx, "tts.minimax.api_key"); key != "" {
				req.APIKey = key
			} else {
				return nil, "", "", fmt.Errorf("no api key")
			}
			req.GroupID, _ = cs.Get(ctx, "tts.minimax.group_id")
			req.APIBase, _ = sc.Get(ctx, "tts.minimax.api_base")
			req.VoiceID, _ = sc.Get(ctx, "tts.minimax.voice")
			req.ModelID, _ = sc.Get(ctx, "tts.minimax.model")

		case "edge":
			req.VoiceID, _ = sc.Get(ctx, "tts.edge.voice")
			req.Rate, _ = sc.Get(ctx, "tts.edge.rate")

		default:
			return nil, "", "", fmt.Errorf("unsupported provider: %s", providerName)
		}

		provider, err := createEphemeralTTSProvider(req)
		if err != nil {
			return nil, "", "", err
		}

		return provider, providerName, auto, nil
	}
}

// saveOrFail invokes setFn; on error logs + writes 500 and returns false.
func saveOrFail(w http.ResponseWriter, ctx context.Context, setFn func(context.Context, string, string) error, key, val, label string) bool {
	if err := setFn(ctx, key, val); err != nil {
		slog.Error("tts.config: failed to save "+label, "error", err)
		http.Error(w, fmt.Sprintf(`{"error":"save %s: %s"}`, label, err.Error()), http.StatusInternalServerError)
		return false
	}
	return true
}

func (r *ttsProviderSaveRequest) resolvedAPIBase() string {
	if r == nil {
		return ""
	}
	if r.APIBase != "" {
		return r.APIBase
	}
	return r.BaseURL
}

func (r *ttsProviderSaveRequest) resolvedVoice() string {
	if r == nil {
		return ""
	}
	if r.Voice != "" {
		return r.Voice
	}
	return r.VoiceID
}

func (r *ttsProviderSaveRequest) resolvedModel() string {
	if r == nil {
		return ""
	}
	if r.Model != "" {
		return r.Model
	}
	return r.ModelID
}
