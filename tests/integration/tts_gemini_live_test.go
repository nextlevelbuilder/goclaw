//go:build integration

package integration

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/audio/gemini"
)

// TestGeminiLive_UpstreamContract verifies that the Gemini provider maintains
// a valid contract with the real Google Generative AI API (preview endpoint).
//
// This test is gated on GEMINI_API_KEY env var. It synthesizes a 50-character
// text against the real API and validates the response structure and audio data.
//
// Skip conditions:
//   - GEMINI_API_KEY not set (development environments)
//   - 5xx responses (Google infrastructure issue, not our bug)
//
// Cadence: Monthly manual run (or opt-in nightly via CI repository secret).
//
// On failure: File issue against the provider tracking version pinning or API migration.
func TestGeminiLive_UpstreamContract(t *testing.T) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		t.Skip("GEMINI_API_KEY not set; skipping live upstream test")
	}

	// Create Gemini provider with real API endpoint
	p := gemini.NewProvider(gemini.Config{
		APIKey:  apiKey,
		APIBase: "https://generativelanguage.googleapis.com",
		Voice:   "Kore",
		Model:   "gemini-2.0-flash-tts-preview",
	})

	ctx := context.Background()

	// Synthesize a 50-character test text
	testText := "Hello, this is a test of the Google Gemini TTS API."

	result, err := p.Synthesize(ctx, testText, audio.TTSOptions{})
	if err != nil {
		// On 5xx: skip (not our bug)
		if isServerError(err) {
			t.Skipf("Google API returned 5xx: %v (infrastructure issue, not our bug)", err)
		}
		// On other errors: fail
		t.Fatalf("Synthesize failed: %v", err)
	}

	// Validate result structure
	if result == nil {
		t.Fatal("Synthesize returned nil result")
	}

	// WAV header validation: first 4 bytes must be "RIFF"
	if len(result.Audio) < 4 {
		t.Fatalf("Audio too short (%d bytes), cannot check WAV header", len(result.Audio))
	}
	riffHeader := string(result.Audio[0:4])
	if riffHeader != "RIFF" {
		t.Errorf("WAV RIFF header invalid: got %q, want RIFF", riffHeader)
	}

	// MIME type validation
	if result.MimeType != "audio/wav" {
		t.Errorf("MIME type: got %q, want audio/wav", result.MimeType)
	}

	// Audio payload minimum size: non-trivial audio should be at least 2KB
	if len(result.Audio) < 2048 {
		t.Errorf("Audio payload too small (%d bytes), want at least 2048", len(result.Audio))
	}

	t.Logf("Gemini live test passed: %d bytes of audio", len(result.Audio))
}

// isServerError checks if an error is due to a 5xx response from the Google API.
// This is a simple heuristic; it checks for "500" or "503" in the error message.
func isServerError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "500") || strings.Contains(msg, "503") || strings.Contains(msg, "server error")
}
