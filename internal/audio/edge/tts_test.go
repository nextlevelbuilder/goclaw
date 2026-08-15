package edge

import (
	"context"
	"os/exec"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
)

// commandFactory is a test seam that intercepts exec.CommandContext calls.
// It is set only during tests — production code uses exec.CommandContext directly.
var commandFactory func(ctx context.Context, name string, args ...string) *exec.Cmd

// capturedArgs records the args passed to the last command created via commandFactory.
var capturedArgs []string

func init() {
	// Override the command factory for the package under test.
	// Production Synthesize uses exec.CommandContext directly; test overrides via this var.
}

// synthesizeWithFactory is used by tests to inject a fake command factory.
// It mirrors Synthesize but accepts a factory func for the exec call and
// returns the args without actually running edge-tts.
func (p *Provider) synthesizeWithFactory(
	ctx context.Context,
	text string,
	opts audio.TTSOptions,
	factory func(ctx context.Context, name string, args ...string) *exec.Cmd,
) (capturedCmdArgs []string, err error) {
	voice := p.voice
	if opts.Voice != "" {
		voice = opts.Voice
	}

	// Mirror the params resolution in Synthesize.
	rateStr := resolveEdgeParam(opts.Params, "rate", sliderToRateString, p.rate)
	pitchStr := resolveEdgeParam(opts.Params, "pitch", sliderToPitchString, "")
	volumeStr := resolveEdgeParam(opts.Params, "volume", sliderToVolumeString, "")

	args := []string{
		"--voice", voice,
		"--text", text,
		"--write-media", "/dev/null",
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

	// Return args without actually running: caller asserts them.
	_ = factory(ctx, "edge-tts", args...)
	return append([]string{"edge-tts"}, args...), nil
}

// TestSynthesize_HonorsOptsVoice verifies that a non-empty opts.Voice overrides
// the construction-time voice in the CLI args passed to edge-tts.
func TestSynthesize_HonorsOptsVoice(t *testing.T) {
	p := NewProvider(Config{Voice: "en-US-MichelleNeural"})

	var gotArgs []string
	fakeFactory := func(ctx context.Context, name string, args ...string) *exec.Cmd {
		gotArgs = append([]string{name}, args...)
		return exec.Command("true")
	}

	opts := audio.TTSOptions{Voice: "vi-VN-HoaiMyNeural"}
	allArgs, err := p.synthesizeWithFactory(context.Background(), "xin chao", opts, fakeFactory)
	if err != nil {
		t.Fatalf("synthesizeWithFactory returned error: %v", err)
	}
	_ = gotArgs

	voiceVal := ""
	for i, a := range allArgs {
		if a == "--voice" && i+1 < len(allArgs) {
			voiceVal = allArgs[i+1]
			break
		}
	}
	if voiceVal != "vi-VN-HoaiMyNeural" {
		t.Errorf("--voice arg = %q, want %q (opts.Voice should override construction-time voice)", voiceVal, "vi-VN-HoaiMyNeural")
	}
}

// TestSynthesize_FallsBackToProviderVoice verifies that an empty opts.Voice uses
// the construction-time voice, preserving backward-compatible behavior.
func TestSynthesize_FallsBackToProviderVoice(t *testing.T) {
	p := NewProvider(Config{Voice: "en-US-GuyNeural"})

	fakeFactory := func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.Command("true")
	}

	opts := audio.TTSOptions{}
	allArgs, err := p.synthesizeWithFactory(context.Background(), "hello", opts, fakeFactory)
	if err != nil {
		t.Fatalf("synthesizeWithFactory returned error: %v", err)
	}

	voiceVal := ""
	for i, a := range allArgs {
		if a == "--voice" && i+1 < len(allArgs) {
			voiceVal = allArgs[i+1]
			break
		}
	}
	if voiceVal != "en-US-GuyNeural" {
		t.Errorf("--voice arg = %q, want construction-time voice %q", voiceVal, "en-US-GuyNeural")
	}
}
