// Package minimax implements TTS via the MiniMax T2A v2 API.
// Docs: https://platform.minimax.io/docs/api-reference/speech-t2a-intro
package minimax

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
)

// pronunciationDictMaxBytes is the maximum accepted byte length for the
// pronunciation_dict param value. Finding #6: cap at 8 KB to prevent DoS.
const pronunciationDictMaxBytes = 8 * 1024

// Config configures the MiniMax TTS provider.
type Config struct {
	APIKey    string
	GroupID   string // required query param
	APIBase   string // default "https://api.minimax.io/v1"
	Model     string // default "speech-02-hd"
	VoiceID   string // default "Wise_Woman"
	TimeoutMs int
}

// Provider implements audio.TTSProvider via MiniMax /t2a_v2.
type Provider struct {
	apiKey    string
	groupID   string
	apiBase   string
	model     string
	voiceID   string
	timeoutMs int
}

// NewProvider returns a MiniMax TTS provider with defaults applied.
func NewProvider(cfg Config) *Provider {
	p := &Provider{
		apiKey:    cfg.APIKey,
		groupID:   cfg.GroupID,
		apiBase:   cfg.APIBase,
		model:     cfg.Model,
		voiceID:   cfg.VoiceID,
		timeoutMs: cfg.TimeoutMs,
	}
	if p.apiBase == "" {
		p.apiBase = "https://api.minimax.io/v1"
	}
	if p.model == "" {
		p.model = "speech-02-hd"
	}
	if p.voiceID == "" {
		p.voiceID = "Wise_Woman"
	}
	if p.timeoutMs <= 0 {
		p.timeoutMs = 30000
	}
	return p
}

// Name returns the stable provider identifier used by the Manager.
func (p *Provider) Name() string { return "minimax" }

