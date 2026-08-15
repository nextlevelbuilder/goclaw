package minimax_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/audio/minimax"
)

// TestGolden_DefaultBody verifies the serialized nil-params request body matches
// the checked-in golden fixture. Finding #10: external golden file catches
// simultaneous Default+tts.go drift that the self-referential invariant test misses.
func TestGolden_DefaultBody(t *testing.T) {
	cfg := minimax.Config{APIKey: "test-key", GroupID: "grp1"}
	body, reqURL := captureMiniMaxBody(t, cfg, audio.TTSOptions{})

	full := map[string]any{
		"body": body,
		"url":  reqURL,
	}
	gotFull, err := json.MarshalIndent(full, "", "  ")
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	goldenPath := filepath.Join("testdata", "default_body.golden.json")
	if _, statErr := os.Stat(goldenPath); os.IsNotExist(statErr) {
		if err := os.MkdirAll("testdata", 0755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(goldenPath, gotFull, 0644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("golden file created: %s", goldenPath)
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}

	var wantMap, gotMap map[string]any
	if err := json.Unmarshal(want, &wantMap); err != nil {
		t.Fatalf("parse golden: %v", err)
	}
	if err := json.Unmarshal(gotFull, &gotMap); err != nil {
		t.Fatalf("parse current: %v", err)
	}

	wantNorm := canonicalMiniMaxJSON(t, wantMap)
	gotNorm := canonicalMiniMaxJSON(t, gotMap)
	if gotNorm != wantNorm {
		t.Errorf("golden fixture mismatch (update testdata/default_body.golden.json if intentional):\n  got:  %s\n  want: %s",
			gotNorm, wantNorm)
	}
}
