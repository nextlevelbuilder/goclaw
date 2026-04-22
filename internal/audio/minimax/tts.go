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
	"net/http"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
)

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
func (p *Provider) Synthesize(ctx context.Context, text string, opts audio.TTSOptions) (*audio.SynthResult, error) {
	voiceID := opts.Voice
	if voiceID == "" {
		voiceID = p.voiceID
	}
	model := opts.Model
	if model == "" {
		model = p.model
	}

	audioFormat, ext, mime := "mp3", "mp3", "audio/mpeg"
	if opts.Format == "opus" || opts.Format == "pcm" || opts.Format == "flac" || opts.Format == "wav" {
		audioFormat = opts.Format
		switch opts.Format {
		case "pcm":
			ext, mime = "pcm", "audio/pcm"
		case "flac":
			ext, mime = "flac", "audio/flac"
		case "wav":
			ext, mime = "wav", "audio/wav"
		}
	}

	body := map[string]any{
		"text":   text,
		"model":  model,
		"stream": false,
		"voice_setting": map[string]any{
			"voice_id": voiceID,
			"speed":    1.0,
			"pitch":    0,
		},
		"audio_setting": map[string]any{
			"format": audioFormat,
		},
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
