package gemini_test

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/audio/gemini"
)

// TestSynthesize_Params_Temperature verifies temperature lands in generationConfig.
func TestSynthesize_Params_Temperature(t *testing.T) {
	cfg := gemini.Config{APIKey: "k"}
	body, _ := captureGeminiBody(t, cfg, audio.TTSOptions{
		Params: map[string]any{"temperature": 1.5},
	})
	gc := assertGeminiGenerationConfig(t, body)
	if v, _ := gc["temperature"].(float64); v != 1.5 {
		t.Errorf("temperature: got %v, want 1.5", gc["temperature"])
	}
}

// TestSynthesize_Params_Seed verifies seed lands in generationConfig.
func TestSynthesize_Params_Seed(t *testing.T) {
	cfg := gemini.Config{APIKey: "k"}
	body, _ := captureGeminiBody(t, cfg, audio.TTSOptions{
		Params: map[string]any{"seed": 12345},
	})
	gc := assertGeminiGenerationConfig(t, body)
	// JSON round-trip: int comes back as float64.
	if v, _ := gc["seed"].(float64); int(v) != 12345 {
		t.Errorf("seed: got %v, want 12345", gc["seed"])
	}
}

// TestSynthesize_Params_PresencePenalty verifies presencePenalty in generationConfig.
func TestSynthesize_Params_PresencePenalty(t *testing.T) {
	cfg := gemini.Config{APIKey: "k"}
	body, _ := captureGeminiBody(t, cfg, audio.TTSOptions{
		Params: map[string]any{"presencePenalty": 0.3},
	})
	gc := assertGeminiGenerationConfig(t, body)
	if v, _ := gc["presencePenalty"].(float64); v != 0.3 {
		t.Errorf("presencePenalty: got %v, want 0.3", gc["presencePenalty"])
	}
}

// TestSynthesize_Params_FrequencyPenalty verifies frequencyPenalty in generationConfig.
func TestSynthesize_Params_FrequencyPenalty(t *testing.T) {
	cfg := gemini.Config{APIKey: "k"}
	body, _ := captureGeminiBody(t, cfg, audio.TTSOptions{
		Params: map[string]any{"frequencyPenalty": -0.1},
	})
	gc := assertGeminiGenerationConfig(t, body)
	if v, _ := gc["frequencyPenalty"].(float64); v != -0.1 {
		t.Errorf("frequencyPenalty: got %v, want -0.1", gc["frequencyPenalty"])
	}
}

// TestSynthesize_Params_AllFour verifies all four params land together.
func TestSynthesize_Params_AllFour(t *testing.T) {
	cfg := gemini.Config{APIKey: "k"}
	body, _ := captureGeminiBody(t, cfg, audio.TTSOptions{
		Params: map[string]any{
			"temperature":      1.5,
			"seed":             12345,
			"presencePenalty":  0.3,
			"frequencyPenalty": -0.1,
		},
	})
	gc := assertGeminiGenerationConfig(t, body)
	if v, _ := gc["temperature"].(float64); v != 1.5 {
		t.Errorf("temperature: got %v, want 1.5", gc["temperature"])
	}
	if v, _ := gc["seed"].(float64); int(v) != 12345 {
		t.Errorf("seed: got %v, want 12345", gc["seed"])
	}
	if v, _ := gc["presencePenalty"].(float64); v != 0.3 {
		t.Errorf("presencePenalty: got %v, want 0.3", gc["presencePenalty"])
	}
	if v, _ := gc["frequencyPenalty"].(float64); v != -0.1 {
		t.Errorf("frequencyPenalty: got %v, want -0.1", gc["frequencyPenalty"])
	}
}

// TestSynthesize_Params_NilParams_NoExtraKeys verifies nil params produces no
// extra keys in generationConfig beyond the mandatory ones.
func TestSynthesize_Params_NilParams_NoExtraKeys(t *testing.T) {
	cfg := gemini.Config{APIKey: "k"}
	body, _ := captureGeminiBody(t, cfg, audio.TTSOptions{})
	gc := assertGeminiGenerationConfig(t, body)

	for _, extra := range []string{"temperature", "seed", "presencePenalty", "frequencyPenalty"} {
		if _, ok := gc[extra]; ok {
			t.Errorf("nil params: generationConfig must not contain %q", extra)
		}
	}
}

// TestCapabilities_Gemini_HasFourParams confirms Capabilities returns exactly
// the four documented params with correct Group tags.
func TestCapabilities_Gemini_HasFourParams(t *testing.T) {
	p := gemini.NewProvider(gemini.Config{APIKey: "k"})
	caps := p.Capabilities()

	type want struct {
		group string
	}
	expected := map[string]want{
		"temperature":      {group: ""},
		"seed":             {group: "advanced"},
		"presencePenalty":  {group: "advanced"},
		"frequencyPenalty": {group: "advanced"},
	}

	found := map[string]bool{}
	for _, p := range caps.Params {
		if w, ok := expected[p.Key]; ok {
			found[p.Key] = true
			if p.Group != w.group {
				t.Errorf("param %q: Group got %q, want %q", p.Key, p.Group, w.group)
			}
		}
	}
	for key := range expected {
		if !found[key] {
			t.Errorf("param %q not found in Capabilities", key)
		}
	}
}

// assertGeminiGenerationConfig extracts generationConfig from the body and fails
// if it is missing or not a map.
func assertGeminiGenerationConfig(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	gc, ok := body["generationConfig"].(map[string]any)
	if !ok {
		t.Fatalf("generationConfig missing or not a map: %#v", body["generationConfig"])
	}
	return gc
}
