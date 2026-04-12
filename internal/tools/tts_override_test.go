package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/tts"
)

func makeTTSManager(providers ...string) *tts.Manager {
	mgr := tts.NewManager(tts.ManagerConfig{Primary: providers[0]})
	for _, name := range providers {
		mgr.RegisterProvider(&fakeTTSProvider{name: name})
	}
	return mgr
}

// fakeTTSProvider is a no-op provider for testing provider resolution.
type fakeTTSProvider struct{ name string }

func (f *fakeTTSProvider) Name() string { return f.name }
func (f *fakeTTSProvider) Synthesize(_ context.Context, _ string, _ tts.Options) (*tts.SynthResult, error) {
	return &tts.SynthResult{Audio: []byte("fake"), Extension: "mp3"}, nil
}

func ctxWithTTSSettings(t *testing.T, override ttsOverride) context.Context {
	t.Helper()
	raw, err := json.Marshal(override)
	if err != nil {
		t.Fatal(err)
	}
	settings := BuiltinToolSettings{"tts": raw}
	return WithBuiltinToolSettings(context.Background(), settings)
}

func TestResolvePrimary_NoOverride(t *testing.T) {
	tool := NewTtsTool(makeTTSManager("openai", "edge"))
	got := tool.resolvePrimary(context.Background(), tool.manager)
	if got != "openai" {
		t.Errorf("got %q, want openai", got)
	}
}

func TestResolvePrimary_TenantOverride(t *testing.T) {
	tool := NewTtsTool(makeTTSManager("openai", "elevenlabs", "edge"))
	ctx := ctxWithTTSSettings(t, ttsOverride{Primary: "elevenlabs"})
	got := tool.resolvePrimary(ctx, tool.manager)
	if got != "elevenlabs" {
		t.Errorf("got %q, want elevenlabs", got)
	}
}

func TestResolvePrimary_UnknownProvider_FallsBack(t *testing.T) {
	tool := NewTtsTool(makeTTSManager("openai", "edge"))
	ctx := ctxWithTTSSettings(t, ttsOverride{Primary: "nonexistent"})
	got := tool.resolvePrimary(ctx, tool.manager)
	if got != "openai" {
		t.Errorf("got %q, want openai (fallback)", got)
	}
}

func TestResolvePrimary_MalformedJSON_FallsBack(t *testing.T) {
	tool := NewTtsTool(makeTTSManager("edge"))
	settings := BuiltinToolSettings{"tts": []byte(`{bad json`)}
	ctx := WithBuiltinToolSettings(context.Background(), settings)
	got := tool.resolvePrimary(ctx, tool.manager)
	if got != "edge" {
		t.Errorf("got %q, want edge (fallback)", got)
	}
}

func TestResolvePrimary_EmptyOverride_FallsBack(t *testing.T) {
	tool := NewTtsTool(makeTTSManager("openai"))
	ctx := ctxWithTTSSettings(t, ttsOverride{})
	got := tool.resolvePrimary(ctx, tool.manager)
	if got != "openai" {
		t.Errorf("got %q, want openai (default)", got)
	}
}
