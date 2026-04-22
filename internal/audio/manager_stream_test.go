package audio_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
)

type bufferedOnlyTTS struct{}

func (bufferedOnlyTTS) Name() string { return "buffered-only" }
func (bufferedOnlyTTS) Synthesize(_ context.Context, _ string, _ audio.TTSOptions) (*audio.SynthResult, error) {
	return &audio.SynthResult{Extension: "mp3", MimeType: "audio/mpeg"}, nil
}

type streamingTTS struct{ name string }

func (s streamingTTS) Name() string { return s.name }
func (s streamingTTS) Synthesize(_ context.Context, _ string, _ audio.TTSOptions) (*audio.SynthResult, error) {
	return &audio.SynthResult{Extension: "mp3", MimeType: "audio/mpeg"}, nil
}
func (s streamingTTS) SynthesizeStream(_ context.Context, text string, _ audio.TTSOptions) (*audio.StreamResult, error) {
	return &audio.StreamResult{
		Audio:     io.NopCloser(strings.NewReader("STREAM:" + text)),
		Extension: "mp3",
		MimeType:  "audio/mpeg",
	}, nil
}

func TestManager_SynthesizeStream_UnsupportedProviderReturnsSentinel(t *testing.T) {
	t.Parallel()
	mgr := audio.NewManager(audio.ManagerConfig{})
	mgr.RegisterTTS(bufferedOnlyTTS{})

	_, err := mgr.SynthesizeStream(context.Background(), "hi", audio.TTSOptions{})
	if !errors.Is(err, audio.ErrStreamingNotSupported) {
		t.Errorf("got err=%v, want ErrStreamingNotSupported", err)
	}
}

func TestManager_SynthesizeStream_DispatchesToStreamingProvider(t *testing.T) {
	t.Parallel()
	mgr := audio.NewManager(audio.ManagerConfig{})
	mgr.RegisterTTS(streamingTTS{name: "el"})

	res, err := mgr.SynthesizeStream(context.Background(), "hello", audio.TTSOptions{})
	if err != nil {
		t.Fatalf("SynthesizeStream error: %v", err)
	}
	defer res.Audio.Close()

	body, err := io.ReadAll(res.Audio)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got, want := string(body), "STREAM:hello"; got != want {
		t.Errorf("body: got %q, want %q", got, want)
	}
}

func TestManager_SynthesizeStream_NoPrimaryReturnsError(t *testing.T) {
	t.Parallel()
	mgr := audio.NewManager(audio.ManagerConfig{Primary: "missing"})
	_, err := mgr.SynthesizeStream(context.Background(), "hi", audio.TTSOptions{})
	if err == nil {
		t.Fatal("expected error when primary not registered")
	}
	if errors.Is(err, audio.ErrStreamingNotSupported) {
		t.Errorf("got ErrStreamingNotSupported, expected provider-not-found")
	}
}
