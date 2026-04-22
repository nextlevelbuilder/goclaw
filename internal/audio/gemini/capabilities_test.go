package gemini

import "testing"

func TestCapabilities_Voices(t *testing.T) {
	p := NewProvider(Config{APIKey: "k"})
	caps := p.Capabilities()
	if len(caps.Voices) != 30 {
		t.Errorf("want 30 voices, got %d", len(caps.Voices))
	}
}

func TestCapabilities_Models(t *testing.T) {
	p := NewProvider(Config{APIKey: "k"})
	caps := p.Capabilities()
	if len(caps.Models) != 3 {
		t.Errorf("want 3 models, got %d", len(caps.Models))
	}
}

func TestCapabilities_CustomFeatures(t *testing.T) {
	p := NewProvider(Config{APIKey: "k"})
	caps := p.Capabilities()
	if caps.CustomFeatures == nil {
		t.Fatal("CustomFeatures is nil")
	}
	if _, ok := caps.CustomFeatures["multi_speaker"]; !ok {
		t.Error("CustomFeatures missing 'multi_speaker'")
	}
	if _, ok := caps.CustomFeatures["audio_tags"]; !ok {
		t.Error("CustomFeatures missing 'audio_tags'")
	}
}

func TestCapabilities_RequiresAPIKey(t *testing.T) {
	p := NewProvider(Config{APIKey: "k"})
	caps := p.Capabilities()
	if !caps.RequiresAPIKey {
		t.Error("want RequiresAPIKey=true")
	}
}

func TestCapabilities_ParamsPopulated(t *testing.T) {
	p := NewProvider(Config{APIKey: "k"})
	caps := p.Capabilities()
	// Phase 1: Gemini now exposes 4 params (temperature + seed + presencePenalty + frequencyPenalty).
	if len(caps.Params) != 4 {
		t.Errorf("want 4 params, got %d", len(caps.Params))
	}
}
