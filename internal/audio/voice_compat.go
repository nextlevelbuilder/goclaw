package audio

import "strings"

// edgeVoiceDefaultVoice is the default Edge TTS voice.
const edgeVoiceDefaultVoice = "en-US-AriaNeural"

// openaiDefaultVoice is the default OpenAI TTS voice.
const openaiDefaultVoice = "alloy"

// IsVoiceCompatible reports whether the given voice ID is compatible with the
// named TTS provider. Returns true for providers without validation rules.
//
// Edge voices follow the BCP-47 + Neural suffix pattern (e.g. "en-US-GuyNeural").
// OpenAI voices are a fixed set: alloy, echo, fable, onyx, nova, shimmer.
func IsVoiceCompatible(provider, voice string) bool {
	if voice == "" {
		return true
	}
	switch provider {
	case "edge":
		return strings.Contains(voice, "Neural")
	case "openai":
		switch voice {
		case "alloy", "echo", "fable", "onyx", "nova", "shimmer":
			return true
		}
		return false
	default:
		// No validation for other providers.
		return true
	}
}

// GetProviderDefaultVoice returns the default voice ID for the named provider.
// Returns an empty string for providers where the SDK selects its own default.
func GetProviderDefaultVoice(provider string) string {
	switch provider {
	case "edge":
		return edgeVoiceDefaultVoice
	case "openai":
		return openaiDefaultVoice
	default:
		return ""
	}
}

// FilterVoiceForProvider returns the voice to use for the given provider.
// If the voice is incompatible with the provider it falls back to the
// provider's default voice (which may be empty, signalling "use SDK default").
// agentOverride indicates the voice came from agent configuration (not from
// explicit tool args or tenant defaults) so that the caller can decide whether
// to emit a warning.
func FilterVoiceForProvider(provider, voice string, agentOverride bool) (filtered string, changed bool) {
	if IsVoiceCompatible(provider, voice) {
		return voice, false
	}
	def := GetProviderDefaultVoice(provider)
	return def, true
}
