package tools

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// audioGenProviderPriority is the default fallback order for music generation providers.
var audioGenProviderPriority = []string{"minimax"}

// audioGenModelDefaults maps provider names to default audio generation models.
var audioGenModelDefaults = map[string]string{
	"minimax": "music-2.5+",
}

// CreateAudioTool generates music or sound effects using AI audio generation APIs.
type CreateAudioTool struct {
	registry          *providers.Registry
	elevenlabsAPIKey  string
	elevenlabsBaseURL string
}

// NewCreateAudioTool creates a CreateAudioTool.
// elevenlabsKey and elevenlabsBase are used for ElevenLabs sound effects generation.
func NewCreateAudioTool(registry *providers.Registry, elevenlabsKey, elevenlabsBase string) *CreateAudioTool {
	return &CreateAudioTool{
		registry:          registry,
		elevenlabsAPIKey:  elevenlabsKey,
		elevenlabsBaseURL: elevenlabsBase,
	}
}

func (t *CreateAudioTool) Name() string { return "create_audio" }

func (t *CreateAudioTool) Description() string {
	return "Generate music, sound effects, or ambient audio from a text description. Returns a MEDIA: path to the generated audio file. Note: for music, duration is determined by lyrics length — provide longer/shorter lyrics to control length. The 'duration' parameter only applies to sound effects."
}

func (t *CreateAudioTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"prompt": map[string]any{
				"type":        "string",
				"description": "Description of audio to generate.",
			},
			"type": map[string]any{
				"type":        "string",
				"description": "Type of audio: 'music' (default) or 'sound_effect'.",
			},
			"duration": map[string]any{
				"type":        "integer",
				"description": "Duration in seconds (only for sound effects). For music, duration is controlled by lyrics length.",
			},
			"lyrics": map[string]any{
				"type":        "string",
				"description": "Lyrics for music generation. Use [Verse], [Chorus] tags.",
			},
			"instrumental": map[string]any{
				"type":        "boolean",
				"description": "No vocals (default: false).",
			},
			"provider": map[string]any{
				"type":        "string",
				"description": "Force a specific provider (e.g. 'minimax').",
			},
			"filename_hint": map[string]any{
				"type":        "string",
				"description": "Short descriptive filename (no extension). Example: 'epic-battle-theme', 'rain-ambience'.",
			},
		},
		"required": []string{"prompt"},
	}
}

func (t *CreateAudioTool) Execute(ctx context.Context, args map[string]any) *Result {
	prompt, _ := args["prompt"].(string)
	if prompt == "" {
		return ErrorResult("prompt is required")
	}

	audioType, _ := args["type"].(string)
	if audioType == "" {
		audioType = "music"
	}

	duration := 0
	if d, ok := args["duration"].(float64); ok {
		duration = int(d)
	}

	lyrics, _ := args["lyrics"].(string)
	instrumental, _ := args["instrumental"].(bool)
	forceProvider, _ := args["provider"].(string)
	filenameHint, _ := args["filename_hint"].(string)

	var audioBytes []byte
	var usage *providers.Usage
	var providerName, model string

	// Sound effects: route directly to ElevenLabs if credentials available.
	if audioType == "sound_effect" {
		if t.elevenlabsAPIKey == "" {
			return ErrorResult("sound_effect generation requires ElevenLabs credentials (elevenlabs.api_key in config)")
		}
		var err error
		audioBytes, err = t.callElevenLabsSoundEffect(ctx, prompt, duration)
		if err != nil {
			return ErrorResult(fmt.Sprintf("ElevenLabs sound effect generation failed: %v", err))
		}
		providerName = "elevenlabs"
		model = "sound-generation"
	} else {
		// Music: use the provider chain.
		chain := ResolveMediaProviderChain(ctx, "create_audio", forceProvider, "",
			audioGenProviderPriority, audioGenModelDefaults, t.registry)

		// Inject prompt and music-specific params into each chain entry.
		for i := range chain {
			if chain[i].Params == nil {
				chain[i].Params = make(map[string]any)
			}
			chain[i].Params["prompt"] = prompt
			chain[i].Params["lyrics"] = lyrics
			chain[i].Params["instrumental"] = instrumental
		}

		// Override timeout to 300s for music generation (slow API).
		for i := range chain {
			if chain[i].Timeout < 300 {
				chain[i].Timeout = 300
			}
		}

		chainResult, err := ExecuteWithChain(ctx, chain, t.registry, t.callProvider)
		if err != nil {
			return ErrorResult(fmt.Sprintf("audio generation failed: %v", err))
		}
		audioBytes = chainResult.Data
		usage = chainResult.Usage
		providerName = chainResult.Provider
		model = chainResult.Model
	}

	// Save to workspace under date-based folder.
	workspace := ToolWorkspaceFromCtx(ctx)
	if workspace == "" {
		workspace = os.TempDir()
	}
	dateDir := filepath.Join(workspace, "generated", time.Now().Format("2006-01-02"))
	if err := os.MkdirAll(dateDir, 0755); err != nil {
		return ErrorResult(fmt.Sprintf("failed to create output directory: %v", err))
	}
	audioPath := filepath.Join(dateDir, mediaFileName(ctx, "audio", filenameHint, "mp3"))
	if err := os.WriteFile(audioPath, audioBytes, 0644); err != nil {
		return ErrorResult(fmt.Sprintf("failed to save generated audio: %v", err))
	}

	// Verify file was persisted.
	if fi, err := os.Stat(audioPath); err != nil {
		slog.Warn("create_audio: file missing immediately after write", "path", audioPath, "error", err)
		return ErrorResult(fmt.Sprintf("generated audio file missing after write: %v", err))
	} else {
		slog.Info("create_audio: file saved", "path", audioPath, "size", fi.Size(), "data_len", len(audioBytes), "provider", providerName, "type", audioType)
	}

	result := &Result{ForLLM: fmt.Sprintf("MEDIA:%s\nUse the EXACT filename when referencing: %s", audioPath, filepath.Base(audioPath))}
	result.Media = []bus.MediaFile{{Path: audioPath, MimeType: "audio/mpeg"}}
	result.Deliverable = fmt.Sprintf("[Generated audio: %s]\nPrompt: %s", filepath.Base(audioPath), prompt)
	result.Provider = providerName
	result.Model = model
	if usage != nil {
		result.Usage = usage
	}
	return result
}

// callProvider dispatches to the correct music generation implementation based on provider type.
func (t *CreateAudioTool) callProvider(ctx context.Context, cp credentialProvider, providerName, model string, params map[string]any) ([]byte, *providers.Usage, error) {
	if cp == nil {
		return nil, nil, fmt.Errorf("provider %q does not expose API credentials required for audio generation", providerName)
	}
	prompt := GetParamString(params, "prompt", "")

	slog.Info("create_audio: calling music generation API",
		"provider", providerName, "model", model)

	switch GetParamString(params, "_provider_type", providerTypeFromName(providerName)) {
	case "minimax":
		return callMinimaxMusicGen(ctx, cp.APIKey(), cp.APIBase(), model, prompt, params)
	case "suno":
		return callSunoMusicGen(ctx, cp.APIKey(), cp.APIBase(), model, prompt, params)
	default:
		return nil, nil, fmt.Errorf("unsupported provider for audio generation: %q", providerName)
	}
}
