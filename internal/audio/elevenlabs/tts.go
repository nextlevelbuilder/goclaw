package elevenlabs

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
)

// TTSProvider synthesizes text via POST /v1/text-to-speech/{voice_id}.
// Legacy name: internal/tts.ElevenLabsProvider.
type TTSProvider struct {
	cfg Config
	c   *client
}

// NewTTSProvider returns an ElevenLabs TTS provider backed by the shared client.
func NewTTSProvider(cfg Config) *TTSProvider {
	cfg = cfg.withDefaults()
	return &TTSProvider{
		cfg: cfg,
		c:   newClient(cfg.APIKey, cfg.BaseURL, cfg.TimeoutMs),
	}
}

// Name returns the stable provider identifier used by the Manager.
func (p *TTSProvider) Name() string { return "elevenlabs" }

// Synthesize converts text to audio. Opts.Voice/Opts.Model override the
// configured defaults; Opts.Format="opus" switches to Ogg Opus output.
func (p *TTSProvider) Synthesize(ctx context.Context, text string, opts audio.TTSOptions) (*audio.SynthResult, error) {
	voiceID := opts.Voice
	if voiceID == "" {
		voiceID = p.cfg.VoiceID
	}
	modelID := opts.Model
	if modelID == "" {
		modelID = p.cfg.ModelID
	}
	if err := ValidateModel(modelID); err != nil {
		return nil, err
	}

	// Output format: MP3 128kbps default, Opus 64kbps for Telegram voice.
	outputFormat, ext, mime := "mp3_44100_128", "mp3", "audio/mpeg"
	if opts.Format == "opus" {
		outputFormat, ext, mime = "opus_48000_64", "ogg", "audio/ogg"
	}

	body := map[string]any{
		"text":     text,
		"model_id": modelID,
		"voice_settings": map[string]any{
			"stability":         0.5,
			"similarity_boost":  0.75,
			"style":             0.0,
			"use_speaker_boost": true,
		},
	}
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal elevenlabs tts request: %w", err)
	}

	path := fmt.Sprintf("/v1/text-to-speech/%s?output_format=%s", voiceID, outputFormat)
	audioBytes, err := p.c.postJSON(ctx, path, bodyJSON, 0)
	if err != nil {
		return nil, fmt.Errorf("elevenlabs tts: %w", err)
	}

	return &audio.SynthResult{
		Audio:     audioBytes,
		Extension: ext,
		MimeType:  mime,
	}, nil
}
