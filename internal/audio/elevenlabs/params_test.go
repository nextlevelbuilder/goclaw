package elevenlabs_test

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/audio/elevenlabs"
)

func TestSynthesize_AppliesParams_NestedStability(t *testing.T) {
	cfg := elevenlabs.Config{APIKey: "k", VoiceID: "v"}
	body, _ := captureElevenLabsBody(t, cfg, audio.TTSOptions{
		Params: map[string]any{
			"voice_settings": map[string]any{"stability": 0.3},
		},
	})
	vs, _ := body["voice_settings"].(map[string]any)
	if vs == nil {
		t.Fatal("voice_settings missing")
	}
	assertFloat(t, vs, "stability", 0.3)
	// Other defaults must be present.
	assertFloat(t, vs, "similarity_boost", 0.75)
	assertBool(t, vs, "use_speaker_boost", true)
}

func TestSynthesize_AppliesParams_Speed(t *testing.T) {
	cfg := elevenlabs.Config{APIKey: "k", VoiceID: "v"}
	body, _ := captureElevenLabsBody(t, cfg, audio.TTSOptions{
		Params: map[string]any{
			"voice_settings": map[string]any{"speed": 1.1},
		},
	})
	vs, _ := body["voice_settings"].(map[string]any)
	assertFloat(t, vs, "speed", 1.1)
}

func TestSynthesize_AppliesParams_UseSpeakerBoostFalse(t *testing.T) {
	cfg := elevenlabs.Config{APIKey: "k", VoiceID: "v"}
	body, _ := captureElevenLabsBody(t, cfg, audio.TTSOptions{
		Params: map[string]any{
			"voice_settings": map[string]any{"use_speaker_boost": false},
		},
	})
	vs, _ := body["voice_settings"].(map[string]any)
	assertBool(t, vs, "use_speaker_boost", false)
}

func TestSynthesize_AppliesParams_TextNormalization(t *testing.T) {
	cfg := elevenlabs.Config{APIKey: "k", VoiceID: "v"}
	body, _ := captureElevenLabsBody(t, cfg, audio.TTSOptions{
		Params: map[string]any{"apply_text_normalization": "off"},
	})
	if v, _ := body["apply_text_normalization"].(string); v != "off" {
		t.Errorf("apply_text_normalization: got %q, want off", body["apply_text_normalization"])
	}
}

func TestSynthesize_AppliesParams_OmitsEmpty_NilParams(t *testing.T) {
	cfg := elevenlabs.Config{APIKey: "k", VoiceID: "v"}
	body, _ := captureElevenLabsBody(t, cfg, audio.TTSOptions{})
	// apply_text_normalization must be absent when not set (API defaults server-side).
	if _, ok := body["apply_text_normalization"]; ok {
		t.Error("apply_text_normalization must not appear for nil params")
	}
}

func TestSynthesize_DoesNotMutateCallerParams(t *testing.T) {
	cfg := elevenlabs.Config{APIKey: "k", VoiceID: "v"}
	original := map[string]any{
		"voice_settings": map[string]any{"stability": 0.5},
		"sentinel":       "untouched",
	}
	origVS := original["voice_settings"].(map[string]any)
	origStability := origVS["stability"]

	captureElevenLabsBody(t, cfg, audio.TTSOptions{Params: original})

	if original["sentinel"] != "untouched" {
		t.Error("caller Params sentinel was mutated")
	}
	if got := origVS["stability"]; got != origStability {
		t.Errorf("caller voice_settings.stability mutated: was %v, now %v", origStability, got)
	}
}

func TestCapabilities_HasParam_Stability(t *testing.T) {
	p := elevenlabs.NewTTSProvider(elevenlabs.Config{APIKey: "k"})
	caps := p.Capabilities()
	for _, param := range caps.Params {
		if param.Key == "voice_settings.stability" {
			if param.Default != 0.5 {
				t.Errorf("stability default: got %v, want 0.5", param.Default)
			}
			return
		}
	}
	t.Error("voice_settings.stability param not found")
}

func TestCapabilities_HasParam_SimilarityBoost(t *testing.T) {
	p := elevenlabs.NewTTSProvider(elevenlabs.Config{APIKey: "k"})
	caps := p.Capabilities()
	for _, param := range caps.Params {
		if param.Key == "voice_settings.similarity_boost" {
			if param.Default != 0.75 {
				t.Errorf("similarity_boost default: got %v, want 0.75", param.Default)
			}
			return
		}
	}
	t.Error("voice_settings.similarity_boost param not found")
}

func TestCapabilities_HasParam_UseSpeakerBoost_DefaultTrue(t *testing.T) {
	p := elevenlabs.NewTTSProvider(elevenlabs.Config{APIKey: "k"})
	caps := p.Capabilities()
	for _, param := range caps.Params {
		if param.Key == "voice_settings.use_speaker_boost" {
			if param.Default != true {
				t.Errorf("use_speaker_boost default: got %v, want true", param.Default)
			}
			return
		}
	}
	t.Error("voice_settings.use_speaker_boost param not found")
}
