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

// TestResolvePrimary_LegacyConfig verifies that pre-existing
// builtin_tool_tenant_configs[tts] rows (legacy from before the /tts page
// migration) are handled gracefully — no crash, no Error-level log.
func TestResolvePrimary_LegacyConfig(t *testing.T) {
	cases := []struct {
		name        string
		rawSettings BuiltinToolSettings // simulates row from builtin_tool_tenant_configs
		want        string              // expected resolved provider
	}{
		{
			name:        "valid legacy JSON returns configured provider",
			rawSettings: BuiltinToolSettings{"tts": []byte(`{"primary":"elevenlabs"}`)},
			want:        "elevenlabs",
		},
		{
			name:        "malformed JSON falls back to manager primary",
			rawSettings: BuiltinToolSettings{"tts": []byte(`not-an-object`)},
			want:        "openai",
		},
		{
			name:        "empty settings map falls back to manager primary",
			rawSettings: BuiltinToolSettings{},
			want:        "openai",
		},
		{
			name:        "missing tts key falls back to manager primary",
			rawSettings: BuiltinToolSettings{"stt": []byte(`{"primary":"whisper"}`)},
			want:        "openai",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tool := NewTtsTool(makeTTSManager("openai", "elevenlabs", "edge"))
			ctx := WithBuiltinToolSettings(context.Background(), tc.rawSettings)
			got := tool.resolvePrimary(ctx, tool.manager)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
