package edge

import (
	"context"
	"os/exec"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
)

// captureEdgeArgsWithOpts is a test helper that captures CLI args via synthesizeWithFactory.
func captureEdgeArgsWithOpts(t *testing.T, p *Provider, opts audio.TTSOptions) []string {
	t.Helper()
	factory := func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.Command("true")
	}
	args, err := p.synthesizeWithFactory(context.Background(), "test text", opts, factory)
	if err != nil {
		t.Fatalf("synthesizeWithFactory: %v", err)
	}
	return args
}

func TestSynthesize_AppliesParams_Rate(t *testing.T) {
	p := NewProvider(Config{})
	args := captureEdgeArgsWithOpts(t, p, audio.TTSOptions{
		Params: map[string]any{"rate": 15},
	})
	assertArg(t, args, "--rate", "+15%")
}

func TestSynthesize_AppliesParams_NegativeRate(t *testing.T) {
	p := NewProvider(Config{})
	args := captureEdgeArgsWithOpts(t, p, audio.TTSOptions{
		Params: map[string]any{"rate": -25},
	})
	assertArg(t, args, "--rate", "-25%")
}

func TestSynthesize_AppliesParams_Pitch(t *testing.T) {
	p := NewProvider(Config{})
	args := captureEdgeArgsWithOpts(t, p, audio.TTSOptions{
		Params: map[string]any{"pitch": 5},
	})
	assertArg(t, args, "--pitch", "+5Hz")
}

func TestSynthesize_AppliesParams_Volume(t *testing.T) {
	p := NewProvider(Config{})
	args := captureEdgeArgsWithOpts(t, p, audio.TTSOptions{
		Params: map[string]any{"volume": 20},
	})
	assertArg(t, args, "--volume", "+20%")
}

func TestSynthesize_AppliesParams_OmitsZeroRate(t *testing.T) {
	// Zero rate/pitch/volume must NOT produce flags (characterization invariant).
	p := NewProvider(Config{})
	args := captureEdgeArgsWithOpts(t, p, audio.TTSOptions{
		Params: map[string]any{"rate": 0, "pitch": 0, "volume": 0},
	})
	for _, flag := range []string{"--rate", "--pitch", "--volume"} {
		if hasFlag(args, flag) {
			t.Errorf("%s flag must not appear for zero value", flag)
		}
	}
}

func TestSynthesize_AppliesParams_NilParamsNoFlags(t *testing.T) {
	p := NewProvider(Config{})
	args := captureEdgeArgsWithOpts(t, p, audio.TTSOptions{})
	for _, flag := range []string{"--rate", "--pitch", "--volume"} {
		if hasFlag(args, flag) {
			t.Errorf("%s flag must not appear for nil params", flag)
		}
	}
}

func TestSynthesize_DoesNotMutateCallerParams(t *testing.T) {
	p := NewProvider(Config{})
	original := map[string]any{
		"rate":     10,
		"sentinel": "untouched",
	}
	snapshot := map[string]any{
		"rate":     original["rate"],
		"sentinel": original["sentinel"],
	}
	captureEdgeArgsWithOpts(t, p, audio.TTSOptions{Params: original})
	for k, want := range snapshot {
		if got := original[k]; got != want {
			t.Errorf("caller Params mutated: key %q was %v, now %v", k, want, got)
		}
	}
}

func TestCapabilities_HasParam_Rate(t *testing.T) {
	p := NewProvider(Config{})
	caps := p.Capabilities()
	for _, param := range caps.Params {
		if param.Key == "rate" && param.Type == audio.ParamTypeInteger {
			return
		}
	}
	t.Error("rate param (integer) not found in Capabilities")
}

func TestCapabilities_HasParam_Pitch(t *testing.T) {
	p := NewProvider(Config{})
	caps := p.Capabilities()
	for _, param := range caps.Params {
		if param.Key == "pitch" && param.Type == audio.ParamTypeInteger {
			return
		}
	}
	t.Error("pitch param (integer) not found in Capabilities")
}
