package providers

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/reliability"
)

type testFallbackProvider struct {
	name      string
	model     string
	err       error
	streamErr error
	calls     int
}

func (p *testFallbackProvider) Chat(_ context.Context, req ChatRequest) (*ChatResponse, error) {
	p.calls++
	if p.err != nil {
		return nil, p.err
	}
	return &ChatResponse{Content: req.Model, FinishReason: "stop"}, nil
}

func (p *testFallbackProvider) ChatStream(_ context.Context, req ChatRequest, onChunk func(StreamChunk)) (*ChatResponse, error) {
	p.calls++
	if p.streamErr != nil {
		if req.Model == "primary-model" {
			onChunk(StreamChunk{Content: "partial"})
		}
		return nil, p.streamErr
	}
	return &ChatResponse{Content: req.Model, FinishReason: "stop"}, nil
}

func (p *testFallbackProvider) DefaultModel() string { return p.model }
func (p *testFallbackProvider) Name() string         { return p.name }

func TestModelFallbackProviderFallsBackOnClassifiedError(t *testing.T) {
	primary := &testFallbackProvider{
		name:  "primary",
		model: "primary-model",
		err:   &HTTPError{Status: 429, Body: "rate limited"},
	}
	backup := &testFallbackProvider{name: "backup", model: "backup-model"}
	provider := NewModelFallbackProvider(FallbackCandidate{
		ProviderName: "primary",
		Provider:     primary,
		Model:        "primary-model",
	}, []FallbackCandidate{
		{ProviderName: "backup", Provider: backup, Model: "backup-model"},
	}, 2, false)

	resp, err := provider.Chat(context.Background(), ChatRequest{})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Content != "backup-model" {
		t.Fatalf("Chat() content = %q, want backup model", resp.Content)
	}
	if primary.calls != 1 || backup.calls != 1 {
		t.Fatalf("calls primary=%d backup=%d, want 1/1", primary.calls, backup.calls)
	}
}

func TestModelFallbackProviderDoesNotFallbackAfterStreamChunk(t *testing.T) {
	streamErr := &HTTPError{Status: 429, Body: "rate limited"}
	primary := &testFallbackProvider{
		name:      "primary",
		model:     "primary-model",
		streamErr: streamErr,
	}
	backup := &testFallbackProvider{name: "backup", model: "backup-model"}
	provider := NewModelFallbackProvider(FallbackCandidate{
		ProviderName: "primary",
		Provider:     primary,
		Model:        "primary-model",
	}, []FallbackCandidate{
		{ProviderName: "backup", Provider: backup, Model: "backup-model"},
	}, 2, false)

	var chunks int
	_, err := provider.ChatStream(context.Background(), ChatRequest{}, func(StreamChunk) {
		chunks++
	})
	if err == nil {
		t.Fatal("ChatStream() error = nil, want primary stream error")
	}
	if chunks != 1 {
		t.Fatalf("chunks = %d, want 1", chunks)
	}
	if backup.calls != 0 {
		t.Fatalf("backup calls = %d, want 0 after partial stream", backup.calls)
	}
}

func TestModelFallbackProviderChatStreamWithHookReportsStreamedChunks(t *testing.T) {
	streamErr := &HTTPError{Status: 429, Body: "rate limited"}
	primary := &testFallbackProvider{
		name:      "primary",
		model:     "primary-model",
		streamErr: streamErr,
	}
	provider := NewModelFallbackProvider(FallbackCandidate{
		ProviderName: "primary",
		Provider:     primary,
		Model:        "primary-model",
	}, nil, 1, false)

	var streamed bool
	_, err := provider.ChatStreamWithHook(context.Background(), ChatRequest{}, func(StreamChunk) {}, func(context.Context, FallbackCandidate, ChatRequest) (FallbackAfterCall, error) {
		return func(_ *ChatResponse, _ error, info FallbackCallInfo) {
			streamed = info.Streamed
		}, nil
	})
	if err == nil {
		t.Fatal("ChatStreamWithHook() error = nil, want stream error")
	}
	if !streamed {
		t.Fatal("FallbackCallInfo.Streamed = false, want true after partial stream")
	}
}

