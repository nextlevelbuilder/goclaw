package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tts"
)

// TtsTool is an agent tool that converts text to speech audio.
// Matching TS src/agents/tools/tts-tool.ts.
// Implements Tool + ContextualTool interfaces.
// Per-call channel is read from ctx for thread-safety.
type TtsTool struct {
	mu        sync.RWMutex
	manager   *tts.Manager
	vaultIntc *VaultInterceptor
}

func (t *TtsTool) SetVaultInterceptor(v *VaultInterceptor) { t.vaultIntc = v }

// NewTtsTool creates a TTS tool backed by the given manager.
func NewTtsTool(mgr *tts.Manager) *TtsTool {
	return &TtsTool{manager: mgr}
}

// UpdateManager swaps the underlying TTS manager (used on config reload).
func (t *TtsTool) UpdateManager(mgr *tts.Manager) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.manager = mgr
}

func (t *TtsTool) Name() string { return "tts" }

func (t *TtsTool) Description() string {
	return "Convert text to speech audio. Returns a MEDIA: path to the generated audio file."
}

func (t *TtsTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"text": map[string]any{
				"type":        "string",
				"description": "The text to convert to speech",
			},
			"voice": map[string]any{
				"type":        "string",
				"description": "Voice ID (provider-specific). Optional — uses default if omitted.",
			},
			"model": map[string]any{
				"type":        "string",
				"description": "Model ID (provider-specific, e.g. eleven_v3). Optional — uses default if omitted.",
			},
			"provider": map[string]any{
				"type":        "string",
				"description": "TTS provider: openai, elevenlabs, edge, minimax. Optional — uses primary if omitted.",
			},
		},
		"required": []string{"text"},
	}
}

// ttsOverride is the tenant settings shape for tts
// (stored in builtin_tool_tenant_configs.settings).
type ttsOverride struct {
	Primary        string `json:"primary,omitempty"`          // override primary provider name
	DefaultVoiceID string `json:"default_voice_id,omitempty"` // tenant-default voice id
	DefaultModel   string `json:"default_model,omitempty"`    // tenant-default model id
}

// agentAudioConfig is the JSON shape read from AgentAudioSnapshot.OtherConfig
// for per-agent TTS tuning. Keys match the agents.other_config column.
type agentAudioConfig struct {
	TTSVoiceID string `json:"tts_voice_id,omitempty"`
	TTSModelID string `json:"tts_model_id,omitempty"`
}

// resolveVoiceAndModel computes the effective voice + model IDs for the
// request using the documented precedence order:
//
//	args > agent (store.AgentAudioFromCtx OtherConfig) > tenant (BuiltinToolSettings) > empty.
//
// Empty return values signal "use provider default" downstream — they are not
// errors. Missing agent snapshot emits slog.Warn so operators can spot
// dispatch-layer regressions; missing tenant settings are quiet (common).
func (t *TtsTool) resolveVoiceAndModel(ctx context.Context, argVoice, argModel string) (voice, model string) {
	voice, model = argVoice, argModel

	// Pull agent-level config from the dispatcher-injected snapshot.
	var agentCfg agentAudioConfig
	if snap, ok := store.AgentAudioFromCtx(ctx); ok {
		if len(snap.OtherConfig) > 0 {
			if err := json.Unmarshal(snap.OtherConfig, &agentCfg); err != nil {
				slog.Warn("tts: failed to parse agent OtherConfig", "error", err, "agent_id", snap.AgentID)
			}
		}
	} else if agentID := store.AgentIDFromContext(ctx); agentID != uuid.Nil {
		// Producer-consumer contract violation: when an agent ctx is in play
		// (AgentIDFromContext returns non-nil), AgentAudioSnapshot should have
		// been injected by the dispatcher. Log as Warn so ops can spot a
		// dispatch-layer regression. Silent when no agent is scoped (unit
		// tests and callers outside the agent loop).
		slog.Warn("tts: agent audio snapshot missing — dispatcher producer may be offline", "agent_id", agentID)
	}

	// Pull tenant defaults from builtin tool settings.
	var tenantCfg ttsOverride
	if settings := BuiltinToolSettingsFromCtx(ctx); settings != nil {
		if raw, ok := settings["tts"]; ok && len(raw) > 0 {
			if err := json.Unmarshal(raw, &tenantCfg); err != nil {
				slog.Warn("tts: failed to parse tenant settings for voice/model", "error", err)
			}
		}
	}

	if voice == "" {
		if agentCfg.TTSVoiceID != "" {
			voice = agentCfg.TTSVoiceID
		} else if tenantCfg.DefaultVoiceID != "" {
			voice = tenantCfg.DefaultVoiceID
		}
	}
	if model == "" {
		if agentCfg.TTSModelID != "" {
			model = agentCfg.TTSModelID
		} else if tenantCfg.DefaultModel != "" {
			model = tenantCfg.DefaultModel
		}
	}
	return voice, model
}

