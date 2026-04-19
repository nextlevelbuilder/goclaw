// Package edge implements TTS via the Microsoft Edge TTS CLI (free, no API key).
// Requires the `edge-tts` Python CLI: `pip install edge-tts`.
package edge

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
)

// Config configures the Edge TTS provider.
type Config struct {
	Voice     string // default "en-US-MichelleNeural"
	Rate      string // speech rate, e.g. "+0%"
	TimeoutMs int
}

// Provider implements audio.TTSProvider via the edge-tts CLI.
type Provider struct {
	voice     string
	rate      string
	timeoutMs int
}

// NewProvider returns an Edge TTS provider with defaults applied.
func NewProvider(cfg Config) *Provider {
	p := &Provider{
		voice:     cfg.Voice,
		rate:      cfg.Rate,
		timeoutMs: cfg.TimeoutMs,
	}
	if p.voice == "" {
		p.voice = "en-US-MichelleNeural"
	}
	if p.timeoutMs <= 0 {
		p.timeoutMs = 30000
	}
	return p
}

// Name returns the stable provider identifier used by the Manager.
func (p *Provider) Name() string { return "edge" }

// Synthesize shells out to edge-tts. Output is always MP3
// (edge-tts default format: audio-24khz-48kbitrate-mono-mp3).
// opts.Voice overrides the construction-time voice when non-empty.
func (p *Provider) Synthesize(ctx context.Context, text string, opts audio.TTSOptions) (*audio.SynthResult, error) {
	tmpDir := os.TempDir()
	outPath := filepath.Join(tmpDir, fmt.Sprintf("tts-%d.mp3", time.Now().UnixNano()))
	defer os.Remove(outPath)

	voice := p.voice
	if opts.Voice != "" {
		voice = opts.Voice
	}

	args := []string{
		"--voice", voice,
		"--text", text,
		"--write-media", outPath,
	}
	if p.rate != "" {
		args = append(args, "--rate", p.rate)
	}

	timeout := time.Duration(p.timeoutMs) * time.Millisecond
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "edge-tts", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("edge-tts failed: %w (output: %s)", err, string(output))
	}

	audioBytes, err := os.ReadFile(outPath)
	if err != nil {
		return nil, fmt.Errorf("read edge-tts output: %w", err)
	}

	return &audio.SynthResult{
		Audio:     audioBytes,
		Extension: "mp3",
		MimeType:  "audio/mpeg",
	}, nil
}
