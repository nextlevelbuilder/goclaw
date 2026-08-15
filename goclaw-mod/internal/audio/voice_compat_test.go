package audio_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
)

func TestIsVoiceCompatible(t *testing.T) {
	tests := []struct {
		provider string
		voice    string
		want     bool
	}{
		// Edge provider — Neural suffix required
		{"edge", "en-US-GuyNeural", true},
		{"edge", "en-US-MichelleNeural", true},
		{"edge", "alloy", false},
		{"edge", "echo", false},
		{"edge", "", true},
		// OpenAI provider — fixed set
		{"openai", "alloy", true},
		{"openai", "echo", true},
		{"openai", "fable", true},
		{"openai", "onyx", true},
		{"openai", "nova", true},
		{"openai", "shimmer", true},
		{"openai", "en-US-GuyNeural", false},
		{"openai", "", true},
		// Other providers — no validation
		{"elevenlabs", "any-voice-id", true},
		{"minimax", "en-US-GuyNeural", true},
	}
	for _, tc := range tests {
		got := audio.IsVoiceCompatible(tc.provider, tc.voice)
		assert.Equal(t, tc.want, got, "IsVoiceCompatible(%q, %q)", tc.provider, tc.voice)
	}
}

func TestGetProviderDefaultVoice(t *testing.T) {
	assert.Equal(t, "en-US-AriaNeural", audio.GetProviderDefaultVoice("edge"))
	assert.Equal(t, "alloy", audio.GetProviderDefaultVoice("openai"))
	assert.Equal(t, "", audio.GetProviderDefaultVoice("elevenlabs"))
}

func TestFilterVoiceForProvider(t *testing.T) {
	// Compatible — unchanged.
	got, changed := audio.FilterVoiceForProvider("openai", "alloy", false)
	assert.False(t, changed)
	assert.Equal(t, "alloy", got)

	// Incompatible edge voice with openai — falls back to openai default.
	got, changed = audio.FilterVoiceForProvider("openai", "en-US-GuyNeural", true)
	assert.True(t, changed)
	assert.Equal(t, "alloy", got)

	// Incompatible openai voice with edge — falls back to edge default.
	got, changed = audio.FilterVoiceForProvider("edge", "alloy", true)
	assert.True(t, changed)
	assert.Equal(t, "en-US-AriaNeural", got)

	// Empty voice — unchanged regardless.
	got, changed = audio.FilterVoiceForProvider("openai", "", false)
	assert.False(t, changed)
	assert.Equal(t, "", got)
}
