package openai

import "github.com/nextlevelbuilder/goclaw/internal/audio"

// openAIModels is the static allowlist of supported OpenAI TTS models.
var openAIModels = []string{
	"tts-1",
	"tts-1-hd",
	"gpt-4o-mini-tts",
}

// openAIVoices is the full 13-voice catalog. tts-1/tts-1-hd support only 9
// (ballad/verse/marin/cedar are gpt-4o-mini-tts only), but backend accepts any
// voice ID — model-compatibility is surfaced as UI help text only.
var openAIVoices = []audio.VoiceOption{
	{VoiceID: "alloy", Name: "Alloy"},
	{VoiceID: "ash", Name: "Ash"},
	{VoiceID: "ballad", Name: "Ballad"},
	{VoiceID: "coral", Name: "Coral"},
	{VoiceID: "echo", Name: "Echo"},
	{VoiceID: "fable", Name: "Fable"},
	{VoiceID: "onyx", Name: "Onyx"},
	{VoiceID: "nova", Name: "Nova"},
	{VoiceID: "sage", Name: "Sage"},
	{VoiceID: "shimmer", Name: "Shimmer"},
	{VoiceID: "verse", Name: "Verse"},
	{VoiceID: "marin", Name: "Marin"},
	{VoiceID: "cedar", Name: "Cedar"},
}

var (
	speedMin  = 0.25
	speedMax  = 4.0
	speedStep = 0.05
)

// openAIParams is the enriched param schema for OpenAI TTS.
// Defaults MUST match the hardcoded values in tts.go (characterization fixture).
var openAIParams = []audio.ParamSchema{
	{
		Key:              "speed",
		Type:             audio.ParamTypeRange,
		Label:            "Speed",
		Description:      "Speech speed multiplier (0.25 = slowest, 4.0 = fastest).",
		Default:          1.0,
		Min:              &speedMin,
		Max:              &speedMax,
		Step:             &speedStep,
		AgentOverridableAs: "speed",
	},
	{
		Key:     "response_format",
		Type:    audio.ParamTypeEnum,
		Label:   "Output Format",
		Default: "mp3",
		Enum: []audio.EnumOption{
			{Value: "mp3", Label: "MP3"},
			{Value: "opus", Label: "Opus"},
			{Value: "aac", Label: "AAC"},
			{Value: "flac", Label: "FLAC"},
			{Value: "wav", Label: "WAV"},
			{Value: "pcm", Label: "PCM"},
		},
	},
	{
		Key:         "instructions",
		Type:        audio.ParamTypeText,
		Label:       "Instructions",
		Description: "System-level style prompt (gpt-4o-mini-tts only). Advanced.",
		Default:     "",
		Group:       "advanced",
		DependsOn: []audio.Dependency{
			{Field: "model", Op: "eq", Value: "gpt-4o-mini-tts"},
		},
	},
}

// Capabilities returns the full capability schema for the OpenAI TTS provider.
func (p *Provider) Capabilities() audio.ProviderCapabilities {
	return audio.ProviderCapabilities{
		Provider:       "openai",
		DisplayName:    "OpenAI TTS",
		RequiresAPIKey: true,
		Models:         openAIModels,
		Voices:         openAIVoices,
		Params:         openAIParams,
	}
}
