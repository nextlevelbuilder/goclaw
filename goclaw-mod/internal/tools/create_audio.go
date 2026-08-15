package tools

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// CreateAudioTool generates music or sound effects using the audio.Manager.
// Provider selection and fallback are handled by the Manager's chain logic.
type CreateAudioTool struct {
	audioMgr  *audio.Manager
	vaultIntc *VaultInterceptor
}

func (t *CreateAudioTool) SetVaultInterceptor(v *VaultInterceptor) { t.vaultIntc = v }

// NewCreateAudioTool creates a CreateAudioTool backed by the audio.Manager.
func NewCreateAudioTool(audioMgr *audio.Manager) *CreateAudioTool {
	return &CreateAudioTool{audioMgr: audioMgr}
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
	filenameHint, _ := args["filename_hint"].(string)

	var res *audio.AudioResult
	var err error

	switch audioType {
	case "sound_effect":
		res, err = t.audioMgr.GenerateSFX(ctx, audio.SFXOptions{
			Prompt:   prompt,
			Duration: duration,
		})
		if err != nil {
			return ErrorResult(fmt.Sprintf("sound effect generation failed: %v", err))
		}
	case "music":
		res, err = t.audioMgr.GenerateMusic(ctx, audio.MusicOptions{
			Prompt:       prompt,
			Lyrics:       lyrics,
			Instrumental: instrumental,
			TimeoutSec:   300,
		})
		if err != nil {
			return ErrorResult(fmt.Sprintf("music generation failed: %v", err))
		}
	default:
		return ErrorResult(fmt.Sprintf("unsupported audio type: %s", audioType))
	}

	audioBytes := res.Audio
	providerName := res.Provider
	model := res.Model

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
		slog.Info("create_audio: file saved",
			"path", audioPath, "size", fi.Size(),
			"data_len", len(audioBytes), "model", model, "type", audioType)
	}

	result := &Result{ForLLM: fmt.Sprintf("MEDIA:%s\nUse the EXACT filename when referencing: %s", audioPath, filepath.Base(audioPath))}
	result.Media = []bus.MediaFile{{Path: audioPath, MimeType: "audio/mpeg", Filename: filepath.Base(audioPath)}}
	result.Deliverable = fmt.Sprintf("[Generated audio: %s]\nPrompt: %s", filepath.Base(audioPath), prompt)
	if t.vaultIntc != nil {
		go t.vaultIntc.AfterWriteMedia(context.WithoutCancel(ctx), audioPath, prompt, "audio/mpeg")
	}
	result.Provider = providerName
	result.Model = model
	return result
}
