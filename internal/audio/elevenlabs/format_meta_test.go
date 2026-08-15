package elevenlabs_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/audio/elevenlabs"
)

// TestFormatMeta_AllFormats is a table-driven test covering all 27 ElevenLabs
// output_format values plus the fallback for unknown input.
// Finding #7: aligned to researcher-02 — no pcm_mulaw_* variants,
// single audio/pcm MIME for all pcm_* variants.
func TestFormatMeta_AllFormats(t *testing.T) {
	cases := []struct {
		format   string
		wantExt  string
		wantMime string
	}{
		// MP3 variants (6 from capabilities enum)
		{"mp3_22050_32", "mp3", "audio/mpeg"},
		{"mp3_44100_32", "mp3", "audio/mpeg"},
		{"mp3_44100_64", "mp3", "audio/mpeg"},
		{"mp3_44100_96", "mp3", "audio/mpeg"},
		{"mp3_44100_128", "mp3", "audio/mpeg"},
		{"mp3_44100_192", "mp3", "audio/mpeg"},
		// Opus variants (5)
		{"opus_16000_32", "opus", "audio/opus"},
		{"opus_24000_32", "opus", "audio/opus"},
		{"opus_24000_64", "opus", "audio/opus"},
		{"opus_24000_96", "opus", "audio/opus"},
		{"opus_48000_32", "opus", "audio/opus"},
		// PCM variants (7) — all use single audio/pcm MIME (Finding #7)
		{"pcm_8000", "pcm", "audio/pcm"},
		{"pcm_16000", "pcm", "audio/pcm"},
		{"pcm_22050", "pcm", "audio/pcm"},
		{"pcm_24000", "pcm", "audio/pcm"},
		{"pcm_32000", "pcm", "audio/pcm"},
		{"pcm_44100", "pcm", "audio/pcm"},
		{"pcm_48000", "pcm", "audio/pcm"},
		// WAV variants (7)
		{"wav_8000", "wav", "audio/wav"},
		{"wav_16000", "wav", "audio/wav"},
		{"wav_22050", "wav", "audio/wav"},
		{"wav_24000", "wav", "audio/wav"},
		{"wav_32000", "wav", "audio/wav"},
		{"wav_44100", "wav", "audio/wav"},
		{"wav_48000", "wav", "audio/wav"},
		// μ-law and a-law (2)
		{"ulaw_8000", "ulaw", "audio/basic"},
		{"alaw_8000", "alaw", "audio/x-alaw"},
		// Fallback for unknown/empty
		{"unknown_format", "mp3", "audio/mpeg"},
		{"", "mp3", "audio/mpeg"},
	}

	for _, tc := range cases {
		t.Run(tc.format, func(t *testing.T) {
			gotExt, gotMime := elevenlabs.FormatMeta(tc.format)
			if gotExt != tc.wantExt {
				t.Errorf("FormatMeta(%q) ext = %q, want %q", tc.format, gotExt, tc.wantExt)
			}
			if gotMime != tc.wantMime {
				t.Errorf("FormatMeta(%q) mime = %q, want %q", tc.format, gotMime, tc.wantMime)
			}
		})
	}
}

// TestSynthesize_OutputFormat_URLRoundTrip verifies output_format flows into the
// URL query param.
func TestSynthesize_OutputFormat_URLRoundTrip(t *testing.T) {
	cfg := elevenlabs.Config{APIKey: "k", VoiceID: "v"}
	_, path := captureElevenLabsBody(t, cfg, audio.TTSOptions{
		Params: map[string]any{"output_format": "pcm_44100"},
	})
	if !containsStr(path, "output_format=pcm_44100") {
		t.Errorf("expected output_format=pcm_44100 in URL, got: %s", path)
	}
}

