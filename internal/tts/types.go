// Package tts provides text-to-speech functionality for GoClaw.
// Matching TS src/tts/tts-core.ts + src/tts/tts.ts.
//
// Supported providers: OpenAI, ElevenLabs, Edge (Microsoft).
// Auto modes: off, always, inbound, tagged.
package tts

import "context"

// Provider synthesizes text into audio bytes.
type Provider interface {
	Name() string
	Synthesize(ctx context.Context, text string, opts Options) (*SynthResult, error)
}

// Options controls synthesis parameters.
type Options struct {
	Voice  string // provider-specific voice ID
	Model  string // provider-specific model ID
	Format string // output format: "mp3", "opus" (default depends on channel)
}

// SynthResult is the output of a TTS synthesis.
type SynthResult struct {
	Audio     []byte // raw audio bytes
	Extension string // file extension without dot: "mp3", "opus", "ogg"
	MimeType  string // e.g. "audio/mpeg", "audio/ogg"
}

// AutoMode controls when TTS is automatically applied.
// Matching TS TtsAutoMode.
type AutoMode string

const (
	AutoOff     AutoMode = "off"     // Disabled
	AutoAlways  AutoMode = "always"  // Apply to all eligible replies
	AutoInbound AutoMode = "inbound" // Only if user sent audio/voice
	AutoTagged  AutoMode = "tagged"  // Only if reply contains [[tts]] directive
)

// Mode controls which reply types get TTS.
// Matching TS TtsMode.
type Mode string

const (
	ModeFinal Mode = "final" // Only final replies (default)
	ModeAll   Mode = "all"   // All replies including tool/block
)
