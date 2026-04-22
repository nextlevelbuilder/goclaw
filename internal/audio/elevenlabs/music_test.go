package elevenlabs

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
)

func TestMusicProvider_GenerateMusic(t *testing.T) {
	makeServer := func(t *testing.T, wantPath string, statusCode int, respBody []byte) *httptest.Server {
		t.Helper()
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("expected POST, got %s", r.Method)
			}
			if !strings.HasPrefix(r.URL.Path, "/v1/music") {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}
			if r.URL.Query().Get("output_format") != "mp3_44100_128" {
				t.Errorf("missing output_format query param, got %q", r.URL.Query().Get("output_format"))
			}
			if r.Header.Get("xi-api-key") != "test-key" {
				t.Errorf("missing xi-api-key: got %q", r.Header.Get("xi-api-key"))
			}
			if r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("missing Content-Type: got %q", r.Header.Get("Content-Type"))
			}
			_, _ = io.ReadAll(r.Body) // drain
			w.WriteHeader(statusCode)
			_, _ = w.Write(respBody)
		}))
	}

	musicBytes := []byte("MUSIC_BYTES")

	t.Run("default_model", func(t *testing.T) {
		srv := makeServer(t, "/v1/music", http.StatusOK, musicBytes)
		defer srv.Close()

		p := NewMusicProvider(Config{APIKey: "test-key", BaseURL: srv.URL})
		res, err := p.GenerateMusic(context.Background(), audio.MusicOptions{
			Prompt: "upbeat jazz",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(res.Audio) != string(musicBytes) {
			t.Errorf("audio: want %q, got %q", musicBytes, res.Audio)
		}
		if res.Extension != "mp3" {
			t.Errorf("extension: want mp3, got %s", res.Extension)
		}
		if res.MimeType != "audio/mpeg" {
			t.Errorf("mimetype: want audio/mpeg, got %s", res.MimeType)
		}
		if res.Model != "music_v1" {
			t.Errorf("model: want music_v1, got %s", res.Model)
		}
	})

	t.Run("custom_model_and_duration", func(t *testing.T) {
		srv := makeServer(t, "/v1/music", http.StatusOK, musicBytes)
		defer srv.Close()

		// Verify request body includes duration and model
		var capturedBody []byte
		bodySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedBody, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(musicBytes)
		}))
		defer bodySrv.Close()

		p := NewMusicProvider(Config{APIKey: "test-key", BaseURL: bodySrv.URL})
		_, err := p.GenerateMusic(context.Background(), audio.MusicOptions{
			Prompt:   "chill lofi",
			Model:    "music_v2",
			Duration: 30,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		body := string(capturedBody)
		if !strings.Contains(body, `"music_v2"`) {
			t.Errorf("body missing model_id: %s", body)
		}
		if !strings.Contains(body, `"music_length_ms"`) {
			t.Errorf("body missing music_length_ms: %s", body)
		}
		if !strings.Contains(body, "30000") {
			t.Errorf("body missing 30000 ms: %s", body)
		}
	})

	t.Run("instrumental_flag", func(t *testing.T) {
		var capturedBody []byte
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedBody, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(musicBytes)
		}))
		defer srv.Close()

		p := NewMusicProvider(Config{APIKey: "test-key", BaseURL: srv.URL})
		_, err := p.GenerateMusic(context.Background(), audio.MusicOptions{
			Prompt:       "epic orchestral",
			Instrumental: true,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(string(capturedBody), `"force_instrumental":true`) {
			t.Errorf("body missing force_instrumental:true: %s", capturedBody)
		}
	})

	t.Run("error_429_propagates", func(t *testing.T) {
		srv := makeServer(t, "/v1/music", http.StatusTooManyRequests, []byte(`{"detail":"rate limit exceeded"}`))
		defer srv.Close()

		p := NewMusicProvider(Config{APIKey: "test-key", BaseURL: srv.URL})
		_, err := p.GenerateMusic(context.Background(), audio.MusicOptions{Prompt: "test"})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "429") {
			t.Errorf("error should mention 429: %v", err)
		}
	})
}
