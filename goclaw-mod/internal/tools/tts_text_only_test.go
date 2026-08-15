package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/audio/gemini"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tts"
)

// stubTextOnlyProvider returns ErrTextOnlyResponse on every Synthesize call.
type stubTextOnlyProvider struct{ name string }

func (s *stubTextOnlyProvider) Name() string { return s.name }
func (s *stubTextOnlyProvider) Synthesize(_ context.Context, _ string, _ audio.TTSOptions) (*audio.SynthResult, error) {
	return nil, gemini.ErrTextOnlyResponse
}

// TestTtsTool_TextOnlyErrorMappedToLocale verifies that when the underlying
// provider returns ErrTextOnlyResponse the tool result:
//   - IsError == true
//   - ForLLM contains the locale-appropriate i18n translation (not raw "all tts providers failed")
func TestTtsTool_TextOnlyErrorMappedToLocale(t *testing.T) {
	for _, tc := range []struct {
		locale     string
		wantSubstr string
	}{
		{locale: "en", wantSubstr: i18n.T("en", i18n.MsgTtsGeminiTextOnly)},
		{locale: "vi", wantSubstr: i18n.T("vi", i18n.MsgTtsGeminiTextOnly)},
	} {
		t.Run("locale="+tc.locale, func(t *testing.T) {
			mgr := audio.NewManager(audio.ManagerConfig{Primary: "gemini"})
			mgr.RegisterTTS(&stubTextOnlyProvider{name: "gemini"})

			tool := NewTtsTool((*tts.Manager)(mgr))
			ctx := store.WithLocale(context.Background(), tc.locale)

			result := tool.Execute(ctx, map[string]any{"text": "hello"})

			if result == nil {
				t.Fatal("Execute returned nil")
			}
			if !result.IsError {
				t.Error("expected IsError=true")
			}
			if !strings.Contains(result.ForLLM, tc.wantSubstr) {
				t.Errorf("ForLLM = %q; want substring %q", result.ForLLM, tc.wantSubstr)
			}
			// Must NOT contain the old collapsed message.
			if strings.Contains(result.ForLLM, "all tts providers failed") {
				t.Errorf("ForLLM still contains old collapsed message: %q", result.ForLLM)
			}
			// Sentinel must be detectable from the raw error path — checked via tool returning translated msg.
			_ = errors.Is(gemini.ErrTextOnlyResponse, gemini.ErrTextOnlyResponse) // compile guard
		})
	}
}
