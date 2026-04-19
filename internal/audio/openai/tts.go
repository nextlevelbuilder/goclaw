// Package openai implements the OpenAI audio/speech API for TTS.
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
)

// Config bundles credentials + TTS defaults for OpenAI.
type Config struct {
	APIKey    string
	APIBase   string // default "https://api.openai.com/v1"
	Model     string // default "gpt-4o-mini-tts"
	Voice     string // default "alloy"
	TimeoutMs int    // default 30000
}

// Provider implements audio.TTSProvider for OpenAI.
type Provider struct {
	apiKey    string
	apiBase   string
	model     string
	voice     string
	timeoutMs int
}

// NewProvider constructs an OpenAI TTS provider with defaults applied.
func NewProvider(cfg Config) *Provider {
	p := &Provider{
		apiKey:    cfg.APIKey,
		apiBase:   cfg.APIBase,
		model:     cfg.Model,
		voice:     cfg.Voice,
		timeoutMs: cfg.TimeoutMs,
	}
	if p.apiBase == "" {
		p.apiBase = "https://api.openai.com/v1"
	}
	if p.model == "" {
		p.model = "gpt-4o-mini-tts"
	}
	if p.voice == "" {
		p.voice = "alloy"
	}
	if p.timeoutMs <= 0 {
		p.timeoutMs = 30000
	}
	return p
}

// Name returns the stable provider identifier used by the Manager.
func (p *Provider) Name() string { return "openai" }

// Synthesize calls POST {apiBase}/audio/speech.
func (p *Provider) Synthesize(ctx context.Context, text string, opts audio.TTSOptions) (*audio.SynthResult, error) {
	voice := opts.Voice
	if voice == "" {
		voice = p.voice
	}
	model := opts.Model
	if model == "" {
		model = p.model
	}
	format := opts.Format
	if format == "" {
		format = "mp3"
	}

	body := map[string]any{
		"model":           model,
		"input":           text,
		"voice":           voice,
		"response_format": format,
	}
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal openai tts request: %w", err)
	}

	url := p.apiBase + "/audio/speech"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("create openai tts request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	hc := &http.Client{Timeout: time.Duration(p.timeoutMs) * time.Millisecond}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai tts request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai tts error %d: %s", resp.StatusCode, string(errBody))
	}
	audioBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read openai tts response: %w", err)
	}

	ext, mime := format, "audio/mpeg"
	switch format {
	case "opus":
		ext, mime = "ogg", "audio/ogg"
	case "mp3":
		mime = "audio/mpeg"
	}

	return &audio.SynthResult{
		Audio:     audioBytes,
		Extension: ext,
		MimeType:  mime,
	}, nil
}
