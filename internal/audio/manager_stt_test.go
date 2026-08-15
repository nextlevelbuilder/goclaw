package audio

import (
	"context"
	"errors"
	"testing"
)

// mockSTT is a test STTProvider that either returns a fixed result or error.
type mockSTT struct {
	name   string
	result *TranscriptResult
	err    error
}

func (m *mockSTT) Name() string { return m.name }
func (m *mockSTT) Transcribe(_ context.Context, _ STTInput, _ STTOptions) (*TranscriptResult, error) {
	return m.result, m.err
}

func newTestManager() *Manager {
	return NewManager(ManagerConfig{})
}

// Case 1: chain ["elevenlabs","proxy"] — Scribe succeeds, result.Provider="elevenlabs".
func TestManager_Transcribe_ChainSuccessFirst(t *testing.T) {
	m := newTestManager()
	m.RegisterSTT(&mockSTT{
		name:   "elevenlabs",
		result: &TranscriptResult{Text: "hello", Provider: "elevenlabs"},
	})
	m.RegisterSTT(&mockSTT{
		name:   "proxy",
		result: &TranscriptResult{Text: "world", Provider: "proxy"},
	})
	m.SetSTTChain([]string{"elevenlabs", "proxy"})

	res, err := m.Transcribe(context.Background(), STTInput{}, STTOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Provider != "elevenlabs" {
		t.Errorf("expected provider 'elevenlabs', got %q", res.Provider)
	}
	if res.Text != "hello" {
		t.Errorf("expected text 'hello', got %q", res.Text)
	}
}

// Case 2: Scribe fails → proxy succeeds → result.Provider="proxy".
func TestManager_Transcribe_FallsToProxy(t *testing.T) {
	m := newTestManager()
	m.RegisterSTT(&mockSTT{
		name: "elevenlabs",
		err:  errors.New("scribe 500"),
	})
	m.RegisterSTT(&mockSTT{
		name:   "proxy",
		result: &TranscriptResult{Text: "fallback", Provider: "proxy"},
	})
	m.SetSTTChain([]string{"elevenlabs", "proxy"})

	res, err := m.Transcribe(context.Background(), STTInput{}, STTOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Provider != "proxy" {
		t.Errorf("expected provider 'proxy', got %q", res.Provider)
	}
}

// Case 3: both fail → ErrAllSTTProvidersFailed.
func TestManager_Transcribe_AllFail(t *testing.T) {
	m := newTestManager()
	m.RegisterSTT(&mockSTT{name: "elevenlabs", err: errors.New("scribe error")})
	m.RegisterSTT(&mockSTT{name: "proxy", err: errors.New("proxy error")})
	m.SetSTTChain([]string{"elevenlabs", "proxy"})

	_, err := m.Transcribe(context.Background(), STTInput{}, STTOptions{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrAllSTTProvidersFailed) {
		t.Errorf("expected ErrAllSTTProvidersFailed, got: %v", err)
	}
}

// Case 4: empty chain → error.
func TestManager_Transcribe_EmptyChain(t *testing.T) {
	m := newTestManager()
	m.SetSTTChain([]string{})

	_, err := m.Transcribe(context.Background(), STTInput{}, STTOptions{})
	if err == nil {
		t.Fatal("expected error for empty chain, got nil")
	}
	if !errors.Is(err, ErrAllSTTProvidersFailed) {
		t.Errorf("expected ErrAllSTTProvidersFailed, got: %v", err)
	}
}

// Case 5: unknown provider in chain is skipped with warn; known one succeeds.
func TestManager_Transcribe_UnknownSkipped(t *testing.T) {
	m := newTestManager()
	m.RegisterSTT(&mockSTT{
		name:   "proxy",
		result: &TranscriptResult{Text: "ok", Provider: "proxy"},
	})
	m.SetSTTChain([]string{"unknown_provider", "proxy"})

	res, err := m.Transcribe(context.Background(), STTInput{}, STTOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Provider != "proxy" {
		t.Errorf("expected provider 'proxy', got %q", res.Provider)
	}
}

// Case 6: channel override wins over tenant chain.
func TestManager_Transcribe_ChannelOverrideWins(t *testing.T) {
	m := newTestManager()

	// Register tenant-level provider.
	m.RegisterSTT(&mockSTT{
		name:   "elevenlabs",
		result: &TranscriptResult{Text: "tenant", Provider: "elevenlabs"},
	})
	m.SetSTTChain([]string{"elevenlabs"})

	// Register channel-scoped proxy that wins for "telegram".
	channelProxy := &mockSTT{
		name:   "proxy",
		result: &TranscriptResult{Text: "channel-override", Provider: "proxy"},
	}
	m.RegisterChannelSTT("telegram", channelProxy)

	ctx := WithChannel(context.Background(), "telegram")
	res, err := m.Transcribe(ctx, STTInput{}, STTOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Text != "channel-override" {
		t.Errorf("expected channel override result, got %q", res.Text)
	}
}