// Synthesize calls MiniMax t2a_v2 (non-streaming). MiniMax returns hex-encoded
// audio in response.data.audio — we decode before returning.
//
// opts.Params keys (nested dot-path):
//   - "speed"               float64  (0.5–2.0, default 1.0)
//   - "vol"                 float64  (0.01–10.0, default 1.0; omitted from body at default)
//   - "pitch"               int      (-12..12, default 0)
//   - "emotion"             string   (optional)
//   - "text_normalization"  bool     (optional)
//   - "audio.format"        string   (mp3/pcm/flac/wav, default "mp3")
//   - "audio.sample_rate"   string   (optional)
//   - "audio.bitrate"       string   (optional, mp3 only)
//   - "audio.channel"       string   (optional)
//   - "language_boost"      string   (optional, e.g. "Chinese", "English")
//   - "subtitle_enable"     bool     (optional)
//   - "pronunciation_dict"  string   (optional JSON array, max 8 KB; Finding #6)
//
// MUST NOT mutate opts.Params — reads only.
func (p *Provider) Synthesize(ctx context.Context, text string, opts audio.TTSOptions) (*audio.SynthResult, error) {
	voiceID := opts.Voice
	if voiceID == "" {
		voiceID = p.voiceID
	}
	model := opts.Model
	if model == "" {
		model = p.model
	}

	// Resolve audio format — affects both MIME type and audio_setting.format.
	audioFormat := resolveMiniMaxString(opts.Params, "audio.format", "")
	if audioFormat == "" {
		// Fall back to opts.Format (legacy path) then default mp3.
		audioFormat = opts.Format
	}
	if audioFormat == "" || (audioFormat != "pcm" && audioFormat != "flac" && audioFormat != "wav") {
		audioFormat = "mp3"
	}
	ext, mime := audioFormat, "audio/mpeg"
	switch audioFormat {
	case "pcm":
		ext, mime = "pcm", "audio/pcm"
	case "flac":
		ext, mime = "flac", "audio/flac"
	case "wav":
		ext, mime = "wav", "audio/wav"
	}

	// Build voice_setting from params with defaults matching characterization.
	speed := resolveMiniMaxFloat(opts.Params, "speed", 1.0)
	pitch := resolveMiniMaxInt(opts.Params, "pitch", 0)

	voiceSetting := map[string]any{
		"voice_id": voiceID,
		"speed":    speed,
		"pitch":    pitch,
	}

	// vol only added when explicitly set and not the default (preserves nil-params body shape).
	if vol := resolveMiniMaxFloat(opts.Params, "vol", -1); vol > 0 && vol != 1.0 {
		voiceSetting["vol"] = vol
	}

	// emotion only added when set.
	if emotion := resolveMiniMaxString(opts.Params, "emotion", ""); emotion != "" {
		voiceSetting["emotion"] = emotion
	}

	// Build audio_setting.
	audioSetting := map[string]any{
		"format": audioFormat,
	}
	if sr := resolveMiniMaxString(opts.Params, "audio.sample_rate", ""); sr != "" {
		audioSetting["sample_rate"] = sr
	}
	if br := resolveMiniMaxString(opts.Params, "audio.bitrate", ""); br != "" {
		audioSetting["bitrate"] = br
	}
	if ch := resolveMiniMaxString(opts.Params, "audio.channel", ""); ch != "" {
		audioSetting["channel"] = ch
	}

	body := map[string]any{
		"text":          text,
		"model":         model,
		"stream":        false,
		"voice_setting": voiceSetting,
		"audio_setting": audioSetting,
	}

	// text_normalization only when explicitly set.
	if tn, ok := resolveMiniMaxBoolExplicit(opts.Params, "text_normalization"); ok {
		body["text_normalization"] = tn
	}

	// language_boost: top-level string hint (omit when empty).
	if lb := resolveMiniMaxString(opts.Params, "language_boost", ""); lb != "" {
		body["language_boost"] = lb
	}

	// subtitle_enable: only when explicitly set.
	if se, ok := resolveMiniMaxBoolExplicit(opts.Params, "subtitle_enable"); ok {
		body["subtitle_enable"] = se
	}

	// pronunciation_dict: parse user-pasted JSON array, wrap in {"tone":[...]}.
	// Finding #6: cap at 8 KB; never log raw value on parse failure — log length only.
	if pdRaw := resolveMiniMaxString(opts.Params, "pronunciation_dict", ""); pdRaw != "" {
		if len(pdRaw) > pronunciationDictMaxBytes {
			slog.Warn("minimax: pronunciation_dict exceeds 8 KB limit, omitting",
				"length", len(pdRaw))
		} else {
			var rules []string
			if err := json.Unmarshal([]byte(pdRaw), &rules); err != nil {
				slog.Warn("minimax: invalid pronunciation_dict JSON, omitting",
					"length", len(pdRaw))
			} else if len(rules) > 0 {
				body["pronunciation_dict"] = map[string]any{"tone": rules}
			}
		}
	}

	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal minimax tts request: %w", err)
	}

	url := fmt.Sprintf("%s/t2a_v2?GroupId=%s", p.apiBase, p.groupID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("create minimax tts request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	hc := &http.Client{Timeout: time.Duration(p.timeoutMs) * time.Millisecond}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("minimax tts request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read minimax tts response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("minimax tts error %d: %s", resp.StatusCode, string(respBody))
	}

	var apiResp miniMaxResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("parse minimax tts response: %w", err)
	}
	if apiResp.BaseResp.StatusCode != 0 {
		return nil, fmt.Errorf("minimax tts api error %d: %s", apiResp.BaseResp.StatusCode, apiResp.BaseResp.StatusMsg)
	}
	if apiResp.Data.Audio == "" {
		return nil, fmt.Errorf("minimax tts returned empty audio")
	}

	audioBytes, err := hex.DecodeString(apiResp.Data.Audio)
	if err != nil {
		return nil, fmt.Errorf("decode minimax tts audio hex: %w", err)
	}

	return &audio.SynthResult{
		Audio:     audioBytes,
		Extension: ext,
		MimeType:  mime,
	}, nil
}

type miniMaxResponse struct {
	BaseResp struct {
		StatusCode int    `json:"status_code"`
		StatusMsg  string `json:"status_msg"`
	} `json:"base_resp"`
	Data struct {
		Audio string `json:"audio"` // hex-encoded audio bytes
	} `json:"data"`
}

func resolveMiniMaxFloat(params map[string]any, key string, def float64) float64 {
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

func resolveMiniMaxInt(params map[string]any, key string, def int) int {
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

func resolveMiniMaxString(params map[string]any, key, def string) string {
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

// resolveMiniMaxBoolExplicit returns (value, true) only when the key is explicitly
// present in params, so callers can omit the field entirely when not set.
func resolveMiniMaxBoolExplicit(params map[string]any, key string) (bool, bool) {
	if params == nil {
		return false, false
	}
	v, ok := audio.GetNested(params, key)
	if !ok {
		return false, false
	}
	b, ok := v.(bool)
	return b, ok
}
