package openai_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/audio/openai"
)

// captureOpenAIBody sends a Synthesize request to a mock server and
// returns the raw JSON body bytes that the provider sent.
func captureOpenAIBody(t *testing.T, cfg openai.Config, opts audio.TTSOptions) []byte {
	t.Helper()
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		captured = b
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Write([]byte("AUDIO"))
	}))
	t.Cleanup(srv.Close)

	cfg.APIBase = srv.URL
	p := openai.NewProvider(cfg)
	_, err := p.Synthesize(t.Context(), "hello", opts)
	if err != nil {
		t.Fatalf("Synthesize failed: %v", err)
	}
	return captured
}

// TestCharacterization_DefaultOpts captures the request body for default opts
// and asserts the expected JSON fields. This is a golden-fixture characterization
// test — the exact shape MUST remain byte-compatible after the Synthesize refactor.
func TestCharacterization_DefaultOpts(t *testing.T) {
	cfg := openai.Config{APIKey: "test-key"}
	body := captureOpenAIBody(t, cfg, audio.TTSOptions{})

	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("body not valid JSON: %v — body: %s", err, body)
	}

	// Golden assertions for default-opts characterization.
	assertJSONField(t, m, "model", "gpt-4o-mini-tts")
	assertJSONField(t, m, "voice", "alloy")
	assertJSONField(t, m, "response_format", "mp3")
	assertJSONField(t, m, "input", "hello")
}

// TestCharacterization_ExplicitVoiceModel verifies the body reflects
// overridden voice/model without mutating any default path.
func TestCharacterization_ExplicitVoiceModel(t *testing.T) {
	cfg := openai.Config{APIKey: "test-key"}
	body := captureOpenAIBody(t, cfg, audio.TTSOptions{Voice: "nova", Model: "tts-1"})

	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("body not valid JSON: %v", err)
	}

	assertJSONField(t, m, "voice", "nova")
	assertJSONField(t, m, "model", "tts-1")
}

func assertJSONField(t *testing.T, m map[string]any, key, want string) {
	t.Helper()
	v, ok := m[key]
	if !ok {
		t.Errorf("missing JSON field %q", key)
		return
	}
	if s, _ := v.(string); s != want {
		t.Errorf("field %q: got %q, want %q", key, s, want)
	}
}
