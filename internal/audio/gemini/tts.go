package gemini

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
)

// Config bundles credentials and TTS defaults for Google Gemini.
type Config struct {
	APIKey    string
	APIBase   string // custom endpoint (optional); must pass validateProviderURL
	Voice     string // default "Kore"
	Model     string // default "gemini-2.5-flash-preview-tts"
	TimeoutMs int    // default 30000
}

// Provider implements audio.TTSProvider and audio.DescribableProvider for Gemini.
type Provider struct {
	cfg Config
	c   *client
}

// NewProvider constructs a Gemini TTS provider with defaults applied.
func NewProvider(cfg Config) *Provider {
	if cfg.Voice == "" {
		cfg.Voice = defaultVoice
	}
	if cfg.Model == "" {
		cfg.Model = defaultModel
	}
	return &Provider{
		cfg: cfg,
		c:   newClient(cfg.APIKey, cfg.APIBase, cfg.TimeoutMs),
	}
}

// Name returns the stable provider identifier.
func (p *Provider) Name() string { return "gemini" }

// Synthesize converts text to WAV audio via the Gemini generateContent API.
// When opts.Speakers is non-empty, multi-speaker mode is used; otherwise single-voice.
// MUST NOT mutate opts.Params or opts.Speakers — reads only.
func (p *Provider) Synthesize(ctx context.Context, text string, opts audio.TTSOptions) (*audio.SynthResult, error) {
	voice := opts.Voice
	if voice == "" {
		voice = p.cfg.Voice
	}
	model := opts.Model
	if model == "" {
		model = p.cfg.Model
	}

	// Validate model.
	if !isValidModel(model) {
		return nil, fmt.Errorf("%w: %s", ErrInvalidModel, model)
	}

	// Build speechConfig — multi-speaker and single-voice are mutually exclusive.
	var speechConfig map[string]any
	if len(opts.Speakers) > 0 {
		if len(opts.Speakers) > 2 {
			return nil, fmt.Errorf("%w: requested %d", ErrSpeakerLimit, len(opts.Speakers))
		}
		// Defensive copy to satisfy no-mutate contract.
		speakers := make([]audio.SpeakerVoice, len(opts.Speakers))
		copy(speakers, opts.Speakers)

		configs := make([]map[string]any, len(speakers))
		for i, s := range speakers {
			configs[i] = map[string]any{
				"speaker": s.Speaker,
				"voiceConfig": map[string]any{
					"prebuiltVoiceConfig": map[string]any{
						"voiceName": s.VoiceID,
					},
				},
			}
		}
		speechConfig = map[string]any{
			"multiSpeakerVoiceConfig": map[string]any{
				"speakerVoiceConfigs": configs,
			},
		}
	} else {
		// Validate voice against static catalog (non-empty voice only — empty falls back to default).
		if voice != "" && !isValidVoice(voice) {
			return nil, fmt.Errorf("%w: %s", ErrInvalidVoice, voice)
		}
		speechConfig = map[string]any{
			"voiceConfig": map[string]any{
				"prebuiltVoiceConfig": map[string]any{
					"voiceName": voice,
				},
			},
		}
	}

	// Per Gemini generateContent spec, speechConfig is NESTED under
	// generationConfig — not a top-level field. Sending it at root returns
	// 400 "Unknown name 'speechConfig': Cannot find field."
	generationConfig := map[string]any{
		"responseModalities": []string{"AUDIO"},
		"speechConfig":       speechConfig,
	}
	// Merge optional params into generationConfig — only when explicitly present
	// in opts.Params (nil default = omit from body, preserving characterization).
	if temp, ok := resolveGeminiFloatExplicit(opts.Params, "temperature"); ok {
		generationConfig["temperature"] = temp
	}
	if seed, ok := resolveGeminiIntExplicit(opts.Params, "seed"); ok {
		generationConfig["seed"] = seed
	}
	if pp, ok := resolveGeminiFloatExplicit(opts.Params, "presencePenalty"); ok {
		generationConfig["presencePenalty"] = pp
	}
	if fp, ok := resolveGeminiFloatExplicit(opts.Params, "frequencyPenalty"); ok {
		generationConfig["frequencyPenalty"] = fp
	}

	reqBody := map[string]any{
		"contents": []map[string]any{
			{"parts": []map[string]any{{"text": text}}},
		},
		"generationConfig": generationConfig,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("gemini: marshal request: %w", err)
	}

	// Single retry on transient no-audio responses (finishReason=OTHER) — the
	// preview TTS endpoint is flaky and usually succeeds on the second try.
	// Anything else (auth, rate limit, safety, invalid model) is returned as-is.
	res, err := p.requestAudio(ctx, model, bodyBytes)
	if err != nil && errors.Is(err, errTransientNoAudio) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(retryBackoff):
		}
		res, err = p.requestAudio(ctx, model, bodyBytes)
	}
	return res, err
}

