package openai_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/audio/openai"
)

// TestGolden_DefaultBody verifies the serialized nil-params request body matches
// the checked-in golden fixture. Finding #10: external golden file catches
// simultaneous Default+tts.go drift that the self-referential invariant test misses.
func TestGolden_DefaultBody(t *testing.T) {
	cfg := openai.Config{APIKey: "test-key"}
	raw := captureOpenAIBody(t, cfg, audio.TTSOptions{})

	goldenPath := filepath.Join("testdata", "default_body.golden.json")
	if _, statErr := os.Stat(goldenPath); os.IsNotExist(statErr) {
		if err := os.MkdirAll("testdata", 0755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		// Pretty-print the raw bytes for readability.
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("parse body: %v", err)
		}
		pretty, _ := json.MarshalIndent(m, "", "  ")
		if err := os.WriteFile(goldenPath, pretty, 0644); err != nil {
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
	wantNorm := canonicalOpenAIJSON(t, want)
	gotNorm := canonicalOpenAIJSON(t, raw)
	if gotNorm != wantNorm {
		t.Errorf("golden fixture mismatch (update testdata/default_body.golden.json if intentional):\n  got:  %s\n  want: %s",
			gotNorm, wantNorm)
	}
}

// canonicalOpenAIJSON normalises raw JSON bytes to a deterministic string
// by round-tripping through map[string]any.
func canonicalOpenAIJSON(t *testing.T, raw []byte) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("canonicalOpenAIJSON unmarshal: %v", err)
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("canonicalOpenAIJSON marshal: %v", err)
	}
	return string(b)
}
