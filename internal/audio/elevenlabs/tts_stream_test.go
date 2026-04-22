package elevenlabs

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
)

// chunkedServer returns an httptest.Server that streams `chunks` back with
// Flush between each. The handler also records the request path + headers.
func chunkedServer(t *testing.T, chunks []string) (*httptest.Server, *atomic.Value) {
	t.Helper()
	lastPath := &atomic.Value{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastPath.Store(r.URL.RequestURI())
		if r.Header.Get("xi-api-key") == "" {
			t.Errorf("missing xi-api-key header")
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		for _, c := range chunks {
			_, _ = w.Write([]byte(c))
			if flusher != nil {
				flusher.Flush()
			}
			time.Sleep(5 * time.Millisecond)
		}
	}))
	return srv, lastPath
}

func TestSynthesizeStream_ReturnsChunkedReader(t *testing.T) {
	t.Parallel()
	chunks := []string{"aaa", "bbb", "ccc"}
	srv, lastPath := chunkedServer(t, chunks)
	defer srv.Close()

	p := NewTTSProvider(Config{
		APIKey:    "test-key",
		BaseURL:   srv.URL,
		VoiceID:   "V1",
		ModelID:   "eleven_v3",
		TimeoutMs: 5000,
	})

	res, err := p.SynthesizeStream(context.Background(), "hello world", audio.TTSOptions{})
	if err != nil {
		t.Fatalf("SynthesizeStream error: %v", err)
	}
	if res.Audio == nil {
		t.Fatal("StreamResult.Audio is nil")
	}
	defer res.Audio.Close()

	if res.Extension != "mp3" {
		t.Errorf("Extension: got %q, want mp3", res.Extension)
	}
	if res.MimeType != "audio/mpeg" {
		t.Errorf("MimeType: got %q, want audio/mpeg", res.MimeType)
	}

	body, err := io.ReadAll(res.Audio)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	want := strings.Join(chunks, "")
	if string(body) != want {
		t.Errorf("body: got %q, want %q", string(body), want)
	}

	// URL must hit the /stream sub-path with mp3 output format.
	path, _ := lastPath.Load().(string)
	if !strings.Contains(path, "/v1/text-to-speech/V1/stream") {
		t.Errorf("path: got %q, want contains /v1/text-to-speech/V1/stream", path)
	}
	if !strings.Contains(path, "output_format=mp3_44100_128") {
		t.Errorf("path: got %q, want contains output_format=mp3_44100_128", path)
	}
}

func TestSynthesizeStream_VoiceOverride(t *testing.T) {
	t.Parallel()
	srv, lastPath := chunkedServer(t, []string{"x"})
	defer srv.Close()

	p := NewTTSProvider(Config{
		APIKey:  "k",
		BaseURL: srv.URL,
		VoiceID: "DEFAULT",
		ModelID: "eleven_v3",
	})

	res, err := p.SynthesizeStream(context.Background(), "hi", audio.TTSOptions{Voice: "OVR"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	res.Audio.Close()

	path, _ := lastPath.Load().(string)
	if !strings.Contains(path, "/v1/text-to-speech/OVR/stream") {
		t.Errorf("voice override not honoured — path=%q", path)
	}
}

func TestSynthesizeStream_CancelContextClosesBody(t *testing.T) {
	t.Parallel()
	// Server writes one chunk then blocks forever.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			_, _ = w.Write([]byte("a"))
			f.Flush()
		}
		<-r.Context().Done()
	}))
	defer srv.Close()

	p := NewTTSProvider(Config{APIKey: "k", BaseURL: srv.URL, VoiceID: "V"})

	ctx, cancel := context.WithCancel(context.Background())
	res, err := p.SynthesizeStream(ctx, "hi", audio.TTSOptions{})
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	// Cancel while body is still open — next Read should fail promptly.
	cancel()
	done := make(chan error, 1)
	go func() {
		buf := make([]byte, 64)
		for {
			_, err := res.Audio.Read(buf)
			if err != nil {
				done <- err
				return
			}
		}
	}()
	select {
	case <-done:
		// ok — read returned an error after cancel.
	case <-time.After(2 * time.Second):
		t.Fatal("Read did not return after ctx cancel")
	}
	res.Audio.Close()
}

func TestSynthesizeStream_OpusFormat(t *testing.T) {
	t.Parallel()
	srv, lastPath := chunkedServer(t, []string{"o"})
	defer srv.Close()

	p := NewTTSProvider(Config{APIKey: "k", BaseURL: srv.URL, VoiceID: "V", ModelID: "eleven_v3"})
	res, err := p.SynthesizeStream(context.Background(), "hi", audio.TTSOptions{Format: "opus"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	res.Audio.Close()

	if res.Extension != "ogg" {
		t.Errorf("Extension: got %q, want ogg", res.Extension)
	}
	if res.MimeType != "audio/ogg" {
		t.Errorf("MimeType: got %q, want audio/ogg", res.MimeType)
	}
	path, _ := lastPath.Load().(string)
	if !strings.Contains(path, "output_format=opus_48000_64") {
		t.Errorf("path: got %q, want contains output_format=opus_48000_64", path)
	}
}

func TestSynthesizeStream_RejectsUnknownModel(t *testing.T) {
	t.Parallel()
	// Server should never be reached — validation runs before HTTP.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected upstream request for invalid model")
	}))
	defer srv.Close()

	p := NewTTSProvider(Config{APIKey: "k", BaseURL: srv.URL, VoiceID: "V"})
	_, err := p.SynthesizeStream(context.Background(), "hi", audio.TTSOptions{Model: "bogus-model"})
	if err == nil {
		t.Fatal("expected error for unknown model, got nil")
	}
}