// retryBackoff is the delay before retrying a transient Gemini failure. Short
// because the user is waiting in the UI and the preview API usually succeeds
// immediately on retry.
const retryBackoff = 500 * time.Millisecond

// requestAudio executes one POST + parse cycle. Returns errTransientNoAudio
// (wrapped with a descriptive message) when finishReason indicates a flaky
// no-audio outcome that the caller may want to retry.
func (p *Provider) requestAudio(ctx context.Context, model string, bodyBytes []byte) (*audio.SynthResult, error) {
	respBytes, status, err := p.c.post(ctx, model, bodyBytes)
	if err != nil {
		return nil, err
	}

	switch status {
	case http.StatusUnauthorized:
		return nil, fmt.Errorf("gemini: auth error (401) — check api key")
	case http.StatusTooManyRequests:
		return nil, fmt.Errorf("gemini: rate limit exceeded (429)")
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("gemini: unexpected status %d: %s", status, string(respBytes))
	}

	// Parse response: candidates[0].content.parts[0].inlineData.data (base64 PCM).
	// Also parse finishReason + promptFeedback so we can surface a useful error
	// when audio is missing (safety filter, prompt block, text-only response, etc).
	var apiResp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text       string `json:"text,omitempty"`
					InlineData *struct {
						Data     string `json:"data"`
						MimeType string `json:"mimeType,omitempty"`
					} `json:"inlineData,omitempty"`
				} `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason,omitempty"`
		} `json:"candidates"`
		PromptFeedback *struct {
			BlockReason string `json:"blockReason,omitempty"`
		} `json:"promptFeedback,omitempty"`
	}
	if err := json.Unmarshal(respBytes, &apiResp); err != nil {
		return nil, fmt.Errorf("gemini: parse response: %w", err)
	}

	// Find the first inlineData across candidates/parts (some responses lead
	// with a text part before the audio part).
	var b64, textHint, finishReason string
	for _, c := range apiResp.Candidates {
		if c.FinishReason != "" && finishReason == "" {
			finishReason = c.FinishReason
		}
		for _, part := range c.Content.Parts {
			if part.InlineData != nil && part.InlineData.Data != "" {
				b64 = part.InlineData.Data
				break
			}
			if part.Text != "" && textHint == "" {
				textHint = part.Text
			}
		}
		if b64 != "" {
			break
		}
	}

	if b64 == "" {
		// Diagnose why audio is missing — give the caller something actionable.
		switch {
		case apiResp.PromptFeedback != nil && apiResp.PromptFeedback.BlockReason != "":
			return nil, fmt.Errorf("gemini: prompt blocked (%s)", apiResp.PromptFeedback.BlockReason)
		case isTransientFinishReason(finishReason):
			return nil, fmt.Errorf("%w: finishReason=%s", errTransientNoAudio, finishReason)
		case finishReason != "" && finishReason != "STOP":
			return nil, fmt.Errorf("gemini: synthesis stopped (%s) — model returned no audio", finishReason)
		case textHint != "":
			snippet := textHint
			if len(snippet) > 200 {
				snippet = snippet[:200] + "…"
			}
			return nil, fmt.Errorf("gemini: model returned text instead of audio (model may not support TTS or input was misinterpreted): %s", snippet)
		default:
			return nil, fmt.Errorf("gemini: response missing inlineData")
		}
	}

	pcm, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("gemini: base64 decode error: %w", err)
	}

	wav := Wrap(pcm)
	return &audio.SynthResult{
		Audio:     wav,
		Extension: "wav",
		MimeType:  "audio/wav",
	}, nil
}

// resolveGeminiFloatExplicit returns (value, true) only when the key is explicitly
// present in opts.Params, so callers can omit the generationConfig field entirely
// when not set. Mirrors resolveMiniMaxBoolExplicit in minimax/tts.go.
func resolveGeminiFloatExplicit(params map[string]any, key string) (float64, bool) {
	if params == nil {
		return 0, false
	}
	v, ok := audio.GetNested(params, key)
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	}
	return 0, false
}

// resolveGeminiIntExplicit returns (value, true) only when the key is explicitly
// present in opts.Params as an integer-compatible type.
func resolveGeminiIntExplicit(params map[string]any, key string) (int, bool) {
	if params == nil {
		return 0, false
	}
	v, ok := audio.GetNested(params, key)
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	}
	return 0, false
}

// isTransientFinishReason reports whether a Gemini finishReason represents a
// non-deterministic failure that's worth retrying. OTHER is the catch-all the
// preview TTS endpoint emits when it just fails to produce audio for no
// user-visible reason — single retry usually succeeds.
func isTransientFinishReason(reason string) bool {
	return reason == "OTHER"
}
