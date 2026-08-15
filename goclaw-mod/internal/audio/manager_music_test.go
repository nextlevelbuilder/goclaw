package audio

import (
	"context"
	"errors"
	"testing"
)

// ---- Fake providers for testing ----

type fakeMusic struct {
	name string
	res  *AudioResult
	err  error
}

func (f *fakeMusic) Name() string { return f.name }
func (f *fakeMusic) GenerateMusic(_ context.Context, _ MusicOptions) (*AudioResult, error) {
	return f.res, f.err
}

type fakeSFX struct {
	name string
	res  *AudioResult
	err  error
}

func (f *fakeSFX) Name() string { return f.name }
func (f *fakeSFX) GenerateSFX(_ context.Context, _ SFXOptions) (*AudioResult, error) {
	return f.res, f.err
}

// ---- GenerateMusic tests ----

func TestManager_GenerateMusic_primary_succeeds(t *testing.T) {
	m := NewManager(ManagerConfig{})
	want := &AudioResult{Audio: []byte("music"), Extension: "mp3", MimeType: "audio/mpeg"}
	m.RegisterMusic(&fakeMusic{name: "elevenlabs", res: want})

	got, err := m.GenerateMusic(context.Background(), MusicOptions{Prompt: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got.Audio) != string(want.Audio) {
		t.Errorf("audio: want %q, got %q", want.Audio, got.Audio)
	}
}

func TestManager_GenerateMusic_primary_fails_secondary_succeeds(t *testing.T) {
	m := NewManager(ManagerConfig{})
	want := &AudioResult{Audio: []byte("fallback_music"), Extension: "mp3", MimeType: "audio/mpeg"}
	m.RegisterMusic(&fakeMusic{name: "elevenlabs", err: errors.New("elevenlabs down")})
	m.RegisterMusic(&fakeMusic{name: "minimax", res: want})

	got, err := m.GenerateMusic(context.Background(), MusicOptions{Prompt: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got.Audio) != string(want.Audio) {
		t.Errorf("audio: want %q, got %q", want.Audio, got.Audio)
	}
}

func TestManager_GenerateMusic_all_fail(t *testing.T) {
	m := NewManager(ManagerConfig{})
	m.RegisterMusic(&fakeMusic{name: "elevenlabs", err: errors.New("elevenlabs down")})
	m.RegisterMusic(&fakeMusic{name: "minimax", err: errors.New("minimax down")})

	_, err := m.GenerateMusic(context.Background(), MusicOptions{Prompt: "test"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !containsStr(err.Error(), "all music providers failed") {
		t.Errorf("error should mention 'all music providers failed': %v", err)
	}
}

func TestManager_GenerateMusic_no_providers(t *testing.T) {
	m := NewManager(ManagerConfig{})

	_, err := m.GenerateMusic(context.Background(), MusicOptions{Prompt: "test"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !containsStr(err.Error(), "no music providers") {
		t.Errorf("error should mention 'no music providers': %v", err)
	}
}

// ---- GenerateSFX tests ----

func TestManager_GenerateSFX_elevenlabs_succeeds(t *testing.T) {
	m := NewManager(ManagerConfig{})
	want := &AudioResult{Audio: []byte("sfx_bytes"), Extension: "mp3", MimeType: "audio/mpeg"}
	m.RegisterSFX(&fakeSFX{name: "elevenlabs", res: want})

	got, err := m.GenerateSFX(context.Background(), SFXOptions{Prompt: "explosion"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got.Audio) != string(want.Audio) {
		t.Errorf("audio: want %q, got %q", want.Audio, got.Audio)
	}
}

func TestManager_GenerateSFX_elevenlabs_fails_fallback(t *testing.T) {
	m := NewManager(ManagerConfig{})
	want := &AudioResult{Audio: []byte("fallback_sfx"), Extension: "mp3", MimeType: "audio/mpeg"}
	m.RegisterSFX(&fakeSFX{name: "elevenlabs", err: errors.New("elevenlabs sfx down")})
	m.RegisterSFX(&fakeSFX{name: "other_sfx", res: want})

	got, err := m.GenerateSFX(context.Background(), SFXOptions{Prompt: "click"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got.Audio) != string(want.Audio) {
		t.Errorf("audio: want %q, got %q", want.Audio, got.Audio)
	}
}

func containsStr(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && func() bool {
		for i := 0; i <= len(s)-len(sub); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	}())
}
