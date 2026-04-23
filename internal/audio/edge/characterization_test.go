package edge

import (
	"context"
	"os/exec"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
)

// TestCharacterization_Edge_DefaultOpts captures the CLI args emitted by
// Synthesize for default opts. This is the golden-fixture test — the exact
// arg shape MUST remain identical after the Synthesize refactor.
//
// Edge TTS has no HTTP body to capture; the "wire format" is the subprocess
// args passed to edge-tts. The characterization fixture is:
//   --voice en-US-MichelleNeural --text <text> --write-media <path>
//   (no --rate flag when rate is empty/zero-default)
func TestCharacterization_Edge_DefaultOpts(t *testing.T) {
	p := NewProvider(Config{}) // empty = defaults

	fakeFactory := func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.Command("true")
	}

	allArgs, err := p.synthesizeWithFactory(context.Background(), "hello world", audio.TTSOptions{}, fakeFactory)
	if err != nil {
		t.Fatalf("synthesizeWithFactory failed: %v", err)
	}

	// Golden: voice must be the default.
	assertArg(t, allArgs, "--voice", "en-US-MichelleNeural")
	// Golden: text must be passed verbatim.
	assertArg(t, allArgs, "--text", "hello world")
	// Golden: no --rate flag when rate is empty.
	if hasFlag(allArgs, "--rate") {
		t.Error("--rate flag must NOT appear for default (empty rate) opts")
	}
}

// TestCharacterization_Edge_WithRate confirms that a non-empty rate value from
// the provider config is forwarded as --rate <value>.
func TestCharacterization_Edge_WithRate(t *testing.T) {
	p := NewProvider(Config{Rate: "+10%"})

	fakeFactory := func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.Command("true")
	}

	allArgs, err := p.synthesizeWithFactory(context.Background(), "hi", audio.TTSOptions{}, fakeFactory)
	if err != nil {
		t.Fatalf("synthesizeWithFactory failed: %v", err)
	}

	assertArg(t, allArgs, "--rate", "+10%")
}

// assertArg checks that flag is immediately followed by want in args.
func assertArg(t *testing.T, args []string, flag, want string) {
	t.Helper()
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			if args[i+1] != want {
				t.Errorf("%s: got %q, want %q", flag, args[i+1], want)
			}
			return
		}
	}
	t.Errorf("flag %q not found in args: %v", flag, args)
}

// hasFlag returns true if flag appears anywhere in args.
func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}
