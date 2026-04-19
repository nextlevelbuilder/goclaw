// Package elevenlabs implements the ElevenLabs audio API for TTS and SFX.
// TTS ships in Phase 1; SFX migrated from internal/tools/create_audio_elevenlabs.go.
// Music generation via /v1/music/compose lands in Phase 3.
package elevenlabs

// Config bundles credentials + TTS defaults for ElevenLabs. Shared by the
// TTS and SFX providers — SFX ignores Voice/Model fields.
type Config struct {
	APIKey    string
	BaseURL   string // default "https://api.elevenlabs.io"
	VoiceID   string // default "pMsXgVXv3BLzUgSXRplE" (TTS only)
	ModelID   string // default "eleven_multilingual_v2" (TTS only)
	TimeoutMs int    // default 30000
}

// defaults fills in blank fields with the values used by the legacy tts package.
func (c Config) withDefaults() Config {
	if c.BaseURL == "" {
		c.BaseURL = "https://api.elevenlabs.io"
	}
	if c.VoiceID == "" {
		c.VoiceID = "pMsXgVXv3BLzUgSXRplE"
	}
	if c.ModelID == "" {
		c.ModelID = "eleven_multilingual_v2"
	}
	if c.TimeoutMs <= 0 {
		c.TimeoutMs = 30000
	}
	return c
}
