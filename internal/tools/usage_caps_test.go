package tools

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	usagecaps "github.com/nextlevelbuilder/goclaw/internal/usage/caps"
)

// bigContent builds a user message string that exceeds `targetChars` words under
// the caps guard's fixed BudgetCounter, so the guard math is predictable here.
func bigContent(targetChars int) string {
	return strings.Repeat("word ", targetChars)
}

// withToolAgentBudget mirrors what the agent loop's injectContext does before a
// tool runs: it propagates the CALLING agent's context window and max_tokens.
func withToolAgentBudget(window, maxTokens int) context.Context {
	ctx := store.WithAgentContextWindow(context.Background(), window)
	return store.WithAgentMaxTokens(ctx, maxTokens)
}

// TestReserveToolLLMUsage_HonorsAgentWindowFromContext is the production-path
// regression guard: reserveToolLLMUsage reads the CALLING agent's budget from
// ctx (set by injectContext via store.WithAgentContextWindow /
// store.WithAgentMaxTokens) and enforces completeInput + max_tokens <= window.
// Model/provider are never budget authorities.
func TestReserveToolLLMUsage_HonorsAgentWindowFromContext(t *testing.T) {
	model := "claude-sonnet-4-5-20250929" // 200k model window — must NOT matter
	req := providers.ChatRequest{
		Model:    model,
		Messages: []providers.Message{{Role: "user", Content: bigContent(40_000)}},
		Options:  map[string]any{providers.OptMaxTokens: 4096},
	}

	// A 200k agent window admits ~40k tokens of prompt.
	ctxBig := withToolAgentBudget(200_000, 8_192)
	if _, err := reserveToolLLMUsage(ctxBig, nil, "read_document", "anthropic", model, req); err != nil {
		t.Fatalf("expected allow under 200k agent window, got %v", err)
	}

	// A 20k agent window must block the SAME request before transport.
	ctxSmall := withToolAgentBudget(20_000, 8_192)
	_, err := reserveToolLLMUsage(ctxSmall, nil, "read_document", "anthropic", model, req)
	if err == nil {
		t.Fatal("expected abort when agent window (20k) is below the request size")
	}
	var ctxErr *usagecaps.ContextWindowExceededError
	if !errors.As(err, &ctxErr) {
		t.Fatalf("expected *ContextWindowExceededError, got %T: %v", err, err)
	}
	if ctxErr.ContextWindow != 20_000 {
		t.Fatalf("guard used window %d, want 20000 (agent cap, not model window)", ctxErr.ContextWindow)
	}
}

// TestReserveToolLLMUsage_FailsClosedWithoutAgentBudget proves an agent-scoped
// tool call reaching the model gate WITHOUT a propagated budget fails closed
// with a wiring error before any transport — it must never silently guess a
// model window.
func TestReserveToolLLMUsage_FailsClosedWithoutAgentBudget(t *testing.T) {
	req := providers.ChatRequest{
		Model:    "gpt-4o",
		Messages: []providers.Message{{Role: "user", Content: "tiny prompt"}},
		Options:  map[string]any{providers.OptMaxTokens: 4096},
	}
	_, err := reserveToolLLMUsage(context.Background(), nil, "read_document", "openai", "gpt-4o", req)
	if err == nil {
		t.Fatal("expected wiring error without a propagated agent budget")
	}
	var wiringErr *usagecaps.AgentBudgetWiringError
	if !errors.As(err, &wiringErr) {
		t.Fatalf("expected *AgentBudgetWiringError, got %T: %v", err, err)
	}
}

// TestReserveToolLLMUsageWithMedia_CountsNativeMediaBytes proves the out-of-band
// media payload is part of completeInput. The fixed BudgetCounter counts the
// standard-base64 representation of the raw bytes (a model/provider-independent
// rule), so a small prompt with a large native-media payload is blocked when it
// no longer fits the CALLING agent's window. The bytes are counted, never
// transported — the guard copy carries them, the real request does not.
func TestReserveToolLLMUsageWithMedia_CountsNativeMediaBytes(t *testing.T) {
	model := "gpt-4o"
	req := func() providers.ChatRequest {
		return providers.ChatRequest{
			Model:    model,
			Messages: []providers.Message{{Role: "user", Content: "Transcribe this."}},
			Options:  map[string]any{providers.OptMaxTokens: 4096},
		}
	}

	// A small media payload under a large agent window is allowed.
	small := bytes.Repeat([]byte{0xAB}, 1024)
	ctxBig := withToolAgentBudget(128_000, 8_192)
	if _, err := reserveToolLLMUsageWithMedia(ctxBig, nil, "read_document", "openai", model, req(), "application/pdf", small); err != nil {
		t.Fatalf("small native media must fit a 128k window, got %v", err)
	}

	// A large media payload against a small agent window is blocked BEFORE
	// transport: base64(256 KiB) is well over the 20k window's input cap.
	big := bytes.Repeat([]byte{0xCD}, 256*1024)
	ctxSmall := withToolAgentBudget(20_000, 8_192)
	_, err := reserveToolLLMUsageWithMedia(ctxSmall, nil, "read_document", "openai", model, req(), "application/pdf", big)
	if err == nil {
		t.Fatal("expected abort: large native media must exceed a 20k agent window")
	}
	var ctxErr *usagecaps.ContextWindowExceededError
	if !errors.As(err, &ctxErr) {
		t.Fatalf("expected *ContextWindowExceededError, got %T: %v", err, err)
	}
	if ctxErr.ContextWindow != 20_000 {
		t.Fatalf("guard used window %d, want 20000 (agent cap)", ctxErr.ContextWindow)
	}
}

// TestReserveToolLLMUsageUnverifiableMedia_FailsClosedUnderAgentBudget proves a
// native-media call whose payload cannot be buffered (a streamed remote URL)
// fails closed under an agent budget rather than undercounting — the
// complete-input invariant cannot be proven, so it refuses to send.
func TestReserveToolLLMUsageUnverifiableMedia_FailsClosedUnderAgentBudget(t *testing.T) {
	model := "gemini-2.0-flash"
	req := providers.ChatRequest{
		Model:    model,
		Messages: []providers.Message{{Role: "user", Content: "Analyze this video."}},
		Options:  map[string]any{providers.OptMaxTokens: 4096},
	}

	// Under an agent budget: no in-memory payload to count -> refuse with an
	// explicit streaming error (not a wiring error).
	ctx := withToolAgentBudget(200_000, 8_192)
	_, err := reserveToolLLMUsageUnverifiableMedia(ctx, nil, "read_video", "gemini", model, req)
	if err == nil {
		t.Fatal("expected fail-closed for unverifiable streamed media under an agent budget")
	}
	var wiringErr *usagecaps.AgentBudgetWiringError
	if errors.As(err, &wiringErr) {
		t.Fatalf("under a valid agent budget the failure must be the streaming refusal, not a wiring error: %v", err)
	}
	if !strings.Contains(err.Error(), "refusing to send") {
		t.Fatalf("expected an explicit streaming refusal, got %v", err)
	}

	// Without a propagated agent budget it falls through to the text path, which
	// itself fails closed with a wiring error — every tool LLM call is
	// agent-scoped, so no path here reaches transport uncounted.
	_, err = reserveToolLLMUsageUnverifiableMedia(context.Background(), nil, "read_video", "gemini", model, req)
	if err == nil {
		t.Fatal("expected fail-closed without a propagated agent budget")
	}
	if !errors.As(err, &wiringErr) {
		t.Fatalf("expected *AgentBudgetWiringError without a budget, got %T: %v", err, err)
	}
}
