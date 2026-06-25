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
}

func (p *tenantRoutingTTSProvider) Name() string { return p.name }

func (p *tenantRoutingTTSProvider) Synthesize(_ context.Context, _ string, _ tts.Options) (*tts.SynthResult, error) {
	p.calls++
	if p.err != nil {
		return nil, p.err
	}
	return &tts.SynthResult{Audio: []byte("audio"), Extension: "mp3", MimeType: "audio/mpeg"}, nil
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
	result := tool.Execute(context.Background(), map[string]any{
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
