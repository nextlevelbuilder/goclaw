package minimax_test

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/audio/minimax"
)

func TestSynthesize_AppliesParams_Speed(t *testing.T) {
	cfg := minimax.Config{APIKey: "k", GroupID: "g"}
	body, _ := captureMiniMaxBody(t, cfg, audio.TTSOptions{
		Params: map[string]any{"speed": 1.5},
	})
	vs, _ := body["voice_setting"].(map[string]any)
	assertMiniMaxFloat(t, vs, "speed", 1.5)
}

func TestSynthesize_AppliesParams_Pitch(t *testing.T) {
	cfg := minimax.Config{APIKey: "k", GroupID: "g"}
	body, _ := captureMiniMaxBody(t, cfg, audio.TTSOptions{
		Params: map[string]any{"pitch": 3},
	})
	vs, _ := body["voice_setting"].(map[string]any)
	if p, _ := vs["pitch"].(float64); int(p) != 3 {
		t.Errorf("pitch: got %v, want 3", vs["pitch"])
	}
}

func TestSynthesize_AppliesParams_AudioGroup_Format(t *testing.T) {
	cfg := minimax.Config{APIKey: "k", GroupID: "g"}
	body, _ := captureMiniMaxBody(t, cfg, audio.TTSOptions{
		Params: map[string]any{
			"audio": map[string]any{"format": "flac"},
		},
	})
	as, _ := body["audio_setting"].(map[string]any)
	if f, _ := as["format"].(string); f != "flac" {
		t.Errorf("audio_setting.format: got %q, want flac", f)
	}
}

func TestSynthesize_AppliesParams_AudioGroup_Bitrate(t *testing.T) {
	cfg := minimax.Config{APIKey: "k", GroupID: "g"}
	body, _ := captureMiniMaxBody(t, cfg, audio.TTSOptions{
		Params: map[string]any{
			"audio": map[string]any{"format": "mp3", "bitrate": "128000"},
		},
	})
	as, _ := body["audio_setting"].(map[string]any)
	if br, _ := as["bitrate"].(string); br != "128000" {
		t.Errorf("audio_setting.bitrate: got %q, want 128000", br)
	}
}

func TestSynthesize_AppliesParams_Emotion(t *testing.T) {
	cfg := minimax.Config{APIKey: "k", GroupID: "g"}
	body, _ := captureMiniMaxBody(t, cfg, audio.TTSOptions{
		Params: map[string]any{"emotion": "happy"},
	})
	vs, _ := body["voice_setting"].(map[string]any)
	if em, _ := vs["emotion"].(string); em != "happy" {
		t.Errorf("voice_setting.emotion: got %q, want happy", em)
	}
}

func TestSynthesize_AppliesParams_OmitsEmpty_NilParams(t *testing.T) {
	cfg := minimax.Config{APIKey: "k", GroupID: "g"}
	body, _ := captureMiniMaxBody(t, cfg, audio.TTSOptions{})
	// text_normalization must be absent for nil params.
	if _, ok := body["text_normalization"]; ok {
		t.Error("text_normalization must not appear for nil params")
	}
	// emotion must be absent.
	vs, _ := body["voice_setting"].(map[string]any)
	if _, ok := vs["emotion"]; ok {
		t.Error("emotion must not appear for nil params")
	}
	// New params must also be absent for nil params.
	if _, ok := body["language_boost"]; ok {
		t.Error("language_boost must not appear for nil params")
	}
	if _, ok := body["subtitle_enable"]; ok {
		t.Error("subtitle_enable must not appear for nil params")
	}
	if _, ok := body["pronunciation_dict"]; ok {
		t.Error("pronunciation_dict must not appear for nil params")
	}
}

// TestSynthesize_AppliesParams_LanguageBoost verifies language_boost top-level field.
func TestSynthesize_AppliesParams_LanguageBoost(t *testing.T) {
	cfg := minimax.Config{APIKey: "k", GroupID: "g"}
	body, _ := captureMiniMaxBody(t, cfg, audio.TTSOptions{
		Params: map[string]any{"language_boost": "Chinese"},
	})
	if lb, _ := body["language_boost"].(string); lb != "Chinese" {
		t.Errorf("language_boost: got %q, want Chinese", body["language_boost"])
	}
}

// TestSynthesize_AppliesParams_SubtitleEnable verifies subtitle_enable top-level field.
func TestSynthesize_AppliesParams_SubtitleEnable(t *testing.T) {
	cfg := minimax.Config{APIKey: "k", GroupID: "g"}
	body, _ := captureMiniMaxBody(t, cfg, audio.TTSOptions{
		Params: map[string]any{"subtitle_enable": true},
	})
	if se, _ := body["subtitle_enable"].(bool); !se {
		t.Errorf("subtitle_enable: got %v, want true", body["subtitle_enable"])
	}
}

