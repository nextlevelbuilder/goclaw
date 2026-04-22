package elevenlabs_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/audio/elevenlabs"
)

// captureElevenLabsBody sends a Synthesize request to a mock server and
// returns the decoded JSON body map + the URL path used.
func captureElevenLabsBody(t *testing.T, cfg elevenlabs.Config, opts audio.TTSOptions) (map[string]any, string) {
	t.Helper()
	var captured []byte
	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.String()
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		captured = b
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Write([]byte("AUDIO"))
	}))
	t.Cleanup(srv.Close)

	cfg.BaseURL = srv.URL
	p := elevenlabs.NewTTSProvider(cfg)
	_, err := p.Synthesize(t.Context(), "hello", opts)
	if err != nil {
		t.Fatalf("Synthesize failed: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(captured, &m); err != nil {
		t.Fatalf("body not valid JSON: %v — body: %s", err, captured)
	}
	return m, capturedPath
}

// TestCharacterization_ElevenLabs_DefaultOpts is the golden-fixture test.
// It captures the exact voice_settings shape that the current hardcoded tts.go emits.
// These values MUST remain byte-compatible after the Synthesize refactor.
func TestCharacterization_ElevenLabs_DefaultOpts(t *testing.T) {
	cfg := elevenlabs.Config{APIKey: "test-key", VoiceID: "test-voice"}
	body, _ := captureElevenLabsBody(t, cfg, audio.TTSOptions{})

	// Assert top-level fields.
	if v, _ := body["text"].(string); v != "hello" {
		t.Errorf("text: got %q, want %q", v, "hello")
	}

	// Assert voice_settings defaults — these are the hardcoded values at tts.go:54-59.
	vs, ok := body["voice_settings"].(map[string]any)
	if !ok {
		t.Fatalf("voice_settings missing or not an object: %#v", body["voice_settings"])
	}

	assertFloat(t, vs, "stability", 0.5)
	assertFloat(t, vs, "similarity_boost", 0.75)
	assertFloat(t, vs, "style", 0.0)
	assertBool(t, vs, "use_speaker_boost", true)
}

// TestCharacterization_ElevenLabs_DefaultOutputFormat verifies the default
// output_format query param is mp3_44100_128 when Format is empty.
func TestCharacterization_ElevenLabs_DefaultOutputFormat(t *testing.T) {
	cfg := elevenlabs.Config{APIKey: "test-key", VoiceID: "my-voice"}
	_, path := captureElevenLabsBody(t, cfg, audio.TTSOptions{})

	if !containsStr(path, "output_format=mp3_44100_128") {
		t.Errorf("expected output_format=mp3_44100_128 in URL, got: %s", path)
	}
	if !containsStr(path, "my-voice") {
		t.Errorf("expected voice ID in URL path, got: %s", path)
	}
}

func assertFloat(t *testing.T, m map[string]any, key string, want float64) {
	t.Helper()
	v, ok := m[key]
	if !ok {
		t.Errorf("missing field %q", key)
		return
	}
	got, ok := v.(float64)
	if !ok {
		t.Errorf("field %q: expected float64, got %T (%v)", key, v, v)
		return
	}
	if got != want {
		t.Errorf("field %q: got %v, want %v", key, got, want)
	}
}

func assertBool(t *testing.T, m map[string]any, key string, want bool) {
	t.Helper()
	v, ok := m[key]
	if !ok {
		t.Errorf("missing field %q", key)
		return
	}
	got, ok := v.(bool)
	if !ok {
		t.Errorf("field %q: expected bool, got %T (%v)", key, v, v)
		return
	}
	if got != want {
		t.Errorf("field %q: got %v, want %v", key, got, want)
	}
}
