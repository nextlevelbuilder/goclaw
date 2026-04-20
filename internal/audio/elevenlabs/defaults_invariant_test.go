package elevenlabs_test

import (
	"encoding/json"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/audio/elevenlabs"
)

// TestDefaults_PreserveLegacyBody verifies that populating opts.Params with
// all Capabilities defaults produces a body byte-equivalent to the nil-Params
// characterization fixture. Guards against silent default flip on save.
func TestDefaults_PreserveLegacyBody(t *testing.T) {
	cfg := elevenlabs.Config{APIKey: "test-key", VoiceID: "test-voice"}
	p := elevenlabs.NewTTSProvider(cfg)
	caps := p.Capabilities()

	if len(caps.Params) == 0 {
		t.Skip("Capabilities.Params not yet populated (Phase C enrichment pending)")
	}

	// Build params map from all schema defaults (nested-key aware).
	params := make(map[string]any)
	for _, s := range caps.Params {
		if s.Default != nil {
			audio.SetNested(params, s.Key, s.Default)
		}
	}

	bodyWithDefaults, pathWithDefaults := captureElevenLabsBody(t, cfg, audio.TTSOptions{Params: params})
	bodyNilParams, pathNilParams := captureElevenLabsBody(t, cfg, audio.TTSOptions{})

	// Re-encode both maps to canonical JSON for deterministic comparison.
	wantJSON := canonicalJSON(t, bodyNilParams)
	gotJSON := canonicalJSON(t, bodyWithDefaults)

	if gotJSON != wantJSON {
		t.Errorf("defaults-invariant body FAILED:\n  with-defaults: %s\n  nil-params:    %s",
			gotJSON, wantJSON)
	}

	// URL (output_format query param) must also be identical.
	if pathWithDefaults != pathNilParams {
		t.Errorf("defaults-invariant URL FAILED:\n  with-defaults: %s\n  nil-params:    %s",
			pathWithDefaults, pathNilParams)
	}
}

// canonicalJSON re-marshals a decoded map to produce a deterministic JSON
// string suitable for equality comparison.
func canonicalJSON(t *testing.T, m map[string]any) string {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("canonicalJSON marshal: %v", err)
	}
	return string(b)
}
