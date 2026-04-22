package elevenlabs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
)

func TestSynthesize_RejectsUnknownModel(t *testing.T) {
	t.Parallel()
	// Server must never be reached — validation runs before HTTP.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected upstream request for invalid model")
	}))
	defer srv.Close()

	p := NewTTSProvider(Config{APIKey: "k", BaseURL: srv.URL, VoiceID: "V"})
	_, err := p.Synthesize(context.Background(), "hi", audio.TTSOptions{Model: "bogus-model"})
	if err == nil {
		t.Fatal("expected error for unknown model, got nil")
	}
}

func TestSynthesize_EmptyModelPassesValidation(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("AUDIO"))
	}))
	defer srv.Close()

	p := NewTTSProvider(Config{APIKey: "k", BaseURL: srv.URL, VoiceID: "V"})
	res, err := p.Synthesize(context.Background(), "hi", audio.TTSOptions{})
	if err != nil {
		t.Fatalf("Synthesize with empty model: %v", err)
	}
	if string(res.Audio) != "AUDIO" {
		t.Errorf("audio: got %q, want AUDIO", res.Audio)
	}
}