func TestModelFallbackProviderFallsBackToSameModelOnDifferentProvider(t *testing.T) {
	primary := &testFallbackProvider{
		name:  "primary",
		model: "shared-model",
		err:   &HTTPError{Status: 404, Body: "model not found"},
	}
	backup := &testFallbackProvider{name: "backup", model: "shared-model"}
	provider := NewModelFallbackProvider(FallbackCandidate{
		ProviderName: "primary",
		Provider:     primary,
		Model:        "shared-model",
	}, []FallbackCandidate{
		{ProviderName: "backup", Provider: backup, Model: "shared-model"},
	}, 0, false)

	resp, err := provider.Chat(context.Background(), ChatRequest{})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Content != "shared-model" {
		t.Fatalf("Chat() content = %q, want shared model from backup", resp.Content)
	}
	if primary.calls != 1 || backup.calls != 1 {
		t.Fatalf("calls primary=%d backup=%d, want 1/1", primary.calls, backup.calls)
	}
}

func TestModelFallbackProviderDoesNotFallbackOnUnknownError(t *testing.T) {
	unknownErr := errors.New("request serialization failed")
	primary := &testFallbackProvider{
		name:  "primary",
		model: "primary-model",
		err:   unknownErr,
	}
	backup := &testFallbackProvider{name: "backup", model: "backup-model"}
	provider := NewModelFallbackProvider(FallbackCandidate{
		ProviderName: "primary",
		Provider:     primary,
		Model:        "primary-model",
	}, []FallbackCandidate{
		{ProviderName: "backup", Provider: backup, Model: "backup-model"},
	}, 0, false)

	_, err := provider.Chat(context.Background(), ChatRequest{})
	if !errors.Is(err, unknownErr) {
		t.Fatalf("Chat() error = %v, want original unknown error", err)
	}
	if backup.calls != 0 {
		t.Fatalf("backup calls = %d, want 0 for unknown error", backup.calls)
	}
}

func TestModelFallbackProviderContinuesAfterContentPolicyFallback(t *testing.T) {
	primary := &testFallbackProvider{
		name:  "primary",
		model: "primary-model",
		err:   &HTTPError{Status: 429, Body: "rate limited"},
	}
	blocked := &testFallbackProvider{
		name:  "blocked",
		model: "blocked-model",
		err:   &HTTPError{Status: 400, Body: `{"error":{"code":"data_inspection_failed","message":"Input text data may contain inappropriate content."}}`},
	}
	backup := &testFallbackProvider{name: "backup", model: "backup-model"}
	provider := NewModelFallbackProvider(FallbackCandidate{
		ProviderName: "primary",
		Provider:     primary,
		Model:        "primary-model",
	}, []FallbackCandidate{
		{ProviderName: "blocked", Provider: blocked, Model: "blocked-model"},
		{ProviderName: "backup", Provider: backup, Model: "backup-model"},
	}, 0, false)

	resp, err := provider.Chat(context.Background(), ChatRequest{})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Content != "backup-model" {
		t.Fatalf("Chat() content = %q, want backup model", resp.Content)
	}
	if primary.calls != 1 || blocked.calls != 1 || backup.calls != 1 {
		t.Fatalf("calls primary=%d blocked=%d backup=%d, want 1/1/1", primary.calls, blocked.calls, backup.calls)
	}
}

func TestModelFallbackProviderFallsBackOnCodexSafetyRefusalString(t *testing.T) {
	primary := &testFallbackProvider{
		name:  "codex-digitop",
		model: "gpt-5.5",
		err:   errors.New("codex: response failed: Invalid prompt: we've limited access to this content for safety reasons"),
	}
	backup := &testFallbackProvider{name: "anthropic", model: "claude-sonnet-4-5"}
	provider := NewModelFallbackProvider(FallbackCandidate{
		ProviderName: "codex-digitop",
		Provider:     primary,
		Model:        "gpt-5.5",
	}, []FallbackCandidate{
		{ProviderName: "anthropic", Provider: backup, Model: "claude-sonnet-4-5"},
	}, 2, false)

	resp, err := provider.Chat(context.Background(), ChatRequest{})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Content != "claude-sonnet-4-5" {
		t.Fatalf("Chat() content = %q, want fallback model response", resp.Content)
	}
	if primary.calls != 1 || backup.calls != 1 {
		t.Fatalf("calls primary=%d backup=%d, want 1/1", primary.calls, backup.calls)
	}
}

