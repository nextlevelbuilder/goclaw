package minimax_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/audio/minimax"
)

// captureMiniMaxBody sends a Synthesize request to a mock server and
// returns the decoded JSON body map + request URL.
func captureMiniMaxBody(t *testing.T, cfg minimax.Config, opts audio.TTSOptions) (map[string]any, string) {
	t.Helper()
	var captured []byte
	var capturedURL string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURL = r.URL.String()
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		captured = b
		// MiniMax returns hex-encoded audio in a JSON envelope.
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"base_resp":{"status_code":0,"status_msg":"ok"},"data":{"audio":"deadbeef"}}`))
	}))
	t.Cleanup(srv.Close)

	cfg.APIBase = srv.URL
	p := minimax.NewProvider(cfg)
	_, err := p.Synthesize(t.Context(), "hello", opts)
	if err != nil {
		t.Fatalf("Synthesize failed: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(captured, &m); err != nil {
		t.Fatalf("body not valid JSON: %v — body: %s", err, captured)
	}
	return m, capturedURL
}

// TestCharacterization_MiniMax_DefaultOpts is the golden-fixture test.
// It captures the exact request body that the current hardcoded tts.go emits
// for default opts. These values MUST remain identical after the Synthesize refactor.
func TestCharacterization_MiniMax_DefaultOpts(t *testing.T) {
	cfg := minimax.Config{APIKey: "test-key", GroupID: "grp1"}
	body, reqURL := captureMiniMaxBody(t, cfg, audio.TTSOptions{})

	// Golden: stream must be false.
	if v, _ := body["stream"].(bool); v != false {
		t.Errorf("stream: got %v, want false", body["stream"])
	}

	// Golden: model defaults.
	if v, _ := body["model"].(string); v != "speech-02-hd" {
		t.Errorf("model: got %q, want speech-02-hd", v)
	}

	// Golden: voice_setting shape.
	vs, ok := body["voice_setting"].(map[string]any)
	if !ok {
		t.Fatalf("voice_setting missing or not an object: %#v", body["voice_setting"])
	}
	if vid, _ := vs["voice_id"].(string); vid != "Wise_Woman" {
		t.Errorf("voice_setting.voice_id: got %q, want Wise_Woman", vid)
	}
	assertMiniMaxFloat(t, vs, "speed", 1.0)
	if pitch, ok := vs["pitch"]; ok {
		// pitch is int 0 — JSON unmarshal gives float64
		if pf, _ := pitch.(float64); pf != 0 {
			t.Errorf("voice_setting.pitch: got %v, want 0", pitch)
		}
	}

	// Golden: audio_setting shape.
	as, ok := body["audio_setting"].(map[string]any)
	if !ok {
		t.Fatalf("audio_setting missing or not an object: %#v", body["audio_setting"])
	}
	if fmt, _ := as["format"].(string); fmt != "mp3" {
		t.Errorf("audio_setting.format: got %q, want mp3", fmt)
	}

	// Golden: GroupId in URL query.
	if !containsMiniMaxStr(reqURL, "GroupId=grp1") {
		t.Errorf("expected GroupId=grp1 in URL, got: %s", reqURL)
	}
}

// TestCharacterization_MiniMax_OpusFormat verifies the format override flows
// through to audio_setting.format.
func TestCharacterization_MiniMax_OpusFormat(t *testing.T) {
	cfg := minimax.Config{APIKey: "key", GroupID: "g"}
	body, _ := captureMiniMaxBody(t, cfg, audio.TTSOptions{Format: "flac"})

	as, ok := body["audio_setting"].(map[string]any)
	if !ok {
		t.Fatalf("audio_setting missing")
	}
	if fmt, _ := as["format"].(string); fmt != "flac" {
		t.Errorf("audio_setting.format: got %q, want flac", fmt)
	}
}

func assertMiniMaxFloat(t *testing.T, m map[string]any, key string, want float64) {
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

func containsMiniMaxStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
