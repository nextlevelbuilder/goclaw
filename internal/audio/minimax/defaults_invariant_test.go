package minimax_test

import (
	"encoding/json"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/audio/minimax"
)

// TestDefaults_PreserveLegacyBody verifies that populating opts.Params with all
// Capabilities defaults produces a body byte-equivalent to the nil-Params
// characterization fixture. Guards against silent default flip on save.
func TestDefaults_PreserveLegacyBody(t *testing.T) {
	cfg := minimax.Config{APIKey: "test-key", GroupID: "grp1"}
	p := minimax.NewProvider(cfg)
	caps := p.Capabilities()

	if len(caps.Params) == 0 {
		t.Skip("Capabilities.Params not yet populated (Phase C enrichment pending)")
	}

	params := make(map[string]any)
	for _, s := range caps.Params {
		if s.Default != nil {
			audio.SetNested(params, s.Key, s.Default)
		}
	}

	bodyWithDefaults, urlWithDefaults := captureMiniMaxBody(t, cfg, audio.TTSOptions{Params: params})
	bodyNilParams, urlNilParams := captureMiniMaxBody(t, cfg, audio.TTSOptions{})

	wantJSON := canonicalMiniMaxJSON(t, bodyNilParams)
	gotJSON := canonicalMiniMaxJSON(t, bodyWithDefaults)
	if gotJSON != wantJSON {
		t.Errorf("defaults-invariant body FAILED:\n  with-defaults: %s\n  nil-params:    %s",
			gotJSON, wantJSON)
	}

	if urlWithDefaults != urlNilParams {
		t.Errorf("defaults-invariant URL FAILED:\n  with-defaults: %s\n  nil-params:    %s",
			urlWithDefaults, urlNilParams)
	}
}

func canonicalMiniMaxJSON(t *testing.T, m map[string]any) string {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("canonicalMiniMaxJSON marshal: %v", err)
	}
	return string(b)
}
