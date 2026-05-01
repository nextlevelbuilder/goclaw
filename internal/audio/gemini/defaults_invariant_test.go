package gemini_test

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/audio/gemini"
)

// captureGeminiBody sends a Synthesize request to a mock server and returns
// the decoded JSON request body + the URL path used.
func captureGeminiBody(t *testing.T, cfg gemini.Config, opts audio.TTSOptions) (map[string]any, string) {
	t.Helper()

	pcm := make([]byte, 64)
	b64 := base64.StdEncoding.EncodeToString(pcm)
	respJSON := []byte(`{"candidates":[{"content":{"parts":[{"inlineData":{"data":"` + b64 + `"}}]}}]}`)

	var captured []byte
	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.String()
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		captured = b
		w.Header().Set("Content-Type", "application/json")
		w.Write(respJSON)
	}))
	t.Cleanup(srv.Close)

	cfg.APIBase = srv.URL
	p := gemini.NewProvider(cfg)
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

// TestDefaults_PreserveLegacyBody verifies that populating opts.Params with all
// Capabilities defaults produces a body byte-equivalent to the nil-Params request.
// Guards against silent default flip when new params land.
func TestDefaults_PreserveLegacyBody(t *testing.T) {
	cfg := gemini.Config{APIKey: "test-key"}
	p := gemini.NewProvider(cfg)
	caps := p.Capabilities()

	if len(caps.Params) == 0 {
		t.Skip("Capabilities.Params not yet populated")
	}

	// Build params map from all schema defaults (only non-nil defaults).
	params := make(map[string]any)
	for _, s := range caps.Params {
		if s.Default != nil {
			audio.SetNested(params, s.Key, s.Default)
		}
	}

	bodyWithDefaults, _ := captureGeminiBody(t, cfg, audio.TTSOptions{Params: params})
	bodyNilParams, _ := captureGeminiBody(t, cfg, audio.TTSOptions{})

	wantJSON := canonicalGeminiJSON(t, bodyNilParams)
	gotJSON := canonicalGeminiJSON(t, bodyWithDefaults)

	if gotJSON != wantJSON {
		t.Errorf("defaults-invariant body FAILED:\n  with-defaults: %s\n  nil-params:    %s",
			gotJSON, wantJSON)
	}
}

// TestGolden_DefaultBody verifies the serialized nil-params request body matches
// the checked-in golden fixture. Update the fixture file when intentional changes land.
// Finding #10: external golden file catches simultaneous Default+tts.go drift.
func TestGolden_DefaultBody(t *testing.T) {
	cfg := gemini.Config{APIKey: "test-key", Voice: "Kore"}
	body, _ := captureGeminiBody(t, cfg, audio.TTSOptions{})

	got, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	goldenPath := filepath.Join("testdata", "default_body.golden.json")

	if _, err := os.Stat(goldenPath); os.IsNotExist(err) {
		// First run: write the golden file.
		if err := os.MkdirAll("testdata", 0755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(goldenPath, got, 0644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("golden file created: %s", goldenPath)
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}

	// Normalize both through JSON round-trip for deterministic key ordering.
	var wantMap, gotMap map[string]any
	if err := json.Unmarshal(want, &wantMap); err != nil {
		t.Fatalf("parse golden: %v", err)
	}
	if err := json.Unmarshal(got, &gotMap); err != nil {
		t.Fatalf("parse current body: %v", err)
	}
	wantNorm := canonicalGeminiJSON(t, wantMap)
	gotNorm := canonicalGeminiJSON(t, gotMap)

	if gotNorm != wantNorm {
		t.Errorf("golden fixture mismatch (update testdata/default_body.golden.json if intentional):\n  got:  %s\n  want: %s",
			gotNorm, wantNorm)
	}
}

func canonicalGeminiJSON(t *testing.T, m map[string]any) string {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("canonicalGeminiJSON: %v", err)
	}
	return string(b)
}
