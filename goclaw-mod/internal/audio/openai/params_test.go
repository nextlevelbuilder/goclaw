package openai_test

import (
	"encoding/json"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/audio/openai"
)

func TestSynthesize_AppliesParams_Speed(t *testing.T) {
	cfg := openai.Config{APIKey: "k"}
	body := captureOpenAIBody(t, cfg, audio.TTSOptions{
		Params: map[string]any{"speed": 1.5},
	})
	var m map[string]any
	json.Unmarshal(body, &m)
	if v, _ := m["speed"].(float64); v != 1.5 {
		t.Errorf("speed: got %v, want 1.5", m["speed"])
	}
}

func TestSynthesize_AppliesParams_ResponseFormat(t *testing.T) {
	cfg := openai.Config{APIKey: "k"}
	body := captureOpenAIBody(t, cfg, audio.TTSOptions{
		Params: map[string]any{"response_format": "opus"},
	})
	var m map[string]any
	json.Unmarshal(body, &m)
	if v, _ := m["response_format"].(string); v != "opus" {
		t.Errorf("response_format: got %q, want opus", m["response_format"])
	}
}

func TestSynthesize_AppliesParams_Instructions(t *testing.T) {
	cfg := openai.Config{APIKey: "k"}
	body := captureOpenAIBody(t, cfg, audio.TTSOptions{
		Model:  "gpt-4o-mini-tts",
		Params: map[string]any{"instructions": "speak slowly"},
	})
	var m map[string]any
	json.Unmarshal(body, &m)
	if v, _ := m["instructions"].(string); v != "speak slowly" {
		t.Errorf("instructions: got %q, want 'speak slowly'", m["instructions"])
	}
}

func TestSynthesize_AppliesParams_OmitsEmpty_NilParams(t *testing.T) {
	cfg := openai.Config{APIKey: "k"}
	body := captureOpenAIBody(t, cfg, audio.TTSOptions{})
	var m map[string]any
	json.Unmarshal(body, &m)
	// instructions must be absent when not set
	if _, ok := m["instructions"]; ok {
		t.Error("instructions must not appear in body when not in params")
	}
}

func TestSynthesize_DoesNotMutateCallerParams(t *testing.T) {
	cfg := openai.Config{APIKey: "k"}
	original := map[string]any{
		"speed":    1.0,
		"sentinel": "untouched",
	}
	// Deep copy to compare after call.
	snapshot := map[string]any{
		"speed":    original["speed"],
		"sentinel": original["sentinel"],
	}

	captureOpenAIBody(t, cfg, audio.TTSOptions{Params: original})

	for k, want := range snapshot {
		if got := original[k]; got != want {
			t.Errorf("caller Params mutated: key %q was %v, now %v", k, want, got)
		}
	}
	if len(original) != len(snapshot) {
		t.Errorf("caller Params size changed: was %d, now %d", len(snapshot), len(original))
	}
}

func TestCapabilities_HasParam_Speed(t *testing.T) {
	p := openai.NewProvider(openai.Config{APIKey: "k"})
	caps := p.Capabilities()
	assertParamExists(t, caps.Params, "speed", audio.ParamTypeRange)
}

func TestCapabilities_HasParam_ResponseFormat(t *testing.T) {
	p := openai.NewProvider(openai.Config{APIKey: "k"})
	caps := p.Capabilities()
	assertParamExists(t, caps.Params, "response_format", audio.ParamTypeEnum)
}

func TestCapabilities_DependsOn_OpenAIInstructions(t *testing.T) {
	p := openai.NewProvider(openai.Config{APIKey: "k"})
	caps := p.Capabilities()
	for _, param := range caps.Params {
		if param.Key == "instructions" {
			if len(param.DependsOn) == 0 {
				t.Fatal("instructions param must have DependsOn")
			}
			dep := param.DependsOn[0]
			if dep.Field != "model" {
				t.Errorf("DependsOn.Field: got %q, want model", dep.Field)
			}
			if dep.Value != "gpt-4o-mini-tts" {
				t.Errorf("DependsOn.Value: got %v, want gpt-4o-mini-tts", dep.Value)
			}
			return
		}
	}
	t.Error("instructions param not found in Capabilities")
}

func TestCapabilities_Has13Voices(t *testing.T) {
	p := openai.NewProvider(openai.Config{APIKey: "k"})
	caps := p.Capabilities()
	if len(caps.Voices) != 13 {
		t.Errorf("expected 13 voices, got %d", len(caps.Voices))
	}
}

func assertParamExists(t *testing.T, params []audio.ParamSchema, key string, typ audio.ParamType) {
	t.Helper()
	for _, p := range params {
		if p.Key == key {
			if p.Type != typ {
				t.Errorf("param %q: type got %q, want %q", key, p.Type, typ)
			}
			return
		}
	}
	t.Errorf("param %q not found in schema", key)
}
