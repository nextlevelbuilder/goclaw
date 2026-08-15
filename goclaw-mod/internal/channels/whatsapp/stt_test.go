package whatsapp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
)

// stubSTTProvider is a minimal in-process STTProvider for unit tests.
type stubSTTProvider struct {
	name    string
	result  *audio.TranscriptResult
	err     error
	delay   time.Duration // simulate slow provider
	called  int
}

func (s *stubSTTProvider) Name() string { return s.name }

func (s *stubSTTProvider) Transcribe(ctx context.Context, in audio.STTInput, opts audio.STTOptions) (*audio.TranscriptResult, error) {
	s.called++
	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return s.result, s.err
}

// newTestChannel builds a minimal Channel with audioMgr for unit tests.
func newTestChannelWithAudio(audioMgr *audio.Manager) *Channel {
	return &Channel{audioMgr: audioMgr}
}

// newAudioMgr creates an audio.Manager with one stub STT provider.
func newAudioMgr(stub *stubSTTProvider) *audio.Manager {
	mgr := audio.NewManager(audio.ManagerConfig{})
	mgr.RegisterSTT(stub)
	mgr.SetSTTChain([]string{stub.Name()})
	return mgr
}

// Case 1: opt-in enabled, success — transcribeVoice calls Transcribe and returns transcript.
func TestTranscribeVoice_OptInSuccess_CallsManager(t *testing.T) {
	stub := &stubSTTProvider{name: "stub", result: &audio.TranscriptResult{Text: "hello world"}}
	mgr := newAudioMgr(stub)
	ch := newTestChannelWithAudio(mgr)
	settings := sttSettings{WhatsappEnabled: true}

	got := ch.transcribeVoice(context.Background(), "/tmp/voice.ogg", "audio/ogg", "en", settings)

	if stub.called != 1 {
		t.Errorf("expected Transcribe called once, got %d", stub.called)
	}
	if got != "hello world" {
		t.Errorf("expected %q, got %q", "hello world", got)
	}
}

// Case 2: opt-in + Transcribe succeeds → returns transcript text.
func TestTranscribeVoice_Success_ReturnsTranscript(t *testing.T) {
	stub := &stubSTTProvider{name: "stub", result: &audio.TranscriptResult{Text: "test transcript"}}
	mgr := newAudioMgr(stub)
	ch := newTestChannelWithAudio(mgr)
	settings := sttSettings{WhatsappEnabled: true}

	got := ch.transcribeVoice(context.Background(), "/tmp/audio.ogg", "audio/ogg", "", settings)

	if got != "test transcript" {
		t.Errorf("expected %q, got %q", "test transcript", got)
	}
}

// Case 3: STT error → fallback "[Voice message]" returned.
func TestTranscribeVoice_STTError_ReturnsFallback(t *testing.T) {
	stub := &stubSTTProvider{name: "stub", err: errors.New("provider unavailable")}
	mgr := newAudioMgr(stub)
	ch := newTestChannelWithAudio(mgr)
	settings := sttSettings{WhatsappEnabled: true}

	got := ch.transcribeVoice(context.Background(), "/tmp/voice.ogg", "audio/ogg", "en", settings)

	fallback := i18n.T("en", i18n.MsgVoiceMessageFallback)
	if got != fallback {
		t.Errorf("expected fallback %q, got %q", fallback, got)
	}
}

// Case 4: ctx DeadlineExceeded → fallback, no crash.
func TestTranscribeVoice_ContextDeadlineExceeded_ReturnsFallback(t *testing.T) {
	// Stub that blocks until ctx done — simulates slow Scribe.
	stub := &stubSTTProvider{name: "stub", delay: 5 * time.Second}
	mgr := newAudioMgr(stub)
	ch := newTestChannelWithAudio(mgr)
	settings := sttSettings{WhatsappEnabled: true}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	got := ch.transcribeVoice(ctx, "/tmp/voice.ogg", "audio/ogg", "en", settings)

	fallback := i18n.T("en", i18n.MsgVoiceMessageFallback)
	if got != fallback {
		t.Errorf("expected fallback %q, got %q", fallback, got)
	}
}

// Case 5: Manager has zero STT providers → ErrAllSTTProvidersFailed → fallback.
func TestTranscribeVoice_NoProviders_ReturnsFallback(t *testing.T) {
	mgr := audio.NewManager(audio.ManagerConfig{})
	// No providers registered — Transcribe returns ErrAllSTTProvidersFailed.
	ch := newTestChannelWithAudio(mgr)
	settings := sttSettings{WhatsappEnabled: true}

	got := ch.transcribeVoice(context.Background(), "/tmp/voice.ogg", "audio/ogg", "", settings)

	fallback := i18n.T("", i18n.MsgVoiceMessageFallback)
	if got != fallback {
		t.Errorf("expected fallback %q, got %q", fallback, got)
	}
}

// Case 6: ctx Done before 10s wall clock → Transcribe aborts, returns fallback.
func TestTranscribeVoice_CtxDoneBeforeTimeout_AbortsCleanly(t *testing.T) {
	stub := &stubSTTProvider{name: "stub", delay: 30 * time.Second}
	mgr := newAudioMgr(stub)
	ch := newTestChannelWithAudio(mgr)
	settings := sttSettings{WhatsappEnabled: true}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately cancelled

	got := ch.transcribeVoice(ctx, "/tmp/voice.ogg", "audio/ogg", "vi", settings)

	fallback := i18n.T("vi", i18n.MsgVoiceMessageFallback)
	if got != fallback {
		t.Errorf("expected fallback %q, got %q", fallback, got)
	}
}

// Case 7: whatsapp_enabled=false (default opt-out) → Transcribe never called, fallback only.
func TestTranscribeVoice_OptOut_NoTranscribeCall_ReturnsFallback(t *testing.T) {
	stub := &stubSTTProvider{name: "stub", result: &audio.TranscriptResult{Text: "should not appear"}}
	mgr := newAudioMgr(stub)
	ch := newTestChannelWithAudio(mgr)
	settings := sttSettings{WhatsappEnabled: false} // opt-out (default)

	got := ch.transcribeVoice(context.Background(), "/tmp/voice.ogg", "audio/ogg", "en", settings)

	if stub.called != 0 {
		t.Errorf("expected Transcribe NOT called, got %d calls", stub.called)
	}
	fallback := i18n.T("en", i18n.MsgVoiceMessageFallback)
	if got != fallback {
		t.Errorf("expected fallback %q, got %q", fallback, got)
	}
}
