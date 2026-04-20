package audio_test

import (
	"context"
	"errors"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/audio/gemini"
)

// mockSentinelTTS returns a configurable error from Synthesize.
type mockSentinelTTS struct {
	providerName string
	err          error
}

func (m *mockSentinelTTS) Name() string { return m.providerName }
func (m *mockSentinelTTS) Synthesize(_ context.Context, _ string, _ audio.TTSOptions) (*audio.SynthResult, error) {
	return nil, m.err
}

// TestSynthesizeWithFallbackAdapted_PreservesTextOnlySentinel verifies that
// ErrTextOnlyResponse survives through SynthesizeWithFallbackAdapted so that
// errors.Is(err, gemini.ErrTextOnlyResponse) returns true at the call site.
func TestSynthesizeWithFallbackAdapted_PreservesTextOnlySentinel(t *testing.T) {
	t.Run("primary_only_returns_sentinel", func(t *testing.T) {
		// Single provider: primary returns ErrTextOnlyResponse. No fallback.
		mgr := audio.NewManager(audio.ManagerConfig{Primary: "gemini"})
		mgr.RegisterTTS(&mockSentinelTTS{
			providerName: "gemini",
			err:          gemini.ErrTextOnlyResponse,
		})

		_, err := mgr.SynthesizeWithFallbackAdapted(context.Background(), "hello", audio.TTSOptions{}, nil)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, gemini.ErrTextOnlyResponse) {
			t.Errorf("errors.Is(err, ErrTextOnlyResponse) = false; err = %v", err)
		}
	})

	t.Run("primary_sentinel_plus_fallback_other_error", func(t *testing.T) {
		// Primary returns ErrTextOnlyResponse; fallback returns a different error.
		// Sentinel must survive errors.Join.
		mgr := audio.NewManager(audio.ManagerConfig{Primary: "gemini"})
		mgr.RegisterTTS(&mockSentinelTTS{
			providerName: "gemini",
			err:          gemini.ErrTextOnlyResponse,
		})
		mgr.RegisterTTS(&mockSentinelTTS{
			providerName: "openai",
			err:          errors.New("openai: connection refused"),
		})

		_, err := mgr.SynthesizeWithFallbackAdapted(context.Background(), "hello", audio.TTSOptions{}, nil)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, gemini.ErrTextOnlyResponse) {
			t.Errorf("errors.Is(err, ErrTextOnlyResponse) = false after errors.Join; err = %v", err)
		}
	})
}
