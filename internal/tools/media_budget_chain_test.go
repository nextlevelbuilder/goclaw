package tools

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/mediabudget"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	usagecaps "github.com/nextlevelbuilder/goclaw/internal/usage/caps"
)

// countingChatProvider answers every Chat as if it were healthy, so a test
// that sees zero calls knows the budget stopped the payload and not the
// provider being unavailable.
type countingChatProvider struct {
	name  string
	calls int
}

func (p *countingChatProvider) Chat(context.Context, providers.ChatRequest) (*providers.ChatResponse, error) {
	p.calls++
	return &providers.ChatResponse{Content: "analysed"}, nil
}

func (p *countingChatProvider) ChatStream(ctx context.Context, req providers.ChatRequest, _ func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	return p.Chat(ctx, req)
}

func (p *countingChatProvider) DefaultModel() string { return "m" }
func (p *countingChatProvider) Name() string         { return p.name }

// chainOver registers each named provider and returns the chain the tools build
// for them, mirroring videoProviderPriority / documentProviderPriority order.
func chainOver(t *testing.T, names ...string) (*providers.Registry, []MediaProviderEntry, []*countingChatProvider) {
	t.Helper()
	registry := providers.NewRegistry(func(context.Context) uuid.UUID { return uuid.Nil })
	chain := make([]MediaProviderEntry, 0, len(names))
	fakes := make([]*countingChatProvider, 0, len(names))
	for _, name := range names {
		fake := &countingChatProvider{name: name}
		registry.Register(fake)
		fakes = append(fakes, fake)
		entry := MediaProviderEntry{Provider: name, Model: "m", Enabled: true}
		entry.applyDefaults()
		chain = append(chain, entry)
	}
	return registry, chain, fakes
}

func assertNoProviderCalled(t *testing.T, fakes []*countingChatProvider) {
	t.Helper()
	for _, fake := range fakes {
		if fake.calls != 0 {
			t.Fatalf("provider %q was called %d time(s) with a refused payload, want 0", fake.name, fake.calls)
		}
	}
}

// TestExecuteWithChain_RefusedVideoReachesNoProviderInTheChain is the
// regression test for the bypass: a refusal used to read as "this provider
// failed", so the chain fell through to an entry that charged the payload a
// flat unit and sent 40 MB of video to it.
func TestExecuteWithChain_RefusedVideoReachesNoProviderInTheChain(t *testing.T) {
	stubMediaProbes(t, 3600, 1) // an hour of video: 946,800 tokens

	registry, chain, fakes := chainOver(t, videoProviderPriority...)
	tool := NewReadVideoTool(registry, nil)

	ctx := withToolAgentBudget(20_000, 8_192)
	for i := range chain {
		chain[i].Params = map[string]any{
			"prompt":            "describe this video",
			"data":              make([]byte, 40*1024*1024),
			"mime":              "video/mp4",
			videoLocalPathParam: "/does/not/matter",
		}
	}

	_, err := ExecuteWithChain(ctx, chain, registry, tool.callProvider)
	var windowErr *usagecaps.ContextWindowExceededError
	if !errors.As(err, &windowErr) {
		t.Fatalf("expected the whole chain to refuse on the context window, got %T: %v", err, err)
	}
	assertNoProviderCalled(t, fakes)
}

// TestExecuteWithChain_RefusedDocumentReachesNoProviderInTheChain is the same
// bypass for read_document, whose chain is five entries long.
func TestExecuteWithChain_RefusedDocumentReachesNoProviderInTheChain(t *testing.T) {
	stubMediaProbes(t, 1, 1000) // the provider's 1000-page maximum: 258,000 tokens

	registry, chain, fakes := chainOver(t, documentProviderPriority...)
	tool := NewReadDocumentTool(registry, nil)

	ctx := withToolAgentBudget(20_000, 8_192)
	for i := range chain {
		chain[i].Params = map[string]any{
			"prompt":               "summarise this",
			"data":                 []byte("%PDF-1.7 crafted"),
			"mime":                 "application/pdf",
			documentLocalPathParam: "/does/not/matter",
		}
	}

	_, err := ExecuteWithChain(ctx, chain, registry, tool.callProvider)
	var windowErr *usagecaps.ContextWindowExceededError
	if !errors.As(err, &windowErr) {
		t.Fatalf("expected the whole chain to refuse on the context window, got %T: %v", err, err)
	}
	assertNoProviderCalled(t, fakes)
}

// TestReadDocument_RefusedPDFNeverReachesItsProvider covers the non-Gemini
// branch on its own: it builds a base64 document the token counter cannot see,
// so without its own media charge it is the cheap way past the gate.
func TestReadDocument_RefusedPDFNeverReachesItsProvider(t *testing.T) {
	stubMediaProbes(t, 1, 1000)

	registry, _, fakes := chainOver(t, "openrouter")
	tool := NewReadDocumentTool(registry, nil)

	ctx := withToolAgentBudget(20_000, 8_192)
	_, _, err := tool.callProvider(ctx, nil, "openrouter", "m", map[string]any{
		"prompt":               "summarise this",
		"data":                 []byte("%PDF-1.7 crafted"),
		"mime":                 "application/pdf",
		documentLocalPathParam: "/does/not/matter",
	})
	if !isBudgetRefusal(err) {
		t.Fatalf("expected a terminal budget refusal, got %T: %v", err, err)
	}
	assertNoProviderCalled(t, fakes)
}

