package elevenlabs

import "github.com/nextlevelbuilder/goclaw/internal/audio"

// elevenLabsModels lists all allowed ElevenLabs TTS model IDs (mirrors AllowedElevenLabsModels).
var elevenLabsModels = []string{
	"eleven_v3",
	"eleven_flash_v2_5",
	"eleven_multilingual_v2",
	"eleven_turbo_v2_5",
}

var (
	stabilityMin  = 0.0
	stabilityMax  = 1.0
	stabilityStep = 0.01
	simBoostMin   = 0.0
	simBoostMax   = 1.0
	simBoostStep  = 0.01
	styleMin      = 0.0
	styleMax      = 1.0
	styleStep     = 0.01
	elSpeedMin    = 0.7
	elSpeedMax    = 1.2
	elSpeedStep   = 0.01
	latencyMin    = 0.0
	latencyMax    = 4.0
	latencyStep   = 1.0
	seedMin       = 0.0
	seedMax       = 4294967295.0 // 2^32-1
	seedStep      = 1.0
)

// elevenLabsParams is the enriched param schema for ElevenLabs TTS.
// Defaults MUST match the hardcoded values in tts.go (characterization fixture):
//
//	stability=0.5, similarity_boost=0.75, style=0.0, use_speaker_boost=true, speed=1.0
//
// Group tagging (Finding #7 + §Requirements #7):
//   - seed, optimize_streaming_latency, language_code, output_format = "advanced"
//   - all others = "" (basic)
var elevenLabsParams = []audio.ParamSchema{
	{
		Key:         "voice_settings.stability",
		Type:        audio.ParamTypeRange,
		Label:       "Stability",
		Description: "Voice stability (0 = more variable, 1 = more consistent).",
		Default:     0.5,
		Min:         &stabilityMin,
		Max:         &stabilityMax,
		Step:        &stabilityStep,
	},
	{
		Key:         "voice_settings.similarity_boost",
		Type:        audio.ParamTypeRange,
		Label:       "Similarity Boost",
		Description: "Similarity to original voice (0 = more creative, 1 = more similar).",
		Default:     0.75,
		Min:         &simBoostMin,
		Max:         &simBoostMax,
		Step:        &simBoostStep,
	},
	{
		Key:                "voice_settings.style",
		Type:               audio.ParamTypeRange,
		Label:              "Style Exaggeration",
		Description:        "Style exaggeration amount (0 = none, 1 = maximum).",
		Default:            0.0,
		Min:                &styleMin,
		Max:                &styleMax,
		Step:               &styleStep,
		AgentOverridableAs: "style",
	},
	{
		Key:     "voice_settings.use_speaker_boost",
		Type:    audio.ParamTypeBoolean,
		Label:   "Speaker Boost",
		Default: true,
	},
	{
		Key:                "voice_settings.speed",
		Type:               audio.ParamTypeRange,
		Label:              "Speed",
		Description:        "Speech speed (0.7 = slowest, 1.2 = fastest).",
		Default:            1.0,
		Min:                &elSpeedMin,
		Max:                &elSpeedMax,
		Step:               &elSpeedStep,
		AgentOverridableAs: "speed",
	},
	{
		Key:     "apply_text_normalization",
		Type:    audio.ParamTypeEnum,
		Label:   "Text Normalization",
		Default: "", // empty = omit from request (API defaults to "auto" server-side)
		Enum: []audio.EnumOption{
			{Value: "", Label: "Auto (default)"},
			{Value: "on", Label: "On"},
			{Value: "off", Label: "Off"},
		},
	},
	{
		Key:         "seed",
		Type:        audio.ParamTypeInteger,
		Label:       "Seed",
		Description: "Deterministic seed for reproducible output (0 = random). Advanced.",
		Default:     0,
		Min:         &seedMin,
		Max:         &seedMax,
		Step:        &seedStep,
		Group:       "advanced",
	},
	{
		Key:         "optimize_streaming_latency",
		Type:        audio.ParamTypeRange,
		Label:       "Streaming Latency Optimization",
		Description: "Latency optimization level (0 = none, 4 = maximum). Advanced.",
		Default:     0.0,
		Min:         &latencyMin,
		Max:         &latencyMax,
		Step:        &latencyStep,
		Group:       "advanced",
	},
	{
		Key:         "language_code",
		Type:        audio.ParamTypeString,
		Label:       "Language Code",
		Description: "ISO 639-1 language hint (e.g. 'en', 'vi'). Advanced.",
		Default:     "",
		Group:       "advanced",
	},
	// output_format — URL query param controlling codec + bitrate.
	// 27 values per ElevenLabs API docs. Default mp3_44100_128 matches the
	// hardcoded fallback in tts.go so the defaults-invariant test stays green.
	// Tier note: mp3_44100_192 requires Creator+; pcm_*/wav_* require Pro+.
	// Backend forwards whatever the user sets; API returns 403 on tier violation.
	{
		Key:   "output_format",
		Type:  audio.ParamTypeEnum,
		Label: "Output Format",
		Description: "Audio codec and bitrate. Higher quality tiers require Creator+ or Pro+ subscription. " +
			"Advanced.",
		Default: "mp3_44100_128",
		Group:   "advanced",
		Enum: []audio.EnumOption{
			// MP3 variants
			{Value: "mp3_22050_32", Label: "MP3 22 kHz 32 kbps"},
			{Value: "mp3_44100_32", Label: "MP3 44.1 kHz 32 kbps"},
			{Value: "mp3_44100_64", Label: "MP3 44.1 kHz 64 kbps"},
			{Value: "mp3_44100_96", Label: "MP3 44.1 kHz 96 kbps"},
			{Value: "mp3_44100_128", Label: "MP3 44.1 kHz 128 kbps (default)"},
			{Value: "mp3_44100_192", Label: "MP3 44.1 kHz 192 kbps (Creator+)"},
			// Opus variants
			{Value: "opus_16000_32", Label: "Opus 16 kHz 32 kbps"},
			{Value: "opus_24000_32", Label: "Opus 24 kHz 32 kbps"},
			{Value: "opus_24000_64", Label: "Opus 24 kHz 64 kbps"},
			{Value: "opus_24000_96", Label: "Opus 24 kHz 96 kbps"},
			{Value: "opus_48000_32", Label: "Opus 48 kHz 32 kbps"},
			// PCM variants (Pro+)
			{Value: "pcm_8000", Label: "PCM 8 kHz (Pro+)"},
			{Value: "pcm_16000", Label: "PCM 16 kHz (Pro+)"},
			{Value: "pcm_22050", Label: "PCM 22 kHz (Pro+)"},
			{Value: "pcm_24000", Label: "PCM 24 kHz (Pro+)"},
			{Value: "pcm_32000", Label: "PCM 32 kHz (Pro+)"},
			{Value: "pcm_44100", Label: "PCM 44.1 kHz (Pro+)"},
			{Value: "pcm_48000", Label: "PCM 48 kHz (Pro+)"},
			// WAV variants (Pro+)
			{Value: "wav_8000", Label: "WAV 8 kHz (Pro+)"},
			{Value: "wav_16000", Label: "WAV 16 kHz (Pro+)"},
			{Value: "wav_22050", Label: "WAV 22 kHz (Pro+)"},
			{Value: "wav_24000", Label: "WAV 24 kHz (Pro+)"},
			{Value: "wav_32000", Label: "WAV 32 kHz (Pro+)"},
			{Value: "wav_44100", Label: "WAV 44.1 kHz (Pro+)"},
			{Value: "wav_48000", Label: "WAV 48 kHz (Pro+)"},
			// μ-law and a-law
			{Value: "ulaw_8000", Label: "μ-law 8 kHz"},
			{Value: "alaw_8000", Label: "A-law 8 kHz"},
		},
	},
}

// Capabilities returns the full capability schema for the ElevenLabs TTS provider.
// Voices is nil — ElevenLabs voices are user-specific and fetched dynamically via /v1/voices.
func (p *TTSProvider) Capabilities() audio.ProviderCapabilities {
	return audio.ProviderCapabilities{
		Provider:       "elevenlabs",
		DisplayName:    "ElevenLabs TTS",
		RequiresAPIKey: true,
		Models:         elevenLabsModels,
		Params:         elevenLabsParams,
	}
}
