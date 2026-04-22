package elevenlabs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

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

// reOutputFormat validates output_format query param: lowercase alphanumeric + underscore only.
// Finding #4: prevents HTTP Parameter Pollution / injection via user-controlled format string.
var reOutputFormat = regexp.MustCompile(`^[a-z0-9_]+$`)

// reLanguageCode validates language_code: BCP-47 subset (2–3 letter lang + optional region).
// Finding #4: prevents injection via user-controlled language code.
var reLanguageCode = regexp.MustCompile(`^[a-z]{2,3}(-[A-Z]{2})?$`)

// FormatMeta maps an ElevenLabs output_format string to the file extension and
// MIME type to use in SynthResult. All pcm_* variants use audio/pcm (single MIME
// per Finding #7 — no per-rate variants). Exported for white-box tests.
//
// Telegram opus contract (Finding #11): this function is NOT called when
// opts.Format=="opus"; that path hardcodes ext=ogg / mime=audio/ogg; codecs=opus.
func FormatMeta(format string) (ext, mime string) {
	switch {
	case strings.HasPrefix(format, "mp3_"):
		return "mp3", "audio/mpeg"
	case strings.HasPrefix(format, "opus_"):
		return "opus", "audio/opus"
	case strings.HasPrefix(format, "pcm_"):
		return "pcm", "audio/pcm"
	case strings.HasPrefix(format, "wav_"):
		return "wav", "audio/wav"
	case format == "ulaw_8000":
		return "ulaw", "audio/basic"
	case format == "alaw_8000":
		return "alaw", "audio/x-alaw"
	default:
		// Unknown format: fall back to mp3 so callers get a usable MIME type.
		return "mp3", "audio/mpeg"
	}
}

// Synthesize converts text to audio. Opts.Voice/Opts.Model override the
// configured defaults; Opts.Format="opus" switches to Ogg Opus output.
// opts.Params keys (nested dot-path):
//   - "voice_settings.stability"        float64  (default 0.5)
//   - "voice_settings.similarity_boost" float64  (default 0.75)
//   - "voice_settings.style"            float64  (default 0.0)
//   - "voice_settings.use_speaker_boost" bool    (default true)
//   - "voice_settings.speed"            float64  (default 1.0)
//   - "apply_text_normalization"        string   (default "auto")
//   - "seed"                            int      (default 0 = omitted)
//   - "optimize_streaming_latency"      int      (query param, default 0 = omitted)
//   - "language_code"                   string   (query param, default "" = omitted)
//   - "output_format"                   string   (query param, default "mp3_44100_128")
//
// MUST NOT mutate opts.Params — reads only.
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

	// Telegram opus contract (Finding #11): when opts.Format=="opus", force
	// ogg/Opus MIME regardless of any user-set output_format param. This
	// preserves Telegram voice-bubble compatibility — Telegram rejects audio/opus
	// MIME; user param affects upstream codec selection only.
	var ext, mime string
	var outputFormat string
	if opts.Format == "opus" {
		outputFormat = "opus_48000_64"
		ext = "ogg"
		mime = "audio/ogg; codecs=opus"
	} else {
		// Default output format; may be overridden by opts.Params below.
		outputFormat = "mp3_44100_128"
		ext = "mp3"
		mime = "audio/mpeg"

		// Honor user-set output_format (Finding #4: validate before use).
		if pf := resolveELString(opts.Params, "output_format", ""); pf != "" {
			if !reOutputFormat.MatchString(pf) {
				return nil, fmt.Errorf("elevenlabs: invalid output_format %q (must match ^[a-z0-9_]+$)", pf)
			}
			outputFormat = pf
			ext, mime = FormatMeta(outputFormat)
		}
	}

	// Build voice_settings from params, falling back to hardcoded defaults.
	// Defaults MUST match characterization fixture (stability=0.5, etc.).
	voiceSettings := map[string]any{
		"stability":         resolveELFloat(opts.Params, "voice_settings.stability", 0.5),
		"similarity_boost":  resolveELFloat(opts.Params, "voice_settings.similarity_boost", 0.75),
		"style":             resolveELFloat(opts.Params, "voice_settings.style", 0.0),
		"use_speaker_boost": resolveELBool(opts.Params, "voice_settings.use_speaker_boost", true),
		"speed":             resolveELFloat(opts.Params, "voice_settings.speed", 1.0),
	}

	body := map[string]any{
		"text":           text,
		"model_id":       modelID,
		"voice_settings": voiceSettings,
	}

	// Optional top-level params.
	if norm := resolveELString(opts.Params, "apply_text_normalization", ""); norm != "" {
		body["apply_text_normalization"] = norm
	}
	if seed := resolveELInt(opts.Params, "seed", -1); seed > 0 {
		body["seed"] = seed
	}

	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal elevenlabs tts request: %w", err)
	}

	// Build URL path with query params using url.Values to prevent injection
	// (Finding #4: no more raw fmt.Sprintf concatenation).
	q := url.Values{}
	q.Set("output_format", outputFormat)
	if latency := resolveELInt(opts.Params, "optimize_streaming_latency", -1); latency > 0 {
		q.Set("optimize_streaming_latency", fmt.Sprintf("%d", latency))
	}
	if lang := resolveELString(opts.Params, "language_code", ""); lang != "" {
		// Finding #4: validate language_code before appending to URL.
		if !reLanguageCode.MatchString(lang) {
			return nil, fmt.Errorf("elevenlabs: invalid language_code %q (must match ^[a-z]{2,3}(-[A-Z]{2})?$)", lang)
		}
		q.Set("language_code", lang)
	}
	path := "/v1/text-to-speech/" + voiceID + "?" + q.Encode()

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

// resolveELFloat reads a float param from params map via nested key, falling back to def.
func resolveELFloat(params map[string]any, key string, def float64) float64 {
	if params == nil {
		return def
	}
	v, ok := audio.GetNested(params, key)
	if !ok {
		return def
	}
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	}
	return def
}

// resolveELBool reads a bool param from params map via nested key, falling back to def.
func resolveELBool(params map[string]any, key string, def bool) bool {
	if params == nil {
		return def
	}
	v, ok := audio.GetNested(params, key)
	if !ok {
		return def
	}
	if b, ok := v.(bool); ok {
		return b
	}
	return def
}

// resolveELString reads a string param from params map via nested key, falling back to def.
func resolveELString(params map[string]any, key, def string) string {
	if params == nil {
		return def
	}
	v, ok := audio.GetNested(params, key)
	if !ok {
		return def
	}
	if s, ok := v.(string); ok {
		return s
	}
	return def
}

// resolveELInt reads an int param from params map via nested key.
// Returns def (-1 by convention for "not set") when absent.
func resolveELInt(params map[string]any, key string, def int) int {
	if params == nil {
		return def
	}
	v, ok := audio.GetNested(params, key)
	if !ok {
		return def
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return def
}