func TestModelFallbackProviderMaxAttemptsCapsTotalAttempts(t *testing.T) {
	primary := &testFallbackProvider{
		name:  "primary",
		model: "primary-model",
		err:   &HTTPError{Status: 429, Body: "rate limited"},
	}
	backup := &testFallbackProvider{name: "backup", model: "backup-model"}
	provider := NewModelFallbackProvider(FallbackCandidate{
		ProviderName: "primary",
		Provider:     primary,
		Model:        "primary-model",
	}, []FallbackCandidate{
		{ProviderName: "backup", Provider: backup, Model: "backup-model"},
	}, 1, false)

	_, err := provider.Chat(context.Background(), ChatRequest{})
	if err == nil {
		t.Fatal("Chat() error = nil, want exhausted after primary only")
	}
	if primary.calls != 1 || backup.calls != 0 {
		t.Fatalf("calls primary=%d backup=%d, want 1/0", primary.calls, backup.calls)
	}
}

// mockFallbackProvider implements Provider with a deterministic rate-limit
// error (retryable, classified with a reason) for ordering tests that exercise
// out-of-order candidates.
type mockFallbackProvider struct{ name string }

func (p *mockFallbackProvider) Chat(_ context.Context, _ ChatRequest) (*ChatResponse, error) {
	return nil, &HTTPError{Status: 429, Body: "rate limited"}
}

func (p *mockFallbackProvider) ChatStream(_ context.Context, _ ChatRequest, _ func(StreamChunk)) (*ChatResponse, error) {
	return nil, &HTTPError{Status: 429, Body: "rate limited"}
}

func (p *mockFallbackProvider) DefaultModel() string { return "" }
func (p *mockFallbackProvider) Name() string         { return p.name }

// orderedProviderNames returns the Chat-order of candidate providers for the
// first run (no cooldowns, every candidate fails).
func orderedProviderNames(t *testing.T, provider *ModelFallbackProvider) []string {
	t.Helper()
	_, err := provider.Chat(context.Background(), ChatRequest{})
	if err == nil {
		t.Fatal("Chat() error = nil, want fallback exhaustion error")
	}
	var names []string
	for _, d := range provider.LastAttempts() {
		if d.Skipped {
			t.Fatalf("unexpected skipped candidate %q", d.Candidate.ProviderName)
		}
		names = append(names, d.Candidate.ProviderName)
	}
	return names
}

// TestModelFallbackHealthOrder verifies the health_order policy: primary is
// first even with the lowest score; qualified fallbacks sort by descending
// score; unqualified fallbacks keep configured order after qualified ones.
func TestModelFallbackHealthOrder(t *testing.T) {
	reg := reliability.Default()
	// 6 successes out of 8 attempts → score 0.75.
	observeMixed(reg, "health-fallback-a", "model", 6, 2)
	observeMixed(reg, "health-fallback-b", "model", 8, 0) // 1.0
	observeMixed(reg, "health-fallback-c", "model", 7, 1) // 0.875
	observeMixed(reg, "health-fallback-d", "model", 3, 0) // below threshold, 1.0 score
	observeMixed(reg, "health-fallback-e", "model", 2, 0) // below threshold

	provider := NewModelFallbackProvider(FallbackCandidate{
		ProviderName: "health-primary",
		Provider:     &mockFallbackProvider{name: "health-primary"},
		Model:        "model",
	}, []FallbackCandidate{
		{ProviderName: "health-fallback-a", Provider: &mockFallbackProvider{name: "health-fallback-a"}, Model: "model"},
		{ProviderName: "health-fallback-b", Provider: &mockFallbackProvider{name: "health-fallback-b"}, Model: "model"},
		{ProviderName: "health-fallback-c", Provider: &mockFallbackProvider{name: "health-fallback-c"}, Model: "model"},
		{ProviderName: "health-fallback-d", Provider: &mockFallbackProvider{name: "health-fallback-d"}, Model: "model"},
		{ProviderName: "health-fallback-e", Provider: &mockFallbackProvider{name: "health-fallback-e"}, Model: "model"},
	}, 0, false).WithFallbackPolicy(FallbackPolicy{
		Strategy:             FallbackStrategyHealth,
		MinAttemptsForHealth: 5,
	})

	got := orderedProviderNames(t, provider)
	want := []string{"health-primary", "health-fallback-b", "health-fallback-c", "health-fallback-a", "health-fallback-d", "health-fallback-e"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("health_order order = %v, want %v", got, want)
	}
}

