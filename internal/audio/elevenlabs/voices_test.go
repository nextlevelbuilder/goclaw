package elevenlabs_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/audio/elevenlabs"
)

func TestListVoices_HappyPath(t *testing.T) {
	fixture := map[string]any{
		"voices": []map[string]any{
			{
				"voice_id":    "voice1",
				"name":        "Bella",
				"labels":      map[string]any{"accent": "american", "gender": "female"},
				"preview_url": "https://cdn.example.com/bella.mp3",
				"category":    "premade",
			},
			{
				"voice_id":    "voice2",
				"name":        "Adam",
				"labels":      map[string]any{"accent": "british"},
				"preview_url": "https://cdn.example.com/adam.mp3",
				"category":    "cloned",
			},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/voices" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("xi-api-key") != "test-key" {
			t.Errorf("missing or wrong api key header")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(fixture)
	}))
	defer srv.Close()

	p := elevenlabs.NewTTSProvider(elevenlabs.Config{
		APIKey:  "test-key",
		BaseURL: srv.URL,
	})
	voices, err := p.ListVoices(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(voices) != 2 {
		t.Fatalf("expected 2 voices, got %d", len(voices))
	}
	if voices[0].ID != "voice1" || voices[0].Name != "Bella" {
		t.Errorf("voice[0] mismatch: %+v", voices[0])
	}
	if voices[0].Labels["accent"] != "american" {
		t.Errorf("voice[0] label accent mismatch: %v", voices[0].Labels)
	}
	if voices[0].PreviewURL != "https://cdn.example.com/bella.mp3" {
		t.Errorf("voice[0] preview url mismatch: %s", voices[0].PreviewURL)
	}
	if voices[1].ID != "voice2" || voices[1].Category != "cloned" {
		t.Errorf("voice[1] mismatch: %+v", voices[1])
	}
}

func TestListVoices_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"detail":"invalid_api_key"}`))
	}))
	defer srv.Close()

	p := elevenlabs.NewTTSProvider(elevenlabs.Config{
		APIKey:  "bad-key",
		BaseURL: srv.URL,
	})
	_, err := p.ListVoices(t.Context())
	if err == nil {
		t.Fatal("expected error for 401, got nil")
	}
	// Error must mention the status code or response body
	if !containsStr(err.Error(), "401") && !containsStr(err.Error(), "invalid_api_key") {
		t.Errorf("expected 401 info in error message, got: %v", err)
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