// TestSynthesize_AppliesParams_PronunciationDict verifies that a valid JSON array
// is wrapped in {"tone":[...]} in the request body.
func TestSynthesize_AppliesParams_PronunciationDict(t *testing.T) {
	cfg := minimax.Config{APIKey: "k", GroupID: "g"}
	body, _ := captureMiniMaxBody(t, cfg, audio.TTSOptions{
		Params: map[string]any{"pronunciation_dict": `["omg/Oh my god"]`},
	})
	pd, ok := body["pronunciation_dict"].(map[string]any)
	if !ok {
		t.Fatalf("pronunciation_dict: expected object, got %T (%v)", body["pronunciation_dict"], body["pronunciation_dict"])
	}
	tone, ok := pd["tone"].([]any)
	if !ok {
		t.Fatalf("pronunciation_dict.tone: expected array, got %T", pd["tone"])
	}
	if len(tone) != 1 {
		t.Fatalf("pronunciation_dict.tone: want 1 entry, got %d", len(tone))
	}
	if tone[0] != "omg/Oh my god" {
		t.Errorf("pronunciation_dict.tone[0]: got %q, want omg/Oh my god", tone[0])
	}
}

// TestSynthesize_PronunciationDict_InvalidJSON verifies that malformed JSON is
// silently omitted (synth continues) and the field is absent from the body.
func TestSynthesize_PronunciationDict_InvalidJSON(t *testing.T) {
	cfg := minimax.Config{APIKey: "k", GroupID: "g"}
	body, _ := captureMiniMaxBody(t, cfg, audio.TTSOptions{
		Params: map[string]any{"pronunciation_dict": `not valid json`},
	})
	if _, ok := body["pronunciation_dict"]; ok {
		t.Error("pronunciation_dict: must be omitted when JSON is invalid")
	}
}

// TestSynthesize_PronunciationDict_ExceedsLimit verifies that a value exceeding
// 8 KB is silently omitted (Finding #6).
func TestSynthesize_PronunciationDict_ExceedsLimit(t *testing.T) {
	cfg := minimax.Config{APIKey: "k", GroupID: "g"}
	// Build a string > 8192 bytes.
	large := make([]byte, 9000)
	for i := range large {
		large[i] = 'a'
	}
	body, _ := captureMiniMaxBody(t, cfg, audio.TTSOptions{
		Params: map[string]any{"pronunciation_dict": string(large)},
	})
	if _, ok := body["pronunciation_dict"]; ok {
		t.Error("pronunciation_dict: must be omitted when value exceeds 8 KB limit")
	}
}

func TestSynthesize_DoesNotMutateCallerParams(t *testing.T) {
	cfg := minimax.Config{APIKey: "k", GroupID: "g"}
	original := map[string]any{
		"speed":    1.0,
		"sentinel": "untouched",
	}
	snapshot := map[string]any{
		"speed":    original["speed"],
		"sentinel": original["sentinel"],
	}
	captureMiniMaxBody(t, cfg, audio.TTSOptions{Params: original})
	for k, want := range snapshot {
		if got := original[k]; got != want {
			t.Errorf("caller Params mutated: key %q was %v, now %v", k, want, got)
		}
	}
}

func TestCapabilities_HasParam_Speed(t *testing.T) {
	p := minimax.NewProvider(minimax.Config{APIKey: "k"})
	caps := p.Capabilities()
	for _, param := range caps.Params {
		if param.Key == "speed" {
			if param.Default != 1.0 {
				t.Errorf("speed default: got %v, want 1.0", param.Default)
			}
			return
		}
	}
	t.Error("speed param not found")
}

func TestCapabilities_DependsOn_MiniMaxBitrate(t *testing.T) {
	p := minimax.NewProvider(minimax.Config{APIKey: "k"})
	caps := p.Capabilities()
	for _, param := range caps.Params {
		if param.Key == "audio.bitrate" {
			if len(param.DependsOn) == 0 {
				t.Fatal("audio.bitrate must have DependsOn")
			}
			dep := param.DependsOn[0]
			if dep.Field != "audio.format" {
				t.Errorf("DependsOn.Field: got %q, want audio.format", dep.Field)
			}
			if dep.Value != "mp3" {
				t.Errorf("DependsOn.Value: got %v, want mp3", dep.Value)
			}
			return
		}
	}
	t.Error("audio.bitrate param not found")
}

func TestCapabilities_VoicesDynamic(t *testing.T) {
	p := minimax.NewProvider(minimax.Config{APIKey: "k"})
	caps := p.Capabilities()
	if caps.CustomFeatures == nil {
		t.Fatal("CustomFeatures must not be nil")
	}
	if dyn, _ := caps.CustomFeatures["voices_dynamic"].(bool); !dyn {
		t.Error("CustomFeatures.voices_dynamic must be true")
	}
	if len(caps.Voices) != 0 {
		t.Error("Voices must be nil/empty for dynamic provider")
	}
}