// TestModelFallbackHealthOrderPrimaryLowestScore verifies the primary always
// leads even when its health score is the lowest of all candidates.
func TestModelFallbackHealthOrderPrimaryLowestScore(t *testing.T) {
	reg := reliability.Default()
	observeMixed(reg, "hp-a", "model", 5, 4)   // 5/9 = 0.556
	observeMixed(reg, "hp-prim", "model", 4, 4) // 4/8 = 0.5
	observeMixed(reg, "hp-b", "model", 8, 0)    // 1.0 (scored first)

	provider := NewModelFallbackProvider(FallbackCandidate{
		ProviderName: "hp-prim",
		Provider:     &mockFallbackProvider{name: "hp-prim"},
		Model:        "model",
	}, []FallbackCandidate{
		{ProviderName: "hp-a", Provider: &mockFallbackProvider{name: "hp-a"}, Model: "model"},
		{ProviderName: "hp-b", Provider: &mockFallbackProvider{name: "hp-b"}, Model: "model"},
	}, 0, false).WithFallbackPolicy(FallbackPolicy{
		Strategy:             FallbackStrategyHealth,
		MinAttemptsForHealth: 5,
	})

	got := orderedProviderNames(t, provider)
	if got[0] != "hp-prim" {
		t.Fatalf("primary not first: %v", got)
	}
	if got[1] != "hp-b" || got[2] != "hp-a" {
		t.Fatalf("fallback order = %v, want hp-b then hp-a", got)
	}
}

// TestModelFallbackPriorityOrder verifies the default strategy keeps the
// historical behavior: scored candidates first (descending), then unscored in
// configured order.
func TestModelFallbackPriorityOrder(t *testing.T) {
	reg := reliability.Default()
	observeMixed(reg, "po-high", "model", 7, 1) // 0.857
	observeMixed(reg, "po-mid", "model", 8, 4)  // 0.5
	observeMixed(reg, "po-no-signal", "model", 2, 2)

	provider := NewModelFallbackProvider(FallbackCandidate{
		ProviderName: "po-primary",
		Provider:     &mockFallbackProvider{name: "po-primary"},
		Model:        "model",
	}, []FallbackCandidate{
		{ProviderName: "po-mid", Provider: &mockFallbackProvider{name: "po-mid"}, Model: "model"},
		{ProviderName: "po-no-signal", Provider: &mockFallbackProvider{name: "po-no-signal"}, Model: "model"},
		{ProviderName: "po-high", Provider: &mockFallbackProvider{name: "po-high"}, Model: "model"},
	}, 0, false)

	got := orderedProviderNames(t, provider)
	want := []string{"po-primary", "po-high", "po-mid", "po-no-signal"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("priority_order order = %v, want %v", got, want)
	}
}

// TestModelFallbackPolicyBackwardCompat verifies a wrapper without an explicit
// policy keeps the historical behavior: no policy → priority order, default
// threshold (5 attempts).
func TestModelFallbackPolicyBackwardCompat(t *testing.T) {
	reg := reliability.Default()
	observeMixed(reg, "bc-a", "model", 8, 0) // 1.0
	observeMixed(reg, "bc-b", "model", 6, 0) // 1.0
	observeMixed(reg, "bc-c", "model", 0, 0) // no signal → configured order

	provider := NewModelFallbackProvider(FallbackCandidate{
		ProviderName: "bc-primary",
		Provider:     &mockFallbackProvider{name: "bc-primary"},
		Model:        "model",
	}, []FallbackCandidate{
		{ProviderName: "bc-a", Provider: &mockFallbackProvider{name: "bc-a"}, Model: "model"},
		{ProviderName: "bc-b", Provider: &mockFallbackProvider{name: "bc-b"}, Model: "model"},
		{ProviderName: "bc-c", Provider: &mockFallbackProvider{name: "bc-c"}, Model: "model"},
	}, 0, false)

	got := orderedProviderNames(t, provider)
	want := []string{"bc-primary", "bc-a", "bc-b", "bc-c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("default-policy order = %v, want %v", got, want)
	}
}