// resolvePrimary returns the effective primary provider name for the request.
// Checks tenant override via BuiltinToolSettingsFromCtx first.
func (t *TtsTool) resolvePrimary(ctx context.Context, mgr *tts.Manager) string {
	if settings := BuiltinToolSettingsFromCtx(ctx); settings != nil {
		if raw, ok := settings["tts"]; ok && len(raw) > 0 {
			var override ttsOverride
			if err := json.Unmarshal(raw, &override); err != nil {
				slog.Warn("tts: failed to parse tenant override, using defaults", "error", err)
			} else if override.Primary != "" {
				// Verify the provider exists in the manager
				if _, exists := mgr.GetProvider(override.Primary); exists {
					return override.Primary
				}
				slog.Warn("tts: tenant override references unknown provider", "primary", override.Primary)
			}
		}
	}
	return mgr.PrimaryProvider()
}

// SetContext is a no-op; channel is now read from ctx (thread-safe).
func (t *TtsTool) SetContext(channel, _ string) {}

func (t *TtsTool) Execute(ctx context.Context, args map[string]any) *Result {
	text, _ := args["text"].(string)
	if text == "" {
		return &Result{ForLLM: "error: text is required", IsError: true}
	}

	argVoice, _ := args["voice"].(string)
	argModel, _ := args["model"].(string)
	providerName, _ := args["provider"].(string)

	// Resolve voice/model via args > agent (ctx snapshot) > tenant > default.
	voice, model := t.resolveVoiceAndModel(ctx, argVoice, argModel)

	// Snapshot manager pointer under read lock so config reloads don't race.
	t.mu.RLock()
	mgr := t.manager
	t.mu.RUnlock()

	// Determine format based on channel (read from ctx — thread-safe)
	channel := ToolChannelFromCtx(ctx)
	opts := tts.Options{Voice: voice, Model: model}
	if channel == "telegram" {
		opts.Format = "opus"
	}

	var result *tts.SynthResult
	var err error

	if providerName != "" {
		// Use specific provider (explicit call param takes precedence)
		p, ok := mgr.GetProvider(providerName)
		if !ok {
			return &Result{ForLLM: fmt.Sprintf("error: tts provider not found: %s", providerName), IsError: true}
		}
		result, err = p.Synthesize(ctx, text, opts)
	} else {
		// Resolve primary from tenant settings or default
		primary := t.resolvePrimary(ctx, mgr)
		if p, ok := mgr.GetProvider(primary); ok {
			result, err = p.Synthesize(ctx, text, opts)
			if err != nil {
				slog.Warn("tts primary provider failed, trying fallback", "provider", primary, "error", err)
				result, err = mgr.SynthesizeWithFallback(ctx, text, opts)
			}
		} else {
			result, err = mgr.SynthesizeWithFallback(ctx, text, opts)
		}
	}

	if err != nil {
		return &Result{ForLLM: fmt.Sprintf("error: tts failed: %s", err.Error()), IsError: true}
	}

	// Write audio to workspace/tts/ so the agent can access the file.
	// Falls back to os.TempDir() if workspace is not available.
	ttsDir := os.TempDir()
	if ws := ToolWorkspaceFromCtx(ctx); ws != "" {
		ttsDir = filepath.Join(ws, "tts")
	}
	if err := os.MkdirAll(ttsDir, 0755); err != nil {
		return &Result{ForLLM: fmt.Sprintf("error: create tts directory: %s", err.Error()), IsError: true}
	}
	audioPath := filepath.Join(ttsDir, fmt.Sprintf("tts-%d.%s", time.Now().UnixNano(), result.Extension))
	if err := os.WriteFile(audioPath, result.Audio, 0644); err != nil {
		return &Result{ForLLM: fmt.Sprintf("error: write tts audio: %s", err.Error()), IsError: true}
	}

	// Return MEDIA: path (matching TS pattern)
	voiceTag := ""
	if channel == "telegram" && result.Extension == "ogg" {
		voiceTag = "[[audio_as_voice]]\n"
	}

	forLLM := fmt.Sprintf("%sMEDIA:%s", voiceTag, audioPath)
	// Set Result.Media explicitly (matching create_audio) so the agent loop's
	// media collector uses the authoritative path even when the ForLLM
	// MEDIA: prefix is reshaped by a provider bridge (e.g. claude_cli MCP).
	// Prefer the provider-supplied MimeType ("audio/mpeg", "audio/ogg") over
	// "audio/"+Extension — the latter yields the non-standard "audio/mp3".
	mimeType := result.MimeType
	if mimeType == "" {
		mimeType = "audio/" + result.Extension
	}
	r := &Result{
		ForLLM: forLLM,
		Media: []bus.MediaFile{{
			Path:     audioPath,
			MimeType: mimeType,
		}},
	}
	r.Deliverable = fmt.Sprintf("[Generated audio: %s]\nText: %s", filepath.Base(audioPath), text)
	if t.vaultIntc != nil {
		mimeType := "audio/" + result.Extension
		go t.vaultIntc.AfterWriteMedia(context.WithoutCancel(ctx), audioPath, text, mimeType)
	}
	return r
}
