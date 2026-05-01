package edge

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
)

// captureEdgeArgs runs synthesizeWithFactory and returns the CLI args.
func captureEdgeArgs(t *testing.T, p *Provider, opts audio.TTSOptions) []string {
	t.Helper()
	fakeFactory := func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.Command("true")
	}
	args, err := p.synthesizeWithFactory(context.Background(), "hello", opts, fakeFactory)
	if err != nil {
		t.Fatalf("synthesizeWithFactory failed: %v", err)
	}
	return args
}

// TestDefaults_PreserveLegacyArgs verifies that populating opts.Params with all
// Capabilities defaults produces identical CLI args to the nil-Params baseline.
func TestDefaults_PreserveLegacyArgs(t *testing.T) {
	p := NewProvider(Config{})
	caps := p.Capabilities()

	if len(caps.Params) == 0 {
		t.Skip("Capabilities.Params not yet populated (Phase C enrichment pending)")
	}

	params := make(map[string]any)
	for _, s := range caps.Params {
		if s.Default != nil {
			audio.SetNested(params, s.Key, s.Default)
		}
	}

	argsWithDefaults := captureEdgeArgs(t, p, audio.TTSOptions{Params: params})
	argsNilParams := captureEdgeArgs(t, p, audio.TTSOptions{})

	// Compare as joined strings for readable diffs.
	want := joinArgs(argsNilParams)
	got := joinArgs(argsWithDefaults)
	if got != want {
		t.Errorf("defaults-invariant FAILED:\n  with-defaults: %s\n  nil-params:    %s", got, want)
	}
}

func joinArgs(args []string) string {
	var result strings.Builder
	for i, a := range args {
		if i > 0 {
			result.WriteString(" ")
		}
		result.WriteString(a)
	}
	return result.String()
}
