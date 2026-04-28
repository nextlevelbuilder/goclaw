package gemini

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
)

// DefaultTextPrefix is the inline style directive prepended to user text
// for every Gemini TTS single-voice request. Gemini TTS preview models do not
// accept systemInstruction; inline prefix is the ONLY supported style control.
// See research/researcher-01-gemini-tts-api.md Q1,Q3.
const DefaultTextPrefix = "Speak naturally: "

// StrongerTextPrefix is the retry prefix used after a 400 "text generation"
// response. Explicitly forbids translation/commentary to force TTS-only mode.
const StrongerTextPrefix = "Read the following text aloud without translating, commenting, or modifying: "

// BuildStyledText prepends prefix to text. Empty prefix returns text unchanged.
// Exported for retry logic that may use a stronger prefix (Phase 03).
func BuildStyledText(prefix, text string) string {
	if prefix == "" {
		return text
	}
	return prefix + text
}

// Config bundles credentials and TTS defaults for Google Gemini.
type Config struct {
	APIKey    string
	APIBase   string // custom endpoint (optional); must pass validateProviderURL
	Voice     string // default "Kore"
	Model     string // default "gemini-3.1-flash-tts-preview"
	TimeoutMs int    // default 120000
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

	// Phase 02 gating: multi-speaker keeps raw transcript; single-voice gets prefix.
	isSingleVoice := len(opts.Speakers) == 0

	// buildBody constructs the request JSON with the given style prefix.
	// Multi-speaker mode ignores prefix — raw transcript is passed unchanged.
	buildBody := func(prefix string) ([]byte, error) {
		sendText := text
		if isSingleVoice {
			sendText = BuildStyledText(prefix, text)
		}
		rb := map[string]any{
			"contents": []map[string]any{
				{"parts": []map[string]any{{"text": sendText}}},
			},
			"generationConfig": generationConfig,
		}
		return json.Marshal(rb)
	}

	bodyBytes, err := buildBody(DefaultTextPrefix)
	if err != nil {
		return nil, fmt.Errorf("gemini: marshal request: %w", err)
	}

	// Retry logic — two independent retry branches, mutually exclusive:
	//   1. errTransientNoAudio (200 OK, finishReason=OTHER): retry with SAME body.
	//   2. ErrTextOnlyResponse (400 text-only): retry with STRONGER prefix body (single-voice only).
	res, err := p.requestAudio(ctx, model, bodyBytes)
	if err != nil && errors.Is(err, errTransientNoAudio) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(retryBackoff):
		}
		res, err = p.requestAudio(ctx, model, bodyBytes) // SAME body
	} else if err != nil && errors.Is(err, ErrTextOnlyResponse) && isSingleVoice {
		// Multi-speaker + text-only → return sentinel unretried; caller decides.
		strongerBody, bErr := buildBody(StrongerTextPrefix)
		if bErr != nil {
			return nil, fmt.Errorf("gemini: marshal retry request: %w", bErr)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(retryBackoff):
		}
		res, err = p.requestAudio(ctx, model, strongerBody) // NEW body with stronger prefix
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
	if isTextOnlyError(status, respBytes) {
		snippet := string(respBytes)
		if len(snippet) > 200 {
			snippet = snippet[:200] + "…"
		}
		return nil, fmt.Errorf("%w: %s", ErrTextOnlyResponse, snippet)
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

// isTextOnlyError returns true when the response is an HTTP 400 whose body
// suggests the model returned text instead of audio. Case-insensitive
// substring match on known Gemini error phrasings. Needles are kept narrow to
// avoid false positives on unrelated "generate text" errors.
func isTextOnlyError(status int, body []byte) bool {
	if status != http.StatusBadRequest || len(body) == 0 {
		return false
	}
	lower := strings.ToLower(string(body))
	for _, needle := range []string{
		"model tried to generate text", // exact phrase from user bug report
		"returned text",                // "returned text when audio was expected"
		"text instead of audio",
		"text-only",
		"text output",
	} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

// isTransientFinishReason reports whether a Gemini finishReason represents a
// non-deterministic failure that's worth retrying. OTHER is the catch-all the
// preview TTS endpoint emits when it just fails to produce audio for no
// user-visible reason — single retry usually succeeds.
func isTransientFinishReason(reason string) bool {
	return reason == "OTHER"
}