// TestSynthesize_OutputFormat_SynthResult verifies SynthResult.Extension and
// MimeType are derived from output_format for a pcm_44100 request.
func TestSynthesize_OutputFormat_SynthResult(t *testing.T) {
	cfg := elevenlabs.Config{APIKey: "k", VoiceID: "v"}
	result := synthesizeAndCapture(t, cfg, audio.TTSOptions{
		Params: map[string]any{"output_format": "pcm_44100"},
	})
	if result.Extension != "pcm" {
		t.Errorf("Extension: got %q, want pcm", result.Extension)
	}
	if result.MimeType != "audio/pcm" {
		t.Errorf("MimeType: got %q, want audio/pcm", result.MimeType)
	}
}

// TestSynthesize_OpusContract_TelegramPath verifies Finding #11:
// when opts.Format="opus", MIME is always "audio/ogg; codecs=opus" regardless
// of any user-set output_format param. Telegram rejects plain "audio/opus" MIME.
func TestSynthesize_OpusContract_TelegramPath(t *testing.T) {
	cfg := elevenlabs.Config{APIKey: "k", VoiceID: "v"}
	result := synthesizeAndCapture(t, cfg, audio.TTSOptions{
		Format: "opus",
		Params: map[string]any{"output_format": "opus_24000_96"},
	})
	if result.MimeType != "audio/ogg; codecs=opus" {
		t.Errorf("Telegram opus contract FAILED: MimeType = %q, want audio/ogg; codecs=opus", result.MimeType)
	}
	if result.Extension != "ogg" {
		t.Errorf("Telegram opus contract FAILED: Extension = %q, want ogg", result.Extension)
	}
}

// TestSynthesize_OutputFormat_InvalidChars verifies Finding #4:
// output_format with injection characters is rejected before any HTTP call.
func TestSynthesize_OutputFormat_InvalidChars(t *testing.T) {
	cases := []string{
		"mp3_44100_128&api_key=LEAK",
		"opus_48000\n",
		"pcm 44100",
		"mp3%20injection",
	}
	for _, bad := range cases {
		t.Run(bad, func(t *testing.T) {
			cfg := elevenlabs.Config{APIKey: "k", VoiceID: "v"}
			_, err := synthesizeWithError(t, cfg, audio.TTSOptions{
				Params: map[string]any{"output_format": bad},
			})
			if err == nil {
				t.Errorf("output_format=%q: expected validation error, got nil", bad)
			}
		})
	}
}

// TestSynthesize_LanguageCode_InvalidChars verifies Finding #4:
// language_code with injection characters is rejected before any HTTP call.
func TestSynthesize_LanguageCode_InvalidChars(t *testing.T) {
	cases := []string{
		"en&voice_id=victim",
		"en\r\nX-Injected: yes",
		"toolong_codex",
	}
	for _, bad := range cases {
		t.Run(bad, func(t *testing.T) {
			cfg := elevenlabs.Config{APIKey: "k", VoiceID: "v"}
			_, err := synthesizeWithError(t, cfg, audio.TTSOptions{
				Params: map[string]any{"language_code": bad},
			})
			if err == nil {
				t.Errorf("language_code=%q: expected validation error, got nil", bad)
			}
		})
	}
}

// synthesizeAndCapture runs Synthesize against a mock server and returns the
// *audio.SynthResult. Fails the test on any error.
func synthesizeAndCapture(t *testing.T, cfg elevenlabs.Config, opts audio.TTSOptions) *audio.SynthResult {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Write([]byte("AUDIO"))
	}))
	t.Cleanup(srv.Close)
	cfg.BaseURL = srv.URL
	p := elevenlabs.NewTTSProvider(cfg)
	result, err := p.Synthesize(t.Context(), "hello", opts)
	if err != nil {
		t.Fatalf("Synthesize failed: %v", err)
	}
	return result
}

// synthesizeWithError runs Synthesize and returns (result, err) without failing
// the test — used to assert expected errors.
func synthesizeWithError(t *testing.T, cfg elevenlabs.Config, opts audio.TTSOptions) (*audio.SynthResult, error) {
	t.Helper()
	// Use a mock server so validation-path errors (pre-HTTP) are still exercised.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Write([]byte("AUDIO"))
	}))
	t.Cleanup(srv.Close)
	cfg.BaseURL = srv.URL
	p := elevenlabs.NewTTSProvider(cfg)
	return p.Synthesize(t.Context(), "hello", opts)
}
