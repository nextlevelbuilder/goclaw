package elevenlabs

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
)

// sfxMaxDurationSeconds is ElevenLabs' upper limit for /v1/sound-generation.
const sfxMaxDurationSeconds = 30

// SFXProvider generates short sound effects via POST /v1/sound-generation.
// Migrated from internal/tools/create_audio_elevenlabs.go — byte-for-byte
// identical request payload + 60s timeout.
type SFXProvider struct {
	cfg Config
	c   *client
}

// NewSFXProvider returns an ElevenLabs SFX provider.
func NewSFXProvider(cfg Config) *SFXProvider {
	cfg = cfg.withDefaults()
	return &SFXProvider{
		cfg: cfg,
		c:   newClient(cfg.APIKey, cfg.BaseURL, cfg.TimeoutMs),
	}
}

// Name returns the stable provider identifier used by the Manager.
func (p *SFXProvider) Name() string { return "elevenlabs" }

// GenerateSFX produces an MP3 sound effect from opts.Prompt. Duration is
// capped at sfxMaxDurationSeconds (ElevenLabs limit).
func (p *SFXProvider) GenerateSFX(ctx context.Context, opts audio.SFXOptions) (*audio.AudioResult, error) {
	duration := min(opts.Duration, sfxMaxDurationSeconds)

	body := map[string]any{
		"text":             opts.Prompt,
		"output_format":    "mp3_44100_128",
		"prompt_influence": 0.3,
	}
	if duration > 0 {
		body["duration_seconds"] = duration
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal sfx request: %w", err)
	}

	// Sound-generation is bursty — legacy code used a fixed 60s timeout, not
	// the per-call TimeoutMs. Preserve that exactly.
	audioBytes, err := p.c.postJSON(ctx, "/v1/sound-generation", jsonBody, 60*time.Second)
	if err != nil {
		return nil, fmt.Errorf("elevenlabs sfx: %w", err)
	}
	if len(audioBytes) == 0 {
		return nil, fmt.Errorf("empty audio response from ElevenLabs")
	}

	return &audio.AudioResult{
		Audio:     audioBytes,
		Extension: "mp3",
		MimeType:  "audio/mpeg",
		Provider:  "elevenlabs",
	}, nil
}
