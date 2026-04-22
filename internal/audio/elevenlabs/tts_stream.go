package elevenlabs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
)

// SynthesizeStream streams ElevenLabs TTS audio via the /stream endpoint.
// Unlike Synthesize, the HTTP body is handed to the caller as an io.ReadCloser
// so bytes can be consumed as they arrive. Callers MUST Close the returned
// Audio reader.
//
// Resolution order for voice/model: opts > configured defaults.
// Model IDs are validated against AllowedElevenLabsModels before dispatch —
// unknown IDs surface an i18n-keyed error without hitting the network.
func (p *TTSProvider) SynthesizeStream(ctx context.Context, text string, opts audio.TTSOptions) (*audio.StreamResult, error) {
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
		return nil, fmt.Errorf("marshal elevenlabs tts stream request: %w", err)
	}

	path := fmt.Sprintf("/v1/text-to-speech/%s/stream?output_format=%s", voiceID, outputFormat)
	url := strings.TrimRight(p.c.baseURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("xi-api-key", p.c.apiKey)

	// No Client.Timeout — streaming may legitimately outlast a per-request
	// deadline. Cancellation is honoured via the request context.
	hc := &http.Client{Timeout: 0}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("elevenlabs tts stream: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("ElevenLabs API error %d: %s", resp.StatusCode, truncate(errBody, 500))
	}

	return &audio.StreamResult{
		Audio:     resp.Body,
		Extension: ext,
		MimeType:  mime,
	}, nil
}
