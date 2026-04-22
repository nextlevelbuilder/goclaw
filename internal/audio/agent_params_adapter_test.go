package audio_test

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/audio/elevenlabs"
	"github.com/nextlevelbuilder/goclaw/internal/audio/minimax"
	oaiprovider "github.com/nextlevelbuilder/goclaw/internal/audio/openai"
)

// TestAdaptAgentParams_TableDriven covers all 5 providers × 3 generic keys
// plus edge cases (empty input, unknown provider, partial keys).
func TestAdaptAgentParams_TableDriven(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		generic  map[string]any
		provider string
		want     map[string]any // nil means expect nil or empty map
	}{
		// --- openai ---
		{name: "openai/speed", generic: map[string]any{"speed": 1.5}, provider: "openai", want: map[string]any{"speed": 1.5}},
		{name: "openai/emotion dropped", generic: map[string]any{"emotion": "happy"}, provider: "openai", want: nil},
		{name: "openai/style dropped", generic: map[string]any{"style": 0.8}, provider: "openai", want: nil},

		// --- elevenlabs ---
		{name: "elevenlabs/speed → voice_settings.speed", generic: map[string]any{"speed": 1.1}, provider: "elevenlabs", want: map[string]any{"voice_settings.speed": 1.1}},
		{name: "elevenlabs/style → voice_settings.style", generic: map[string]any{"style": 0.5}, provider: "elevenlabs", want: map[string]any{"voice_settings.style": 0.5}},
		{name: "elevenlabs/emotion dropped", generic: map[string]any{"emotion": "sad"}, provider: "elevenlabs", want: nil},

		// --- edge ---
		{name: "edge/speed dropped", generic: map[string]any{"speed": 1.0}, provider: "edge", want: nil},
		{name: "edge/emotion dropped", generic: map[string]any{"emotion": "angry"}, provider: "edge", want: nil},
		{name: "edge/style dropped", generic: map[string]any{"style": 0.3}, provider: "edge", want: nil},

		// --- minimax ---
		{name: "minimax/speed", generic: map[string]any{"speed": 1.2}, provider: "minimax", want: map[string]any{"speed": 1.2}},
		{name: "minimax/emotion", generic: map[string]any{"emotion": "happy"}, provider: "minimax", want: map[string]any{"emotion": "happy"}},
		{name: "minimax/style dropped", generic: map[string]any{"style": 0.7}, provider: "minimax", want: nil},

		// --- gemini ---
		{name: "gemini/speed dropped", generic: map[string]any{"speed": 1.0}, provider: "gemini", want: nil},
		{name: "gemini/emotion dropped", generic: map[string]any{"emotion": "excited"}, provider: "gemini", want: nil},
		{name: "gemini/style dropped", generic: map[string]any{"style": 0.4}, provider: "gemini", want: nil},

		// --- edge cases ---
		{name: "empty generic map", generic: map[string]any{}, provider: "minimax", want: nil},
		{name: "nil generic map", generic: nil, provider: "minimax", want: nil},
		{name: "unknown provider returns nil", generic: map[string]any{"speed": 1.0}, provider: "azure", want: nil},

		// --- multi-key cases ---
		{name: "minimax speed+emotion both map", generic: map[string]any{"speed": 0.9, "emotion": "neutral"}, provider: "minimax", want: map[string]any{"speed": 0.9, "emotion": "neutral"}},
		{name: "elevenlabs speed+style both map", generic: map[string]any{"speed": 1.0, "style": 0.3}, provider: "elevenlabs", want: map[string]any{"voice_settings.speed": 1.0, "voice_settings.style": 0.3}},
		{name: "minimax all 3 keys — style dropped", generic: map[string]any{"speed": 1.1, "emotion": "happy", "style": 0.5}, provider: "minimax", want: map[string]any{"speed": 1.1, "emotion": "happy"}},

		// Critical Finding #1: fallback scenario — ElevenLabs native keys are NOT forwarded to MiniMax
		{name: "Finding#1 no cross-provider bleed: voice_settings.speed not a generic key", generic: map[string]any{"speed": 1.1}, provider: "minimax", want: map[string]any{"speed": 1.1}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := audio.AdaptAgentParams(tc.generic, tc.provider)

			if tc.want == nil {
				if len(got) > 0 {
					t.Errorf("expected nil/empty map, got %v", got)
				}
				return
			}

			if len(got) != len(tc.want) {
				t.Errorf("map length mismatch: want %d keys %v, got %d keys %v", len(tc.want), tc.want, len(got), got)
				return
			}
			for wk, wv := range tc.want {
				if gv, ok := got[wk]; !ok {
					t.Errorf("missing key %q in result %v", wk, got)
				} else if gv != wv {
					t.Errorf("key %q: want %v (%T), got %v (%T)", wk, wv, wv, gv, gv)
				}
			}
		})
	}
}

