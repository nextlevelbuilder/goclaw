package http

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

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
	Provider  string                    `json:"provider"`
	Auto      string                    `json:"auto"`
	Mode      string                    `json:"mode"`
	MaxLength int                       `json:"max_length"`
	OpenAI    ttsProviderConfigResponse `json:"openai"`
	ElevenLabs ttsProviderConfigResponse `json:"elevenlabs"`
	Edge      ttsProviderConfigResponse `json:"edge"`
	MiniMax   ttsProviderConfigResponse `json:"minimax"`
}

type ttsProviderConfigResponse struct {
	APIKey  string `json:"api_key,omitempty"`  // masked
	APIBase string `json:"api_base,omitempty"`
	Voice   string `json:"voice,omitempty"`
	Model   string `json:"model,omitempty"`
	GroupID string `json:"group_id,omitempty"`
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
			var ml int
			if json.Unmarshal([]byte(v), &ml) == nil && ml > 0 {
				resp.MaxLength = ml
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
		}
		if v, _ := h.systemConfigs.Get(ctx, "tts.elevenlabs.voice"); v != "" {
			resp.ElevenLabs.Voice = v
		}
		if v, _ := h.systemConfigs.Get(ctx, "tts.elevenlabs.model"); v != "" {
			resp.ElevenLabs.Model = v
		}
		if v, _ := h.systemConfigs.Get(ctx, "tts.minimax.api_base"); v != "" {
			resp.MiniMax.APIBase = v
		}
		if v, _ := h.systemConfigs.Get(ctx, "tts.minimax.voice"); v != "" {
			resp.MiniMax.Voice = v
		}
		if v, _ := h.systemConfigs.Get(ctx, "tts.minimax.model"); v != "" {
			resp.MiniMax.Model = v
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
	Provider   string                   `json:"provider"`
	Auto       string                   `json:"auto"`
	Mode       string                   `json:"mode"`
	MaxLength  int                      `json:"max_length"`
	OpenAI     *ttsProviderSaveRequest  `json:"openai,omitempty"`
	ElevenLabs *ttsProviderSaveRequest  `json:"elevenlabs,omitempty"`
	MiniMax    *ttsProviderSaveRequest  `json:"minimax,omitempty"`
}

type ttsProviderSaveRequest struct {
	APIKey  string `json:"api_key,omitempty"`
	APIBase string `json:"api_base,omitempty"`
	Voice   string `json:"voice,omitempty"`
	Model   string `json:"model,omitempty"`
	GroupID string `json:"group_id,omitempty"`
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
			h.systemConfigs.Set(ctx, "tts.provider", req.Provider)
		}
		if req.Auto != "" {
			h.systemConfigs.Set(ctx, "tts.auto", req.Auto)
		}
		if req.Mode != "" {
			h.systemConfigs.Set(ctx, "tts.mode", req.Mode)
		}
		if req.MaxLength > 0 {
			h.systemConfigs.Set(ctx, "tts.max_length", fmt.Sprintf("%d", req.MaxLength))
		}

		// Provider-specific non-secrets
		if req.OpenAI != nil {
			if req.OpenAI.APIBase != "" {
				h.systemConfigs.Set(ctx, "tts.openai.api_base", req.OpenAI.APIBase)
			}
			if req.OpenAI.Voice != "" {
				h.systemConfigs.Set(ctx, "tts.openai.voice", req.OpenAI.Voice)
			}
			if req.OpenAI.Model != "" {
				h.systemConfigs.Set(ctx, "tts.openai.model", req.OpenAI.Model)
			}
		}
		if req.ElevenLabs != nil {
			if req.ElevenLabs.APIBase != "" {
				h.systemConfigs.Set(ctx, "tts.elevenlabs.api_base", req.ElevenLabs.APIBase)
			}
			if req.ElevenLabs.Voice != "" {
				h.systemConfigs.Set(ctx, "tts.elevenlabs.voice", req.ElevenLabs.Voice)
			}
			if req.ElevenLabs.Model != "" {
				h.systemConfigs.Set(ctx, "tts.elevenlabs.model", req.ElevenLabs.Model)
			}
		}
		if req.MiniMax != nil {
			if req.MiniMax.APIBase != "" {
				h.systemConfigs.Set(ctx, "tts.minimax.api_base", req.MiniMax.APIBase)
			}
			if req.MiniMax.Voice != "" {
				h.systemConfigs.Set(ctx, "tts.minimax.voice", req.MiniMax.Voice)
			}
			if req.MiniMax.Model != "" {
				h.systemConfigs.Set(ctx, "tts.minimax.model", req.MiniMax.Model)
			}
		}
	}

	// Save secrets (only if not masked)
	if h.configSecrets != nil {
		if req.OpenAI != nil && req.OpenAI.APIKey != "" && req.OpenAI.APIKey != "***" {
			if err := h.configSecrets.Set(ctx, "tts.openai.api_key", req.OpenAI.APIKey); err != nil {
				slog.Warn("tts.config: failed to save openai api_key", "error", err)
			}
		}
		if req.ElevenLabs != nil && req.ElevenLabs.APIKey != "" && req.ElevenLabs.APIKey != "***" {
			if err := h.configSecrets.Set(ctx, "tts.elevenlabs.api_key", req.ElevenLabs.APIKey); err != nil {
				slog.Warn("tts.config: failed to save elevenlabs api_key", "error", err)
			}
		}
		if req.MiniMax != nil {
			if req.MiniMax.APIKey != "" && req.MiniMax.APIKey != "***" {
				if err := h.configSecrets.Set(ctx, "tts.minimax.api_key", req.MiniMax.APIKey); err != nil {
					slog.Warn("tts.config: failed to save minimax api_key", "error", err)
				}
			}
			if req.MiniMax.GroupID != "" {
				if err := h.configSecrets.Set(ctx, "tts.minimax.group_id", req.MiniMax.GroupID); err != nil {
					slog.Warn("tts.config: failed to save minimax group_id", "error", err)
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
		req := testConnectionRequest{Provider: providerName}

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
