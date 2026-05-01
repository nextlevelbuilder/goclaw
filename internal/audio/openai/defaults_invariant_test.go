package openai_test

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/audio/openai"
)

// TestDefaults_PreserveLegacyBody verifies that populating opts.Params with
// all Capabilities defaults produces a body byte-equivalent to the nil-Params
// characterization fixture. This guards against "silent default flip" where a
// user saves an unchanged form, the blob stores schema defaults, and the next
// synthesize call sends different values upstream.
func TestDefaults_PreserveLegacyBody(t *testing.T) {
	cfg := openai.Config{APIKey: "test-key"}
	p := openai.NewProvider(cfg)
	caps := p.Capabilities()

	if len(caps.Params) == 0 {
		t.Skip("Capabilities.Params not yet populated (Phase C enrichment pending)")
	}

	// Build params map from all schema defaults.
	params := make(map[string]any)
	for _, s := range caps.Params {
		if s.Default != nil {
			audio.SetNested(params, s.Key, s.Default)
		}
	}

	// Capture body with defaults-from-schema.
	bodyWithDefaults := captureOpenAIBody(t, cfg, audio.TTSOptions{Params: params})
	// Capture body with nil Params (current legacy path).
	bodyNilParams := captureOpenAIBody(t, cfg, audio.TTSOptions{})

	if string(bodyWithDefaults) != string(bodyNilParams) {
		t.Errorf("defaults-invariant FAILED:\n  with-defaults: %s\n  nil-params:    %s",
			bodyWithDefaults, bodyNilParams)
	}
}
