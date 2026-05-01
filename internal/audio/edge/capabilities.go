package edge

import "github.com/nextlevelbuilder/goclaw/internal/audio"

// edgeDefaultVoices lists a representative set of Edge TTS voices.
// Full list is available via `edge-tts --list-voices`; these cover common locales.
var edgeDefaultVoices = []audio.VoiceOption{
	{VoiceID: "en-US-MichelleNeural", Name: "Michelle", Language: "en-US", Gender: "Female"},
	{VoiceID: "en-US-GuyNeural", Name: "Guy", Language: "en-US", Gender: "Male"},
	{VoiceID: "en-GB-SoniaNeural", Name: "Sonia", Language: "en-GB", Gender: "Female"},
	{VoiceID: "vi-VN-HoaiMyNeural", Name: "HoaiMy", Language: "vi-VN", Gender: "Female"},
	{VoiceID: "vi-VN-NamMinhNeural", Name: "NamMinh", Language: "vi-VN", Gender: "Male"},
	{VoiceID: "zh-CN-XiaoxiaoNeural", Name: "Xiaoxiao", Language: "zh-CN", Gender: "Female"},
	{VoiceID: "zh-CN-YunxiNeural", Name: "Yunxi", Language: "zh-CN", Gender: "Male"},
}

var (
	rateMin    = -50.0
	rateMax    = 100.0
	rateStep   = 1.0
	pitchMin   = -50.0
	pitchMax   = 50.0
	pitchStep  = 1.0
	volumeMin  = -50.0
	volumeMax  = 100.0
	volumeStep = 1.0
)

// edgeParams is the enriched param schema for Edge TTS.
// Defaults MUST match the hardcoded tts.go behaviour (no --rate/--pitch/--volume flags = zero).
// UI slider int values; Synthesize converts via sliderToRateString / sliderToPitchString.
var edgeParams = []audio.ParamSchema{
	{
		Key:         "rate",
		Type:        audio.ParamTypeInteger,
		Label:       "Rate",
		Description: "Speech rate adjustment in percent (-50 to +100).",
		Default:     0,
		Min:         &rateMin,
		Max:         &rateMax,
		Step:        &rateStep,
	},
	{
		Key:         "pitch",
		Type:        audio.ParamTypeInteger,
		Label:       "Pitch",
		Description: "Pitch adjustment in Hz (-50 to +50).",
		Default:     0,
		Min:         &pitchMin,
		Max:         &pitchMax,
		Step:        &pitchStep,
	},
	{
		Key:         "volume",
		Type:        audio.ParamTypeInteger,
		Label:       "Volume",
		Description: "Volume adjustment in percent (-50 to +100).",
		Default:     0,
		Min:         &volumeMin,
		Max:         &volumeMax,
		Step:        &volumeStep,
	},
}

// Capabilities returns the full capability schema for the Edge TTS provider.
// Edge TTS requires no API key (uses the free edge-tts CLI).
func (p *Provider) Capabilities() audio.ProviderCapabilities {
	return audio.ProviderCapabilities{
		Provider:       "edge",
		DisplayName:    "Edge TTS (Microsoft)",
		RequiresAPIKey: false,
		Voices:         edgeDefaultVoices,
		Params:         edgeParams,
	}
}
