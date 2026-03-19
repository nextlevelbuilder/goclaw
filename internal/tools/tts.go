package tools

import (
	"context"
	"fmt"
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
	mu      sync.RWMutex
	manager *tts.Manager
}

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
		// Use specific provider
		p, ok := mgr.GetProvider(providerName)
		if !ok {
			return &Result{ForLLM: fmt.Sprintf("error: tts provider not found: %s", providerName), IsError: true}
		}
		result, err = p.Synthesize(ctx, text, opts)
	} else {
		result, err = mgr.SynthesizeWithFallback(ctx, text, opts)
	}

	if err != nil {
		return &Result{ForLLM: fmt.Sprintf("error: tts failed: %s", err.Error()), IsError: true}
	}

	// Write audio to temp file
	tmpDir := os.TempDir()
	audioPath := filepath.Join(tmpDir, fmt.Sprintf("tts-%d.%s", time.Now().UnixNano(), result.Extension))
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
	return r
}
