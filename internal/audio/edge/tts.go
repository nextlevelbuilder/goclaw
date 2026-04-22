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
	Rate      string // speech rate, e.g. "+0%"; overridden by opts.Params["rate"]
	TimeoutMs int
}

// Provider implements audio.TTSProvider via the edge-tts CLI.
type Provider struct {
	voice     string
	rate      string // legacy config-level rate string; opts.Params["rate"] takes precedence
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
// opts.Params keys (integer slider values):
//   - "rate"   int (-50..+100) → "--rate +N%"
//   - "pitch"  int (-50..+50)  → "--pitch +NHz"
//   - "volume" int (-50..+100) → "--volume +N%"
//
// Zero value for rate/pitch/volume means no flag is added (preserves legacy behaviour).
// MUST NOT mutate opts.Params.
func (p *Provider) Synthesize(ctx context.Context, text string, opts audio.TTSOptions) (*audio.SynthResult, error) {
	tmpDir := os.TempDir()
	outPath := filepath.Join(tmpDir, fmt.Sprintf("tts-%d.mp3", time.Now().UnixNano()))
	defer os.Remove(outPath)

	voice := p.voice
	if opts.Voice != "" {
		voice = opts.Voice
	}

	// Resolve rate/pitch/volume from opts.Params (int slider).
	// When Params is nil or key absent, fall back to legacy p.rate string if set.
	rateStr := resolveEdgeParam(opts.Params, "rate", sliderToRateString, p.rate)
	pitchStr := resolveEdgeParam(opts.Params, "pitch", sliderToPitchString, "")
	volumeStr := resolveEdgeParam(opts.Params, "volume", sliderToVolumeString, "")

	args := []string{
		"--voice", voice,
		"--text", text,
		"--write-media", outPath,
	}
	if rateStr != "" && rateStr != "+0%" {
		args = append(args, "--rate", rateStr)
	}
	if pitchStr != "" && pitchStr != "+0Hz" {
		args = append(args, "--pitch", pitchStr)
	}
	if volumeStr != "" && volumeStr != "+0%" {
		args = append(args, "--volume", volumeStr)
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

// resolveEdgeParam reads an integer slider value from params and converts it
// to the edge-tts string format using conv. Falls back to legacyStr when the
// key is absent from params or params is nil. Returns "" when both sources
// are absent.
func resolveEdgeParam(params map[string]any, key string, conv func(int) string, legacyStr string) string {
	if params != nil {
		if v, ok := audio.GetNested(params, key); ok {
			n := toInt(v)
			return conv(n)
		}
	}
	return legacyStr
}

// toInt converts a numeric any value to int. Returns 0 for unknown types.
func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case float32:
		return int(n)
	}
	return 0
}
