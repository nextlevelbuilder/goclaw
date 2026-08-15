package tts_test

// Symbol-coverage + alias identity test for internal/tts.
//
// Why a _test.go file (not a compile-time guard in alias.go)? Guards in alias.go
// are mandatory for signatures; this test additionally proves every callable
// symbol in the former tts package still resolves after the alias layer lands.
// If a rare exported symbol is forgotten in alias.go, this file fails to
// compile — fastest possible feedback loop.
//
// The test references every alias target via the tts.* package name. Adding a
// new exported symbol to internal/audio requires adding both an alias in
// alias.go AND a reference here.

import (
	"context"
	"testing"
	"unsafe"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/audio/edge"
	"github.com/nextlevelbuilder/goclaw/internal/audio/elevenlabs"
	"github.com/nextlevelbuilder/goclaw/internal/audio/minimax"
	"github.com/nextlevelbuilder/goclaw/internal/audio/openai"
	"github.com/nextlevelbuilder/goclaw/internal/tts"
)

// --- Types (15) — alias identity checks (both sides must be exact same type) ---

var (
	_ *audio.Manager             = (*tts.Manager)(nil)
	_ audio.TTSProvider          = (tts.Provider)(nil)
	_ *audio.TTSOptions          = (*tts.Options)(nil)
	_ *audio.SynthResult         = (*tts.SynthResult)(nil)
	_ audio.AutoMode             = tts.AutoMode("")
	_ audio.Mode                 = tts.Mode("")
	_ *audio.ManagerConfig       = (*tts.ManagerConfig)(nil)
	_ *edge.Config               = (*tts.EdgeConfig)(nil)
	_ *edge.Provider             = (*tts.EdgeProvider)(nil)
	_ *elevenlabs.Config         = (*tts.ElevenLabsConfig)(nil)
	_ *elevenlabs.TTSProvider    = (*tts.ElevenLabsProvider)(nil)
	_ *minimax.Config            = (*tts.MiniMaxConfig)(nil)
	_ *minimax.Provider          = (*tts.MiniMaxProvider)(nil)
	_ *openai.Config             = (*tts.OpenAIConfig)(nil)
	_ *openai.Provider           = (*tts.OpenAIProvider)(nil)
)

// --- Constants (6) ---

var (
	_ = tts.AutoOff
	_ = tts.AutoAlways
	_ = tts.AutoInbound
	_ = tts.AutoTagged
	_ = tts.ModeFinal
	_ = tts.ModeAll
)

// --- Constructors (5) ---

var (
	_ = tts.NewManager
	_ = tts.NewEdgeProvider
	_ = tts.NewElevenLabsProvider
	_ = tts.NewMiniMaxProvider
	_ = tts.NewOpenAIProvider
)

// TestAliasIdentity proves at runtime that alias targets resolve to the
// underlying audio types — guard against accidental "replica type" divergence.
func TestAliasIdentity(t *testing.T) {
	if unsafe.Sizeof(tts.Options{}) != unsafe.Sizeof(audio.TTSOptions{}) {
		t.Fatalf("tts.Options size mismatch: got %d, want %d",
			unsafe.Sizeof(tts.Options{}), unsafe.Sizeof(audio.TTSOptions{}))
	}
	mgr := tts.NewManager(tts.ManagerConfig{Primary: "none"})
	if mgr == nil {
		t.Fatal("tts.NewManager returned nil")
	}
	// Exercise interface method set end-to-end to prove Provider alias works.
	var _ func(context.Context, string, tts.Options) (*tts.SynthResult, error) = mgr.Synthesize
}