// TestModelFallbackMinAttemptsGate verifies the health_order attempt gate:
// a candidate below the threshold keeps configured order even with a perfect
// score; crossing the threshold enables ranking.
func TestModelFallbackMinAttemptsGate(t *testing.T) {
	reg := reliability.Default()
	observeMixed(reg, "gate-guard", "model", 3, 0) // 1.0, below threshold
	reg.Health.ObserveSuccess("gate-top", "model")
	reg.Health.ObserveSuccess("gate-top", "model")
	reg.Health.ObserveSuccess("gate-top", "model")
	reg.Health.ObserveSuccess("gate-top", "model")
	reg.Health.ObserveSuccess("gate-top", "model")
	reg.Health.ObserveSuccess("gate-top", "model") // 6 attempts, 1.0

	provider := NewModelFallbackProvider(FallbackCandidate{
		ProviderName: "gate-primary",
		Provider:     &mockFallbackProvider{name: "gate-primary"},
		Model:        "model",
	}, []FallbackCandidate{
		{ProviderName: "gate-guard", Provider: &mockFallbackProvider{name: "gate-guard"}, Model: "model"},
		{ProviderName: "gate-top", Provider: &mockFallbackProvider{name: "gate-top"}, Model: "model"},
	}, 0, false).WithFallbackPolicy(FallbackPolicy{
		Strategy:             FallbackStrategyHealth,
		MinAttemptsForHealth: 5,
	})

	got := orderedProviderNames(t, provider)
	want := []string{"gate-primary", "gate-top", "gate-guard"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("gate order = %v, want %v (below-threshold candidate must trail)", got, want)
	}
}

// TestModelFallbackCooldownSkipDiagnostic verifies runOrdered records a
// Skipped + "cooldown" diagnostic when a candidate is skipped, after a
// previous run's failure seeded the cooldown.
func TestModelFallbackCooldownSkipDiagnostic(t *testing.T) {
	primary := &testFallbackProvider{
		name:  "sd-primary",
		model: "model",
		err:   &HTTPError{Status: 429, Body: "rate limited"},
	}
	fallback := &testFallbackProvider{
		name:  "sd-fallback",
		model: "model",
		err:   &HTTPError{Status: 429, Body: "rate limited"},
	}
	provider := NewModelFallbackProvider(FallbackCandidate{
		ProviderName: "sd-primary",
		Provider:     primary,
		Model:        "model",
	}, []FallbackCandidate{
		{ProviderName: "sd-fallback", Provider: fallback, Model: "model"},
	}, 0, true)

	// First run: primary fails, fallback fails → fallback enters cooldown,
	// run ends exhausted. Both providers were called once.
	if _, err := provider.Chat(context.Background(), ChatRequest{}); err == nil {
		t.Fatal("first Chat() error = nil, want fallback exhaustion error")
	}
	if primary.calls != 1 || fallback.calls != 1 {
		t.Fatalf("first run calls primary=%d fallback=%d, want 1/1", primary.calls, fallback.calls)
	}
	primCalls, fallbackCalls := primary.calls, fallback.calls

	// Second run: primary fails again; the fallback's first probe is granted
	// during cooldown, so it is tried once more.
	if _, err := provider.Chat(context.Background(), ChatRequest{}); err == nil {
		t.Fatal("second Chat() error = nil, want fallback exhaustion error")
	}
	if primary.calls != primCalls+1 || fallback.calls != fallbackCalls+1 {
		t.Fatalf("second run calls primary=%d (want %d) fallback=%d (want %d, probe granted)",
			primary.calls, primCalls+1, fallback.calls, fallbackCalls+1)
	}
	primCalls, fallbackCalls = primary.calls, fallback.calls

	// Third run: primary fails again; the fallback's probe window is not
	// granted yet → candidate skipped and recorded with reason "cooldown".
	if _, err := provider.Chat(context.Background(), ChatRequest{}); err == nil {
		t.Fatal("third Chat() error = nil, want fallback exhaustion error")
	}
	if fallback.calls != fallbackCalls {
		t.Fatalf("third run fallback calls = %d (want %d, skipped)", fallback.calls, fallbackCalls)
	}

	// The diagnostic tape accumulates across runs; the final pair is the third
	// run's decision. Both candidates have their probe windows consumed by the
	// second run, so both are skipped for cooldown before any provider call.
	diags := provider.LastAttempts()
	if len(diags) < 2 {
		t.Fatalf("diagnostics = %d, want at least 2", len(diags))
	}
	last := diags[len(diags)-1]
	prev := diags[len(diags)-2]
	if !prev.Skipped || prev.SkipReason != "cooldown" || prev.Candidate.ProviderName != "sd-primary" {
		t.Fatalf("penultimate diagnostic = %+v, want skipped sd-primary with reason cooldown", prev)
	}
	if !last.Skipped || last.SkipReason != "cooldown" || last.Candidate.ProviderName != "sd-fallback" {
		t.Fatalf("final diagnostic = %+v, want skipped sd-fallback with reason cooldown", last)
	}
}

