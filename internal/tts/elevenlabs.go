package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ElevenLabsProvider implements TTS via the ElevenLabs API.
// Matching TS elevenLabsTTS() in src/tts/tts-core.ts.
type ElevenLabsProvider struct {
	apiKey    string
	baseURL   string
	voiceID   string // default "pMsXgVXv3BLzUgSXRplE"
	modelID   string // default "eleven_multilingual_v2"
	timeoutMs int
}

// ElevenLabsConfig configures the ElevenLabs TTS provider.
type ElevenLabsConfig struct {
	APIKey    string
	BaseURL   string
	VoiceID   string
	ModelID   string
	TimeoutMs int
}

// NewElevenLabsProvider creates an ElevenLabs TTS provider.
func NewElevenLabsProvider(cfg ElevenLabsConfig) *ElevenLabsProvider {
	p := &ElevenLabsProvider{
		apiKey:    cfg.APIKey,
		baseURL:   cfg.BaseURL,
		voiceID:   cfg.VoiceID,
		modelID:   cfg.ModelID,
		timeoutMs: cfg.TimeoutMs,
	}
	if p.baseURL == "" {
		p.baseURL = "https://api.elevenlabs.io"
	}
	if p.voiceID == "" {
		p.voiceID = "pMsXgVXv3BLzUgSXRplE"
	}
	if p.modelID == "" {
		p.modelID = "eleven_multilingual_v2"
	}
	if p.timeoutMs <= 0 {
		p.timeoutMs = 30000
	}
	return p
}

func (p *ElevenLabsProvider) Name() string { return "elevenlabs" }

// Synthesize calls the ElevenLabs text-to-speech endpoint.
// Matching TS: POST {baseUrl}/v1/text-to-speech/{voiceId}.
func (p *ElevenLabsProvider) Synthesize(ctx context.Context, text string, opts Options) (*SynthResult, error) {
	voiceID := opts.Voice
	if voiceID == "" {
		voiceID = p.voiceID
	}
	modelID := opts.Model
	if modelID == "" {
		modelID = p.modelID
	}

	// Determine output format
	outputFormat := "mp3_44100_128"
	ext := "mp3"
	mime := "audio/mpeg"
	if opts.Format == "opus" {
		outputFormat = "opus_48000_64"
		ext = "ogg"
		mime = "audio/ogg"
	}

	body := map[string]interface{}{
		"text":     text,
		"model_id": modelID,
		"voice_settings": map[string]interface{}{
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

	url := fmt.Sprintf("%s/v1/text-to-speech/%s?output_format=%s", p.baseURL, voiceID, outputFormat)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("create elevenlabs tts request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("xi-api-key", p.apiKey)

	client := &http.Client{Timeout: time.Duration(p.timeoutMs) * time.Millisecond}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("elevenlabs tts request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("elevenlabs tts error %d: %s", resp.StatusCode, string(errBody))
	}

	audio, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read elevenlabs tts response: %w", err)
	}

	return &SynthResult{
		Audio:     audio,
		Extension: ext,
		MimeType:  mime,
	}, nil
}
