package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/tts"
)

type tenantRoutingTTSProvider struct {
	name  string
	calls int
	err   error
	voice string
	model string
}

func (p *tenantRoutingTTSProvider) Name() string { return p.name }

func (p *tenantRoutingTTSProvider) Synthesize(_ context.Context, _ string, opts tts.Options) (*tts.SynthResult, error) {
	p.calls++
	p.voice = opts.Voice
	p.model = opts.Model
	if p.err != nil {
		return nil, p.err
	}
	return &tts.SynthResult{Audio: []byte("audio"), Extension: "mp3", MimeType: "audio/mpeg"}, nil
}

func TestTtsTool_UsesSystemConfigForResolvedTenantProvider(t *testing.T) {
	t.Parallel()

	globalProvider := &tenantRoutingTTSProvider{name: "openai"}
	tenantProvider := &tenantRoutingTTSProvider{name: "edge"}
	mgr := newTenantRoutingManager("openai", globalProvider)
	mgr.SetTenantResolver(func(context.Context) (audio.TTSProvider, string, audio.AutoMode, error) {
		return tenantProvider, "edge", audio.AutoOff, nil
	})

	tool := NewTtsTool(mgr)
	tool.SetSystemConfigStore(&fakeSystemConfigStore{data: map[string]string{
		"tts.openai.voice": "alloy",
		"tts.openai.model": "openai-model",
		"tts.edge.voice":   "vi-VN-HoaiMyNeural",
		"tts.edge.model":   "edge-model",
	}})

	result := tool.Execute(WithToolWorkspace(context.Background(), t.TempDir()), map[string]any{
		"text": "hello from tenant provider",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
	if tenantProvider.voice != "vi-VN-HoaiMyNeural" {
		t.Fatalf("tenant voice = %q, want %q", tenantProvider.voice, "vi-VN-HoaiMyNeural")
	}
	if tenantProvider.model != "edge-model" {
		t.Fatalf("tenant model = %q, want %q", tenantProvider.model, "edge-model")
	}
	if globalProvider.calls != 0 {
		t.Fatalf("global provider calls = %d, want 0", globalProvider.calls)
	}
}

func newTenantRoutingManager(primary string, providers ...*tenantRoutingTTSProvider) *tts.Manager {
	mgr := tts.NewManager(tts.ManagerConfig{Primary: primary})
	for _, provider := range providers {
		mgr.RegisterTTS(provider)
	}
	return mgr
}

func TestTtsTool_UsesTenantProviderWhenProviderOmitted(t *testing.T) {
	t.Parallel()

	edgeProvider := &tenantRoutingTTSProvider{name: "edge"}
	geminiProvider := &tenantRoutingTTSProvider{name: "gemini"}
	mgr := newTenantRoutingManager("edge", edgeProvider)
	mgr.SetTenantResolver(func(context.Context) (audio.TTSProvider, string, audio.AutoMode, error) {
		return geminiProvider, "gemini", audio.AutoOff, nil
	})

	tool := NewTtsTool(mgr)
	result := tool.Execute(context.Background(), map[string]any{
		"text": "hello from tenant provider",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
	if geminiProvider.calls != 1 {
		t.Fatalf("tenant gemini calls = %d, want 1", geminiProvider.calls)
	}
	if edgeProvider.calls != 0 {
		t.Fatalf("global edge calls = %d, want 0", edgeProvider.calls)
	}
}

func TestTtsTool_ExplicitTenantProviderWhenMissingFromGlobalManager(t *testing.T) {
	t.Parallel()

	edgeProvider := &tenantRoutingTTSProvider{name: "edge"}
	geminiProvider := &tenantRoutingTTSProvider{name: "gemini"}
	mgr := newTenantRoutingManager("edge", edgeProvider)
	mgr.SetTenantResolver(func(context.Context) (audio.TTSProvider, string, audio.AutoMode, error) {
		return geminiProvider, "gemini", audio.AutoOff, nil
	})

	tool := NewTtsTool(mgr)
	result := tool.Execute(context.Background(), map[string]any{
		"text":     "hello from explicit tenant provider",
		"provider": "gemini",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
	if geminiProvider.calls != 1 {
		t.Fatalf("tenant gemini calls = %d, want 1", geminiProvider.calls)
	}
	if edgeProvider.calls != 0 {
		t.Fatalf("global edge calls = %d, want 0", edgeProvider.calls)
	}
}

func TestTtsTool_TenantProviderFailureFallsBackWhenProviderOmitted(t *testing.T) {
	t.Parallel()

	edgeProvider := &tenantRoutingTTSProvider{name: "edge"}
	geminiProvider := &tenantRoutingTTSProvider{name: "gemini", err: errors.New("tenant unavailable")}
	mgr := newTenantRoutingManager("edge", edgeProvider)
	mgr.SetTenantResolver(func(context.Context) (audio.TTSProvider, string, audio.AutoMode, error) {
		return geminiProvider, "gemini", audio.AutoOff, nil
	})

	tool := NewTtsTool(mgr)
	tool.SetSystemConfigStore(&fakeSystemConfigStore{data: map[string]string{
		"tts.edge.voice":   "vi-VN-HoaiMyNeural",
		"tts.edge.model":   "edge-model",
		"tts.gemini.voice": "Kore",
		"tts.gemini.model": "gemini-model",
	}})
	result := tool.Execute(WithToolWorkspace(context.Background(), t.TempDir()), map[string]any{
		"text": "hello with tenant fallback",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
	if geminiProvider.calls != 1 {
		t.Fatalf("tenant gemini calls = %d, want 1", geminiProvider.calls)
	}
	if edgeProvider.calls != 1 {
		t.Fatalf("global edge calls = %d, want 1", edgeProvider.calls)
	}
	if geminiProvider.voice != "Kore" || geminiProvider.model != "gemini-model" {
		t.Fatalf("tenant options = voice %q model %q, want tenant system config", geminiProvider.voice, geminiProvider.model)
	}
	if edgeProvider.voice != "vi-VN-HoaiMyNeural" || edgeProvider.model != "edge-model" {
		t.Fatalf("fallback options = voice %q model %q, want global system config", edgeProvider.voice, edgeProvider.model)
	}
}

func TestTtsTool_FallbackResolvesOptionsForEveryActualProvider(t *testing.T) {
	t.Parallel()

	managerPrimary := &tenantRoutingTTSProvider{name: "edge"}
	preferred := &tenantRoutingTTSProvider{name: "openai", err: errors.New("openai unavailable")}
	tenantProvider := &tenantRoutingTTSProvider{name: "gemini", err: errors.New("tenant provider unavailable")}
	mgr := newTenantRoutingManager("edge", managerPrimary, preferred)
	mgr.SetTenantResolver(func(context.Context) (audio.TTSProvider, string, audio.AutoMode, error) {
		return tenantProvider, "gemini", audio.AutoOff, nil
	})

	tool := NewTtsTool(mgr)
	tool.SetSystemConfigStore(&fakeSystemConfigStore{data: map[string]string{
		"tts.gemini.voice": "Kore",
		"tts.gemini.model": "gemini-model",
		"tts.openai.voice": "alloy",
		"tts.openai.model": "openai-model",
		"tts.edge.voice":   "vi-VN-HoaiMyNeural",
		"tts.edge.model":   "edge-model",
	}})
	ctx := ctxWithTTSSettings(t, ttsOverride{Primary: "openai"})
	ctx = WithToolWorkspace(ctx, t.TempDir())

	result := tool.Execute(ctx, map[string]any{"text": "fallback across providers"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
	if tenantProvider.voice != "Kore" || tenantProvider.model != "gemini-model" {
		t.Fatalf("tenant options = voice %q model %q, want gemini config", tenantProvider.voice, tenantProvider.model)
	}
	if preferred.voice != "alloy" || preferred.model != "openai-model" {
		t.Fatalf("preferred options = voice %q model %q, want openai config", preferred.voice, preferred.model)
	}
	if managerPrimary.voice != "vi-VN-HoaiMyNeural" || managerPrimary.model != "edge-model" {
		t.Fatalf("secondary options = voice %q model %q, want edge config", managerPrimary.voice, managerPrimary.model)
	}
	if preferred.calls != 1 || managerPrimary.calls != 1 {
		t.Fatalf("fallback calls = preferred %d secondary %d, want one each", preferred.calls, managerPrimary.calls)
	}
}

func TestTtsTool_ExplicitProviderMismatchStillErrors(t *testing.T) {
	t.Parallel()

	geminiProvider := &tenantRoutingTTSProvider{name: "gemini"}
	mgr := newTenantRoutingManager("edge", &tenantRoutingTTSProvider{name: "edge"})
	mgr.SetTenantResolver(func(context.Context) (audio.TTSProvider, string, audio.AutoMode, error) {
		return geminiProvider, "gemini", audio.AutoOff, nil
	})

	tool := NewTtsTool(mgr)
	result := tool.Execute(context.Background(), map[string]any{
		"text":     "hello from mismatched provider",
		"provider": "openai",
	})

	if !result.IsError {
		t.Fatalf("expected error, got %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "tts provider not found: openai") {
		t.Fatalf("error = %q, want provider not found for openai", result.ForLLM)
	}
	if geminiProvider.calls != 0 {
		t.Fatalf("tenant gemini calls = %d, want 0", geminiProvider.calls)
	}
}

func TestTtsTool_GlobalFallbackStillWorksWithoutTenantProvider(t *testing.T) {
	t.Parallel()

	edgeProvider := &tenantRoutingTTSProvider{name: "edge"}
	mgr := newTenantRoutingManager("edge", edgeProvider)

	tool := NewTtsTool(mgr)
	result := tool.Execute(context.Background(), map[string]any{
		"text": "hello from global provider",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
	if edgeProvider.calls != 1 {
		t.Fatalf("global edge calls = %d, want 1", edgeProvider.calls)
	}
}