// TestModelFallbackDiagnosticsHealthScore verifies tried candidates record
// their health score at ordering time and it reflects the seeded signal.
func TestModelFallbackDiagnosticsHealthScore(t *testing.T) {
	reg := reliability.Default()
	// 4 successes + 1 failure → 5 attempts, score 0.8, no circuit penalty.
	observeMixed(reg, "diag-fb", "model", 4, 1)

	primary := &testFallbackProvider{name: "diag-primary", model: "model", err: &HTTPError{Status: 429, Body: "rate limited"}}
	provider := NewModelFallbackProvider(FallbackCandidate{
		ProviderName: "diag-primary",
		Provider:     primary,
		Model:        "model",
	}, []FallbackCandidate{
		{ProviderName: "diag-fb", Provider: &testFallbackProvider{name: "diag-fb", model: "model"}, Model: "model"},
	}, 0, false)

	if _, err := provider.Chat(context.Background(), ChatRequest{}); err != nil {
		t.Fatalf("Chat() error = %v, want fallback success", err)
	}

	diags := provider.LastAttempts()
	if len(diags) != 2 {
		t.Fatalf("diagnostics = %d, want 2", len(diags))
	}
	if diags[1].Candidate.ProviderName != "diag-fb" {
		t.Fatalf("second diagnostic = %+v, want diag-fb", diags[1])
	}
	if diags[1].HealthScore != 0.8 {
		t.Fatalf("diag-fb HealthScore = %f, want 0.8", diags[1].HealthScore)
	}
}

// TestModelFallbackLastAttemptsCopy verifies LastAttempts returns a defensive
// copy: mutating the returned slice does not affect subsequent diagnostics.
func TestModelFallbackLastAttemptsCopy(t *testing.T) {
	primary := &testFallbackProvider{name: "cp-primary", model: "model"}
	provider := NewModelFallbackProvider(FallbackCandidate{
		ProviderName: "cp-primary",
		Provider:     primary,
		Model:        "model",
	}, nil, 1, false)

	if _, err := provider.Chat(context.Background(), ChatRequest{}); err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	first := provider.LastAttempts()
	if len(first) != 1 {
		t.Fatalf("diagnostics = %d, want 1", len(first))
	}
	first[0].Skipped = true

	again := provider.LastAttempts()
	if again[0].Skipped {
		t.Fatal("mutation leaked into provider state")
	}
}

func observeMixed(reg *reliability.Runtime, provider, model string, successes, failures int) {
	for i := 0; i < successes; i++ {
		reg.Health.ObserveSuccess(provider, model)
	}
	for i := 0; i < failures; i++ {
		reg.Health.ObserveFailure(provider, model, reliability.ErrModelEmptyOutput)
	}
}