// TestAdaptAgentParams_Finding9_CrossCheck verifies that every provider with
// at least one AdaptAgentParams mapping also has at least one ParamSchema with
// non-empty AgentOverridableAs in its capabilities, AND vice versa.
// This is the compile-guard drift test mandated by red-team Finding #9.
//
// The new AgentOverridableAs field (string) replaces AgentOverridable (bool) to
// encode the generic key alias directly in the capability schema — eliminating
// the separate hard-coded genericToNative lookup tables in each UI.
func TestAdaptAgentParams_Finding9_CrossCheck(t *testing.T) {
	t.Parallel()

	// Providers that the adapter switch handles (non-skip branches).
	// Each must have ≥1 ParamSchema with non-empty AgentOverridableAs in capabilities.
	overridableProviders := map[string]struct {
		caps       []audio.ParamSchema
		adapterMap map[string]any // any non-nil output proves mapping exists
	}{
		"openai": {
			caps:       buildOpenAICaps(),
			adapterMap: audio.AdaptAgentParams(map[string]any{"speed": 1.0}, "openai"),
		},
		"elevenlabs": {
			caps:       buildElevenLabsCaps(),
			adapterMap: audio.AdaptAgentParams(map[string]any{"speed": 1.0}, "elevenlabs"),
		},
		"minimax": {
			caps:       buildMiniMaxCaps(),
			adapterMap: audio.AdaptAgentParams(map[string]any{"speed": 1.0}, "minimax"),
		},
	}

	for providerName, info := range overridableProviders {
		// 1. Adapter must produce output for this provider.
		if len(info.adapterMap) == 0 {
			t.Errorf("provider %q: AdaptAgentParams returned empty map — adapter switch has no active branches", providerName)
		}

		// 2. Capabilities must have at least one param with non-empty AgentOverridableAs.
		hasOverridable := false
		for _, p := range info.caps {
			if p.AgentOverridableAs != "" {
				hasOverridable = true
				break
			}
		}
		if !hasOverridable {
			t.Errorf("provider %q: no ParamSchema with non-empty AgentOverridableAs in capabilities", providerName)
		}
	}

	// 3. Cross-check: every (nativeKey, genericAlias) pair in capabilities must
	// be reflected in the adapter switch. AdaptAgentParams(generic=genericAlias)
	// for this provider must map to nativeKey in the output.
	for providerName, info := range overridableProviders {
		for _, p := range info.caps {
			if p.AgentOverridableAs == "" {
				continue
			}
			genericKey := p.AgentOverridableAs
			nativeKey := p.Key
			result := audio.AdaptAgentParams(map[string]any{genericKey: "sentinel"}, providerName)
			if result == nil {
				t.Errorf("provider %q param %q (generic %q): AdaptAgentParams returned nil — adapter switch missing branch",
					providerName, nativeKey, genericKey)
				continue
			}
			if _, ok := result[nativeKey]; !ok {
				t.Errorf("provider %q param %q (generic %q): adapter output %v does not contain expected native key",
					providerName, nativeKey, genericKey, result)
			}
		}
	}

	// Providers that are skip-only (edge, gemini) must return nil from adapter.
	for _, skipProvider := range []string{"edge", "gemini"} {
		out := audio.AdaptAgentParams(map[string]any{"speed": 1.0, "emotion": "happy", "style": 0.5}, skipProvider)
		if len(out) > 0 {
			t.Errorf("provider %q should produce no output (skip-only) but got %v", skipProvider, out)
		}
	}
}

// buildOpenAICaps returns the params slice from the real OpenAI capabilities.
func buildOpenAICaps() []audio.ParamSchema {
	p := oaiprovider.NewProvider(oaiprovider.Config{})
	return p.Capabilities().Params
}

// buildElevenLabsCaps returns the params slice from the real ElevenLabs capabilities.
func buildElevenLabsCaps() []audio.ParamSchema {
	p := elevenlabs.NewTTSProvider(elevenlabs.Config{})
	return p.Capabilities().Params
}

// buildMiniMaxCaps returns the params slice from the real MiniMax capabilities.
func buildMiniMaxCaps() []audio.ParamSchema {
	p := minimax.NewProvider(minimax.Config{})
	return p.Capabilities().Params
}
