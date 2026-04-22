package elevenlabs

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
)

// musicDefaultTimeoutMs is the timeout for music generation — longer than TTS/SFX
// because generation can take 30-60+ seconds for full tracks.
const musicDefaultTimeoutMs = 300_000

// MusicProvider generates music via POST /v1/music.
// ElevenLabs Music API spec: prompt-driven, binary MP3 response.
type MusicProvider struct {
	cfg Config
	c   *client
}

// NewMusicProvider returns an ElevenLabs music provider.
func NewMusicProvider(cfg Config) *MusicProvider {
	cfg = cfg.withDefaults()
	timeoutMs := max(cfg.TimeoutMs, musicDefaultTimeoutMs)
	return &MusicProvider{
		cfg: cfg,
		c:   newClient(cfg.APIKey, cfg.BaseURL, timeoutMs),
	}
}

// Name returns the stable provider identifier used by the Manager.
func (p *MusicProvider) Name() string { return "elevenlabs" }

// GenerateMusic produces an MP3 from opts.Prompt via POST /v1/music.
// Duration is expressed in seconds; the API takes music_length_ms.
// Lyrics are appended to the prompt (ElevenLabs music API has no separate lyrics field).
func (p *MusicProvider) GenerateMusic(ctx context.Context, opts audio.MusicOptions) (*audio.AudioResult, error) {
	modelID := opts.Model
	if modelID == "" {
		modelID = "music_v1"
	}

	prompt := opts.Prompt
	if opts.Lyrics != "" {
		prompt = opts.Prompt + "\n\nLyrics:\n" + opts.Lyrics
	}

	body := map[string]any{
		"prompt":   prompt,
		"model_id": modelID,
	}
	if opts.Duration > 0 {
		body["music_length_ms"] = opts.Duration * 1000
	}
	if opts.Instrumental {
		body["force_instrumental"] = true
	}

	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal elevenlabs music request: %w", err)
	}

	// music_length_ms is only valid with prompt (not composition_plan) — our path always uses prompt.
	path := "/v1/music?output_format=mp3_44100_128"
	audioBytes, err := p.c.postJSON(ctx, path, bodyJSON, time.Duration(musicDefaultTimeoutMs)*time.Millisecond)
	if err != nil {
		return nil, fmt.Errorf("elevenlabs music: %w", err)
	}
	if len(audioBytes) == 0 {
		return nil, fmt.Errorf("empty audio response from ElevenLabs music")
	}

	return &audio.AudioResult{
		Audio:     audioBytes,
		Extension: "mp3",
		MimeType:  "audio/mpeg",
		Model:     modelID,
		Provider:  "elevenlabs",
	}, nil
}
