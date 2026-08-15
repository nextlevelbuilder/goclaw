package gemini

import "github.com/nextlevelbuilder/goclaw/internal/audio"

var (
	geminiTempMin  = 0.0
	geminiTempMax  = 2.0
	geminiTempStep = 0.01

	geminiPenaltyMin  = -2.0
	geminiPenaltyMax  = 2.0
	geminiPenaltyStep = 0.01
)

// geminiParams is the enriched param schema for Gemini TTS.
// All params have nil Default so they are omitted from the request body
// when not explicitly set, preserving characterization byte-equivalence.
//
// Tagging:
//   - temperature: basic (Group="")
//   - seed, presencePenalty, frequencyPenalty: advanced (Group="advanced")
var geminiParams = []audio.ParamSchema{
	{
		Key:  "temperature",
		Type: audio.ParamTypeRange,
		Label: "Temperature",
		// Finding #14: note subtle effect on TTS; primary expressiveness via audio tags.
		Description: "Sampling temperature (0.0–2.0, default 1.0). Effect is subtle on TTS — primary expressiveness comes from audio tags inserted in the text.",
		Default:     nil, // omit from body when not set; API default is 1.0
		Min:         &geminiTempMin,
		Max:         &geminiTempMax,
		Step:        &geminiTempStep,
		Group:       "", // basic
	},
	{
		Key:         "seed",
		Type:        audio.ParamTypeInteger,
		Label:       "Seed",
		Description: "Deterministic seed for reproducible output. Advanced.",
		Default:     nil,
		Group:       "advanced",
	},
	{
		Key:         "presencePenalty",
		Type:        audio.ParamTypeRange,
		Label:       "Presence Penalty",
		Description: "Presence penalty (-2.0–2.0). Experimental — effect on TTS is not well-characterised. Advanced.",
		Default:     nil,
		Min:         &geminiPenaltyMin,
		Max:         &geminiPenaltyMax,
		Step:        &geminiPenaltyStep,
		Group:       "advanced",
	},
	{
		Key:         "frequencyPenalty",
		Type:        audio.ParamTypeRange,
		Label:       "Frequency Penalty",
		Description: "Frequency penalty (-2.0–2.0). Experimental — effect on TTS is not well-characterised. Advanced.",
		Default:     nil,
		Min:         &geminiPenaltyMin,
		Max:         &geminiPenaltyMax,
		Step:        &geminiPenaltyStep,
		Group:       "advanced",
	},
}

// Capabilities returns the static capability schema for the Gemini TTS provider.
// CustomFeatures drives frontend custom slot rendering in Phase C.
func (p *Provider) Capabilities() audio.ProviderCapabilities {
	return audio.ProviderCapabilities{
		Provider:       "gemini",
		DisplayName:    "Google Gemini TTS",
		RequiresAPIKey: true,
		Models:         geminiModels,
		Voices:         geminiVoices,
		Params:         geminiParams,
		CustomFeatures: map[string]any{
			"multi_speaker": true,
			"audio_tags":    true,
		},
	}
}
