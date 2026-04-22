package telegram

import "testing"

// TestIsVoiceCompatible verifies the voice-compatible content type check.
func TestIsVoiceCompatible(t *testing.T) {
	tests := []struct {
		contentType string
		want        bool
	}{
		// Voice-compatible types
		{"audio/ogg", true},
		{"audio/mpeg", true},
		{"audio/mp3", true},
		{"audio/m4a", true},
		{"audio/x-m4a", true},

		// Non-voice audio types
		{"audio/wav", false},
		{"audio/flac", false},
		{"audio/aac", false},
		{"audio/webm", false},

		// Non-audio types
		{"video/mp4", false},
		{"image/png", false},
		{"application/octet-stream", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.contentType, func(t *testing.T) {
			got := isVoiceCompatible(tt.contentType)
			if got != tt.want {
				t.Errorf("isVoiceCompatible(%q) = %v, want %v", tt.contentType, got, tt.want)
			}
		})
	}
}

// TestVoiceMetadataRouting verifies that audio_as_voice metadata routes correctly.
// This is a unit test for the routing logic - full integration requires bot mocking.
func TestVoiceMetadataRouting(t *testing.T) {
	// Test that voice-compatible audio with audio_as_voice=true should route to sendVoice.
	// Test that non-voice audio or missing flag should route to sendAudio.
	// This documents the expected behavior - actual bot calls require integration tests.

	tests := []struct {
		name          string
		contentType   string
		audioAsVoice  string
		expectVoice   bool
	}{
		{"OGG with voice flag", "audio/ogg", "true", true},
		{"MP3 with voice flag", "audio/mpeg", "true", true},
		{"OGG without voice flag", "audio/ogg", "", false},
		{"OGG with false flag", "audio/ogg", "false", false},
		{"WAV with voice flag", "audio/wav", "true", false}, // WAV not voice-compatible
		{"Video with voice flag", "video/mp4", "true", false}, // not audio
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Routing logic: use voice if metadata["audio_as_voice"] == "true" && isVoiceCompatible(ct)
			shouldUseVoice := tt.audioAsVoice == "true" && isVoiceCompatible(tt.contentType)
			if shouldUseVoice != tt.expectVoice {
				t.Errorf("voice routing for %s: got %v, want %v", tt.name, shouldUseVoice, tt.expectVoice)
			}
		})
	}
}
