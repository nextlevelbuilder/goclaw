package gemini

import "errors"

// Sentinel errors returned by Provider.Synthesize.
// Callers should use errors.Is to distinguish these from generic upstream errors.
var (
	// ErrInvalidVoice is returned when the requested voice is not in the Gemini catalog.
	ErrInvalidVoice = errors.New("gemini: invalid voice")

	// ErrSpeakerLimit is returned when more than 2 speakers are requested.
	// Gemini TTS multi-speaker mode supports at most 2 speakers.
	ErrSpeakerLimit = errors.New("gemini: speaker limit exceeded (max 2)")

	// ErrInvalidModel is returned when the requested model is not in the allowlist.
	ErrInvalidModel = errors.New("gemini: invalid model")

	// errTransientNoAudio is an internal sentinel for non-deterministic Gemini
	// failures where the API returns 200 OK but no audio (typically
	// finishReason=OTHER). These are flaky on the preview TTS endpoints and
	// usually succeed on a single retry.
	errTransientNoAudio = errors.New("gemini: transient no-audio response")

	// ErrTextOnlyResponse is returned when Gemini TTS responds 400 indicating it
	// attempted text generation rather than speech synthesis. This typically
	// happens when the input is vague or contains translation/manipulation
	// intent. Retryable once with a stronger prefix (see tts.go retry logic).
	ErrTextOnlyResponse = errors.New("gemini: text-only response (model refused to synthesize audio)")
)
