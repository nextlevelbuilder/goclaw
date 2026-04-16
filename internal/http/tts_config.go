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
		if req.Provider != "" {
			if err := h.systemConfigs.Set(ctx, "tts.provider", req.Provider); err != nil {
				slog.Error("tts.config: failed to save provider", "error", err)
				http.Error(w, fmt.Sprintf(`{"error":"save provider: %s"}`, err.Error()), http.StatusInternalServerError)
				return
			}
		}
		if req.Auto != "" {
			if err := h.systemConfigs.Set(ctx, "tts.auto", req.Auto); err != nil {
				slog.Error("tts.config: failed to save auto", "error", err)
				http.Error(w, fmt.Sprintf(`{"error":"save auto: %s"}`, err.Error()), http.StatusInternalServerError)
				return
			}
		}
		if req.Mode != "" {
			if err := h.systemConfigs.Set(ctx, "tts.mode", req.Mode); err != nil {
				slog.Error("tts.config: failed to save mode", "error", err)
				http.Error(w, fmt.Sprintf(`{"error":"save mode: %s"}`, err.Error()), http.StatusInternalServerError)
				return
			}
		}
		if req.MaxLength > 0 {
			if err := h.systemConfigs.Set(ctx, "tts.max_length", fmt.Sprintf("%d", req.MaxLength)); err != nil {
				slog.Error("tts.config: failed to save max_length", "error", err)
				http.Error(w, fmt.Sprintf(`{"error":"save max_length: %s"}`, err.Error()), http.StatusInternalServerError)
				return
			}
		}
		if req.TimeoutMs > 0 {
			if err := h.systemConfigs.Set(ctx, "tts.timeout_ms", fmt.Sprintf("%d", req.TimeoutMs)); err != nil {
				slog.Error("tts.config: failed to save timeout_ms", "error", err)
				http.Error(w, fmt.Sprintf(`{"error":"save timeout_ms: %s"}`, err.Error()), http.StatusInternalServerError)
				return
			}
		}

		// Provider-specific non-secrets
		if req.OpenAI != nil {
			if apiBase := req.OpenAI.resolvedAPIBase(); apiBase != "" {
				if err := h.systemConfigs.Set(ctx, "tts.openai.api_base", apiBase); err != nil {
					slog.Error("tts.config: failed to save openai api_base", "error", err)
					http.Error(w, fmt.Sprintf(`{"error":"save openai api_base: %s"}`, err.Error()), http.StatusInternalServerError)
					return
				}
			}
			if voice := req.OpenAI.resolvedVoice(); voice != "" {
				if err := h.systemConfigs.Set(ctx, "tts.openai.voice", voice); err != nil {
					slog.Error("tts.config: failed to save openai voice", "error", err)
					http.Error(w, fmt.Sprintf(`{"error":"save openai voice: %s"}`, err.Error()), http.StatusInternalServerError)
					return
				}
			}
			if model := req.OpenAI.resolvedModel(); model != "" {
				if err := h.systemConfigs.Set(ctx, "tts.openai.model", model); err != nil {
					slog.Error("tts.config: failed to save openai model", "error", err)
					http.Error(w, fmt.Sprintf(`{"error":"save openai model: %s"}`, err.Error()), http.StatusInternalServerError)
					return
				}
			}
		}
		if req.ElevenLabs != nil {
			if apiBase := req.ElevenLabs.resolvedAPIBase(); apiBase != "" {
				if err := h.systemConfigs.Set(ctx, "tts.elevenlabs.api_base", apiBase); err != nil {
					slog.Error("tts.config: failed to save elevenlabs api_base", "error", err)
					http.Error(w, fmt.Sprintf(`{"error":"save elevenlabs api_base: %s"}`, err.Error()), http.StatusInternalServerError)
					return
				}
			}
			if voice := req.ElevenLabs.resolvedVoice(); voice != "" {
				if err := h.systemConfigs.Set(ctx, "tts.elevenlabs.voice", voice); err != nil {
					slog.Error("tts.config: failed to save elevenlabs voice", "error", err)
					http.Error(w, fmt.Sprintf(`{"error":"save elevenlabs voice: %s"}`, err.Error()), http.StatusInternalServerError)
					return
				}
			}
			if model := req.ElevenLabs.resolvedModel(); model != "" {
				if err := h.systemConfigs.Set(ctx, "tts.elevenlabs.model", model); err != nil {
					slog.Error("tts.config: failed to save elevenlabs model", "error", err)
					http.Error(w, fmt.Sprintf(`{"error":"save elevenlabs model: %s"}`, err.Error()), http.StatusInternalServerError)
					return
				}
			}
		}
		if req.Edge != nil {
			if voice := req.Edge.resolvedVoice(); voice != "" {
				if err := h.systemConfigs.Set(ctx, "tts.edge.voice", voice); err != nil {
					slog.Error("tts.config: failed to save edge voice", "error", err)
					http.Error(w, fmt.Sprintf(`{"error":"save edge voice: %s"}`, err.Error()), http.StatusInternalServerError)
					return
				}
			}
			if req.Edge.Rate != "" {
				if err := h.systemConfigs.Set(ctx, "tts.edge.rate", req.Edge.Rate); err != nil {
					slog.Error("tts.config: failed to save edge rate", "error", err)
					http.Error(w, fmt.Sprintf(`{"error":"save edge rate: %s"}`, err.Error()), http.StatusInternalServerError)
					return
				}
			}
			if req.Edge.Enabled != nil {
				if err := h.systemConfigs.Set(ctx, "tts.edge.enabled", fmt.Sprintf("%t", *req.Edge.Enabled)); err != nil {
					slog.Error("tts.config: failed to save edge enabled", "error", err)
					http.Error(w, fmt.Sprintf(`{"error":"save edge enabled: %s"}`, err.Error()), http.StatusInternalServerError)
					return
				}
			}
		}
		if req.MiniMax != nil {
			if apiBase := req.MiniMax.resolvedAPIBase(); apiBase != "" {
				if err := h.systemConfigs.Set(ctx, "tts.minimax.api_base", apiBase); err != nil {
					slog.Error("tts.config: failed to save minimax api_base", "error", err)
					http.Error(w, fmt.Sprintf(`{"error":"save minimax api_base: %s"}`, err.Error()), http.StatusInternalServerError)
					return
				}
			}
			if voice := req.MiniMax.resolvedVoice(); voice != "" {
				if err := h.systemConfigs.Set(ctx, "tts.minimax.voice", voice); err != nil {
					slog.Error("tts.config: failed to save minimax voice", "error", err)
					http.Error(w, fmt.Sprintf(`{"error":"save minimax voice: %s"}`, err.Error()), http.StatusInternalServerError)
					return
				}
			}
			if model := req.MiniMax.resolvedModel(); model != "" {
				if err := h.systemConfigs.Set(ctx, "tts.minimax.model", model); err != nil {
					slog.Error("tts.config: failed to save minimax model", "error", err)
					http.Error(w, fmt.Sprintf(`{"error":"save minimax model: %s"}`, err.Error()), http.StatusInternalServerError)
					return
				}
			}
		}
	}

	// Save secrets (only if not masked)
	if h.configSecrets != nil {
		if req.OpenAI != nil && req.OpenAI.APIKey != "" && req.OpenAI.APIKey != "***" {
			if err := h.configSecrets.Set(ctx, "tts.openai.api_key", req.OpenAI.APIKey); err != nil {
				slog.Error("tts.config: failed to save openai api_key", "error", err)
				http.Error(w, fmt.Sprintf(`{"error":"save openai api_key: %s"}`, err.Error()), http.StatusInternalServerError)
				return
			}
		}
		if req.ElevenLabs != nil && req.ElevenLabs.APIKey != "" && req.ElevenLabs.APIKey != "***" {
			if err := h.configSecrets.Set(ctx, "tts.elevenlabs.api_key", req.ElevenLabs.APIKey); err != nil {
				slog.Error("tts.config: failed to save elevenlabs api_key", "error", err)
				http.Error(w, fmt.Sprintf(`{"error":"save elevenlabs api_key: %s"}`, err.Error()), http.StatusInternalServerError)
				return
			}
		}
		if req.MiniMax != nil {
			if req.MiniMax.APIKey != "" && req.MiniMax.APIKey != "***" {
				if err := h.configSecrets.Set(ctx, "tts.minimax.api_key", req.MiniMax.APIKey); err != nil {
					slog.Error("tts.config: failed to save minimax api_key", "error", err)
					http.Error(w, fmt.Sprintf(`{"error":"save minimax api_key: %s"}`, err.Error()), http.StatusInternalServerError)
					return
				}
			}
			if req.MiniMax.GroupID != "" {
				if err := h.configSecrets.Set(ctx, "tts.minimax.group_id", req.MiniMax.GroupID); err != nil {
					slog.Error("tts.config: failed to save minimax group_id", "error", err)
					http.Error(w, fmt.Sprintf(`{"error":"save minimax group_id: %s"}`, err.Error()), http.StatusInternalServerError)
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
