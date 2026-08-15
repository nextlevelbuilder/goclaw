package openaicompat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
)

// errBodyLimit caps how much of an error response body is read into a message.
const errBodyLimit = 512

// TTSProvider synthesizes speech via an OpenAI-compatible POST /audio/speech.
type TTSProvider struct {
	cfg Config
}

// NewTTSProvider returns a TTS provider for the configured endpoint.
// Returns ErrAPIBaseRequired when cfg.APIBase is empty.
func NewTTSProvider(cfg Config) (*TTSProvider, error) {
	if strings.TrimSpace(cfg.APIBase) == "" {
		return nil, ErrAPIBaseRequired
	}
	return &TTSProvider{cfg: cfg.withDefaults()}, nil
}

// Name returns the stable provider identifier.
func (p *TTSProvider) Name() string { return providerName }

// Synthesize calls POST {api_base}/audio/speech and returns the audio bytes.
//
// Resolution order for voice, model and format is: per-request opts, then
// opts.Params, then the provider config. Voice and model are omitted from the
// request when they resolve empty — some engines pick the model from the voice,
// and sending an empty string would override that choice rather than defer to it.
func (p *TTSProvider) Synthesize(ctx context.Context, text string, opts audio.TTSOptions) (*audio.SynthResult, error) {
	format := firstNonEmpty(opts.Format, paramString(opts.Params, "response_format"), p.cfg.TTSFormat)
	voice := firstNonEmpty(opts.Voice, p.cfg.TTSVoice)
	model := firstNonEmpty(opts.Model, p.cfg.TTSModel)

	body := map[string]any{
		"input":           text,
		"response_format": format,
	}
	if voice != "" {
		body["voice"] = voice
	}
	if model != "" {
		body["model"] = model
	}
	if speed := paramFloat(opts.Params, "speed"); speed > 0 {
		body["speed"] = speed
	}

	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("openai_compat tts: marshal request: %w", err)
	}

	url := strings.TrimRight(p.cfg.APIBase, "/") + "/audio/speech"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("openai_compat tts: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if p.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	}

	hc := &http.Client{Timeout: time.Duration(p.cfg.TimeoutMs) * time.Millisecond}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai_compat tts: http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, errBodyLimit))
		return nil, fmt.Errorf("openai_compat tts: endpoint error %d: %s", resp.StatusCode, strings.TrimSpace(string(errBody)))
	}

	audioBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("openai_compat tts: read response: %w", err)
	}

	ext, mime := formatToExtMime(format)
	return &audio.SynthResult{
		Audio:     audioBytes,
		Extension: ext,
		MimeType:  mime,
	}, nil
}

// formatToExtMime maps an OpenAI response_format to a file extension and MIME
// type. Unknown formats are passed through as their own extension with a
// generic MIME type rather than guessed at.
func formatToExtMime(format string) (ext, mime string) {
	switch format {
	case "mp3":
		return "mp3", "audio/mpeg"
	case "opus":
		return "ogg", "audio/ogg"
	case "aac":
		return "aac", "audio/aac"
	case "flac":
		return "flac", "audio/flac"
	case "wav":
		return "wav", "audio/wav"
	case "pcm":
		return "pcm", "audio/pcm"
	default:
		return format, "application/octet-stream"
	}
}

// firstNonEmpty returns the first non-empty string, or "" if all are empty.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// paramString reads a string param, returning "" when absent or not a string.
func paramString(params map[string]any, key string) string {
	if params == nil {
		return ""
	}
	v, ok := audio.GetNested(params, key)
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// paramFloat reads a numeric param, returning 0 when absent or not numeric.
func paramFloat(params map[string]any, key string) float64 {
	if params == nil {
		return 0
	}
	v, ok := audio.GetNested(params, key)
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	default:
		return 0
	}
}
