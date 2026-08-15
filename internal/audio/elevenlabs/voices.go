package elevenlabs

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
)

// voicesResponse mirrors the ElevenLabs GET /v1/voices response envelope.
type voicesResponse struct {
	Voices []voiceJSON `json:"voices"`
}

type voiceJSON struct {
	VoiceID    string            `json:"voice_id"`
	Name       string            `json:"name"`
	Labels     map[string]string `json:"labels"`
	PreviewURL string            `json:"preview_url"`
	Category   string            `json:"category"`
}

// ListVoices fetches the caller's available voices from ElevenLabs
// GET /v1/voices with xi-api-key auth. Results are NOT cached here;
// caching lives in audio.VoiceCache.
func (p *TTSProvider) ListVoices(ctx context.Context) ([]audio.Voice, error) {
	raw, err := p.c.getJSON(ctx, "/v1/voices")
	if err != nil {
		return nil, fmt.Errorf("elevenlabs list voices: %w", err)
	}

	var resp voicesResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("elevenlabs list voices: parse response: %w", err)
	}

	voices := make([]audio.Voice, 0, len(resp.Voices))
	for _, v := range resp.Voices {
		labels := v.Labels
		if labels == nil {
			labels = make(map[string]string)
		}
		voices = append(voices, audio.Voice{
			ID:         v.VoiceID,
			Name:       v.Name,
			Labels:     labels,
			PreviewURL: v.PreviewURL,
			Category:   v.Category,
		})
	}
	return voices, nil
}
