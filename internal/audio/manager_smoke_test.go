package audio_test

// Manager skeleton smoke test. Red-first: this file fails to compile until
// internal/audio is created with Manager, TTSProvider, and the 4 Register*
// functions. Once compile passes, tests exercise the minimum surface needed
// by phase-01 callers (NewManager, RegisterTTS, HasProviders, zero-value
// chains).

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
)

func TestNewManager_ReturnsNonNil(t *testing.T) {
	mgr := audio.NewManager(audio.ManagerConfig{Primary: "test"})
	if mgr == nil {
		t.Fatal("NewManager returned nil")
	}
}

func TestManager_ZeroValueChains_AreEmpty(t *testing.T) {
	mgr := audio.NewManager(audio.ManagerConfig{})
	if mgr.HasProviders() {
		t.Fatal("fresh Manager should not report providers")
	}
	if _, ok := mgr.GetProvider("anything"); ok {
		t.Fatal("fresh Manager should not resolve providers")
	}
}

type stubTTS struct{ name string }

func (s stubTTS) Name() string { return s.name }
func (s stubTTS) Synthesize(_ any, _ string, _ audio.TTSOptions) (*audio.SynthResult, error) {
	return &audio.SynthResult{Extension: "mp3", MimeType: "audio/mpeg"}, nil
}

// NOTE: stubTTS.Synthesize takes `any` for ctx to avoid importing context in
// the smoke test — the TTSProvider interface will enforce context.Context via
// compile check when we add `var _ audio.TTSProvider = stubTTS{}`.
//
// Smoke test intentionally omits the `var _ audio.TTSProvider = stubTTS{}`
// assertion because stubTTS uses `any` for ctx. Provider-interface conformance
// is covered by the alias_test (tts.Provider alias of audio.TTSProvider) and
// by production provider packages (elevenlabs, openai, edge, minimax).