// TestReadAudio_RefusedAudioNeverReachesItsProvider proves the audio tool
// stops before transport too. The provider here is an HTTP endpoint, so a hit
// count of zero is proof no request left the process.
func TestReadAudio_RefusedAudioNeverReachesItsProvider(t *testing.T) {
	stubMediaProbes(t, 7200, 1) // two hours of audio: 230,400 tokens

	hits := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	tool := NewReadAudioTool(nil, nil)
	cp := &mockCredentialProvider{apiKey: "test-key", apiBase: ts.URL}
	ctx := withToolAgentBudget(200_000, 8_192)

	_, _, err := tool.callProvider(ctx, cp, "openai", "whisper-1", map[string]any{
		"prompt":            "transcribe this",
		"data":              make([]byte, 4_000_000),
		"mime":              "audio/mpeg",
		audioLocalPathParam: "/does/not/matter",
		"_provider_type":    "openai",
	})
	if !isBudgetRefusal(err) {
		t.Fatalf("expected a terminal budget refusal, got %T: %v", err, err)
	}
	if hits != 0 {
		t.Fatalf("the transcription endpoint was contacted %d time(s), want 0", hits)
	}
}

// TestBudgetRefusal_ProviderFailureStillFallsThrough guards the other
// direction: an ordinary provider error must keep the fallback chain working.
func TestBudgetRefusal_ProviderFailureStillFallsThrough(t *testing.T) {
	registry, chain, _ := chainOver(t, "first", "second")

	calls := 0
	fn := func(_ context.Context, _ credentialProvider, providerName, _ string, _ map[string]any) ([]byte, *providers.Usage, error) {
		calls++
		if providerName == "first" {
			return nil, nil, errors.New("upstream 502")
		}
		return []byte("ok"), nil, nil
	}

	result, err := ExecuteWithChain(context.Background(), chain, registry, fn)
	if err != nil {
		t.Fatalf("a plain provider failure must still fall through, got %v", err)
	}
	if result.Provider != "second" || calls != 2 {
		t.Fatalf("fell through to %q after %d call(s), want second after 2", result.Provider, calls)
	}
}

// TestExecuteWithChain_UnmeasurableMediaIsTerminal covers the refusal class the
// window guard never sees: nothing could be measured, so no number exists to
// compare against a window, and the chain must still stop.
func TestExecuteWithChain_UnmeasurableMediaIsTerminal(t *testing.T) {
	registry, chain, _ := chainOver(t, "first", "second")

	calls := 0
	fn := func(context.Context, credentialProvider, string, string, map[string]any) ([]byte, *providers.Usage, error) {
		calls++
		return nil, nil, refuseUnpriceableMedia(mediabudget.KindVideo, mediabudget.ErrNoProbeableBytes)
	}

	_, err := ExecuteWithChain(context.Background(), chain, registry, fn)
	if !errors.Is(err, mediabudget.ErrDurationUnmeasurable) {
		t.Fatalf("expected the unmeasurable refusal to surface, got %T: %v", err, err)
	}
	if calls != 1 {
		t.Fatalf("the chain made %d call(s) after a budget refusal, want 1", calls)
	}
}

// TestReserveToolLLMUsageWithMedia_MediaChargeTripsTenantTokenCap joins the two
// halves the other tests cover separately: a charge produced by mediabudget is
// what the tenant's token cap sees, through a real usagecaps.Service.
func TestReserveToolLLMUsageWithMedia_MediaChargeTripsTenantTokenCap(t *testing.T) {
	stubMediaProbes(t, 3600, 1) // 946,800 tokens of video

	tenantID := uuid.New()
	policy := store.UsageCapPolicy{ID: uuid.New(), TenantID: tenantID, MaxTokens: int64Ptr(100_000), Enabled: true}
	capStore := &fakeToolUsageCapStore{policies: []store.UsageCapPolicy{policy}, enforceTokenCaps: true}
	providerStore := &fakeToolProviderStore{provider: &store.LLMProviderData{
		BaseModel:    store.BaseModel{ID: uuid.New()},
		Name:         "openrouter",
		ProviderType: store.ProviderOpenRouter,
		APIKey:       "sk-test",
	}}
	svc := usagecaps.NewService(capStore, providerStore)

	// The window is large enough to admit the charge, so only the tenant cap
	// can refuse it.
	ctx := store.WithTenantID(withToolAgentBudget(2_000_000, 8), tenantID)
	model := "some/model"

	if _, err := reserveToolLLMUsage(ctx, svc, "read_video", "openrouter", model, mediaTestRequest(model)); err != nil {
		t.Fatalf("the same request without media must pass the tenant cap, got %v", err)
	}

	_, err := reserveToolLLMUsageWithMedia(ctx, svc, "read_video", "openrouter", model, mediaTestRequest(model), bigVideoPayload())
	if !errors.Is(err, usagecaps.ErrCapExceeded) {
		t.Fatalf("expected the tenant token cap to block the media charge, got %T: %v", err, err)
	}
	if !isBudgetRefusal(err) {
		t.Fatalf("a tenant cap denial must be terminal for the fallback chain, got %T: %v", err, err)
	}
	if capStore.reserved.EstimatedTokens < 946_800 {
		t.Fatalf("the reservation saw %d tokens, want at least the 946,800-token media charge", capStore.reserved.EstimatedTokens)
	}
	if len(capStore.events) == 0 || capStore.events[0].Decision != store.UsageCapEventBlock {
		t.Fatalf("expected a block event recorded for the tenant, got %+v", capStore.events)
	}
}
