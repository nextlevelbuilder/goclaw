// Package openaicompat implements the OpenAI audio API (speech + transcriptions)
// against any OpenAI-compatible endpoint — gpu-manager, Speaches, vLLM, LocalAI,
// llama.cpp, Ollama.
//
// It is deliberately distinct from the openai package. Both speak the same wire
// format, but "openai" means api.openai.com semantics: a fixed voice set
// (alloy, echo, ...), an mp3 default, and a base URL that falls back to the
// public API. A self-hosted engine shares none of that — its voices are its own
// (e.g. Piper's "fr_FR-gilles-low"), its formats are whatever it built in, and
// there is no sane default host.
//
// Keeping them separate matters beyond tidiness: audio.IsVoiceCompatible applies
// OpenAI's voice allowlist to any provider named "openai" and silently
// substitutes the default voice for anything outside it. Routing a self-hosted
// engine through that name turns a valid local voice ID into "alloy" with no
// error. Providers without validation rules pass through untouched, which is the
// correct behaviour here.
package openaicompat

import "errors"

// ErrAPIBaseRequired is returned by the constructors when APIBase is empty.
// Unlike the openai package there is no public endpoint to fall back to, and
// defaulting to api.openai.com would send self-hosted traffic to a vendor.
var ErrAPIBaseRequired = errors.New("openai_compat: api_base is required")

// providerName is the stable identifier reported to the audio Manager and used
// in chain configuration. It must not be "openai" — see the package doc.
const providerName = "openai_compat"

// Default tuning. Formats intentionally follow the OpenAI wire defaults; an
// endpoint that cannot encode mp3 is configured with Format: "wav".
const (
	defaultTTSFormat = "mp3"
	defaultTimeoutMs = 30000
)

// Config bundles the endpoint, credentials and defaults for both the TTS and
// STT providers. APIBase is required; everything else has a default or is
// resolved per-request.
type Config struct {
	// APIBase is the OpenAI-compatible root, including any version prefix —
	// e.g. "http://gpu-manager:8080/v1". Required.
	APIBase string
	// APIKey is sent as a Bearer token when non-empty. Self-hosted endpoints
	// frequently have no auth, so an empty key omits the header entirely
	// rather than sending "Bearer ".
	APIKey string
	// TTSModel is the default model for synthesis. Optional: some engines
	// resolve the model from the requested voice instead.
	TTSModel string
	// TTSVoice is the default voice for synthesis. Optional.
	TTSVoice string
	// TTSFormat is the default response_format for synthesis. Defaults to
	// "mp3" to match OpenAI. Set to "wav" or "pcm" for engines without an
	// encoder.
	TTSFormat string
	// STTModel is the default model for transcription. Optional.
	STTModel string
	// TimeoutMs is the per-request timeout. Defaults to 30000.
	TimeoutMs int
}

// withDefaults returns a copy with zero-valued optional fields filled in.
// APIBase is not defaulted — its absence is an error, not a fallback.
func (c Config) withDefaults() Config {
	if c.TTSFormat == "" {
		c.TTSFormat = defaultTTSFormat
	}
	if c.TimeoutMs <= 0 {
		c.TimeoutMs = defaultTimeoutMs
	}
	return c
}
