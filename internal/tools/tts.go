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
	Primary string `json:"primary,omitempty"` // override primary provider name
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

	voice, _ := args["voice"].(string)
	providerName, _ := args["provider"].(string)

	// Snapshot manager pointer under read lock so config reloads don't race.
	t.mu.RLock()
	mgr := t.manager
	t.mu.RUnlock()

	// Determine format based on channel (read from ctx — thread-safe)
	channel := ToolChannelFromCtx(ctx)
	opts := tts.Options{Voice: voice}
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
	r := &Result{ForLLM: forLLM}
	r.Deliverable = fmt.Sprintf("[Generated audio: %s]\nText: %s", filepath.Base(audioPath), text)
	if t.vaultIntc != nil {
		mimeType := "audio/" + result.Extension
		go t.vaultIntc.AfterWriteMedia(context.WithoutCancel(ctx), audioPath, text, mimeType)
	}
	return r
}
