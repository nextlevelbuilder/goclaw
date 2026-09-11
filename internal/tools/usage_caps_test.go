package tools

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/mediabudget"
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

// stubMediaProbes fixes what the external binaries report, so a charge under test is the policy's arithmetic and not whatever is installed on the runner.
func stubMediaProbes(t *testing.T, seconds int, pages int) {
	t.Helper()
	t.Cleanup(mediabudget.SetProbesForTest(
		func(context.Context, string) (time.Duration, bool) {
			return time.Duration(seconds) * time.Second, true
		},
		func(context.Context, string) (int, bool) { return pages, true },
	))
}

// An hour of video is 946,800 tokens at the published rate: far past a 20k
// window, comfortably inside a 2M one.
func bigVideoPayload() mediabudget.Payload {
	return mediabudget.Payload{Kind: mediabudget.KindVideo, MIME: "video/mp4", Size: 200_000_000, Path: "/does/not/matter"}
}

func mediaTestRequest(model string) providers.ChatRequest {
	return providers.ChatRequest{
		Model:    model,
		Messages: []providers.Message{{Role: "user", Content: "Describe this video."}},
		Options:  map[string]any{providers.OptMaxTokens: 8},
	}
}

func TestReserveToolLLMUsageWithMedia_LargePayloadTripsContextWindowGuard(t *testing.T) {
	stubMediaProbes(t, 3600, 1)
	model := "gpt-4o"
	ctxSmall := withToolAgentBudget(20_000, 8)
	_, err := reserveToolLLMUsageWithMedia(ctxSmall, nil, "read_video", "openai", model, mediaTestRequest(model), bigVideoPayload())
	if err == nil {
		t.Fatal("expected abort: an hour of measured video must exceed a 20k window")
	}
	var ctxErr *usagecaps.ContextWindowExceededError
	if !errors.As(err, &ctxErr) {
		t.Fatalf("expected *ContextWindowExceededError, got %T: %v", err, err)
	}
	if ctxErr.ContextWindow != 20_000 {
		t.Fatalf("guard used window %d, want 20000 (agent cap)", ctxErr.ContextWindow)
	}
}

func TestReserveToolLLMUsageWithMedia_LargePayloadAllowedUnderLargeWindow(t *testing.T) {
	stubMediaProbes(t, 3600, 1)
	model := "gpt-4o"
	ctxBig := withToolAgentBudget(2_000_000, 8)
	if _, err := reserveToolLLMUsageWithMedia(ctxBig, nil, "read_video", "openai", model, mediaTestRequest(model), bigVideoPayload()); err != nil {
		t.Fatalf("expected allow under a 2M window, got %v", err)
	}
}

// Valid because reserveToolLLMUsageWithMediaTokens feeds the same mediaTokens value to both the guard and Request.ExtraInputTokens.
func TestReserveToolLLMUsageWithMedia_ChargesSummedMediaBudgetEstimate(t *testing.T) {
	stubMediaProbes(t, 12, 1)
	model := "gpt-4o"
	video := mediabudget.Payload{Kind: mediabudget.KindVideo, MIME: "video/mp4", Size: 32_000, Path: "/does/not/matter"}
	audio := mediabudget.Payload{Kind: mediabudget.KindAudio, MIME: "audio/mpeg", Size: 6_000, Path: "/does/not/matter"}
	videoTokens, err := mediabudget.Tokens(context.Background(), video)
	if err != nil {
		t.Fatalf("pricing the video payload: %v", err)
	}
	audioTokens, err := mediabudget.Tokens(context.Background(), audio)
	if err != nil {
		t.Fatalf("pricing the audio payload: %v", err)
	}
	wantMediaTokens := videoTokens + audioTokens

	ctxTiny := withToolAgentBudget(1, 1)

	_, textOnlyErr := reserveToolLLMUsage(ctxTiny, nil, "read_video", "openai", model, mediaTestRequest(model))
	var textOnlyErrCtx *usagecaps.ContextWindowExceededError
	if !errors.As(textOnlyErr, &textOnlyErrCtx) {
		t.Fatalf("expected text-only call to abort under a 1-token window, got %v", textOnlyErr)
	}

	_, mediaErr := reserveToolLLMUsageWithMedia(ctxTiny, nil, "read_video", "openai", model, mediaTestRequest(model), video, audio)
	var mediaErrCtx *usagecaps.ContextWindowExceededError
	if !errors.As(mediaErr, &mediaErrCtx) {
		t.Fatalf("expected media call to abort under a 1-token window, got %v", mediaErr)
	}

	gotMediaTokens := mediaErrCtx.InputTokens - textOnlyErrCtx.InputTokens
	if gotMediaTokens != wantMediaTokens {
		t.Fatalf("media charge reaching the guard = %d, want %d (sum of mediabudget.Tokens per payload)", gotMediaTokens, wantMediaTokens)
	}
}

// mediaChargeIsTheBlocker runs one payload through the media reservation under a
// window an identical text-only request passes, so a refusal can only come from
// the media charge.
func mediaChargeIsTheBlocker(t *testing.T, window int, payload mediabudget.Payload) {
	t.Helper()
	model := "gpt-4o"
	ctx := withToolAgentBudget(window, 8)

	if _, err := reserveToolLLMUsage(ctx, nil, "read_media", "openai", model, mediaTestRequest(model)); err != nil {
		t.Fatalf("the same request without media must pass under a %d-token window, got %v", window, err)
	}

	_, err := reserveToolLLMUsageWithMedia(ctx, nil, "read_media", "openai", model, mediaTestRequest(model), payload)
	var ctxErr *usagecaps.ContextWindowExceededError
	if !errors.As(err, &ctxErr) {
		t.Fatalf("expected *ContextWindowExceededError from the media charge, got %T: %v", err, err)
	}
	if ctxErr.ContextWindow != window {
		t.Fatalf("guard used window %d, want %d", ctxErr.ContextWindow, window)
	}
}

// Two hours of measured podcast is 230,400 tokens, which no 200k window holds.
func TestReserveToolLLMUsageWithMedia_LargeAudioCannotProceed(t *testing.T) {
	stubMediaProbes(t, 7200, 1)
	mediaChargeIsTheBlocker(t, 200_000, mediabudget.Payload{Kind: mediabudget.KindAudio, MIME: "audio/mpeg", Size: 40_000_000, Path: "/does/not/matter"})
}

// A PDF whose page count cannot be measured is charged the provider's
// 1000-page maximum, and 258,000 tokens does not fit a 200k window.
func TestReserveToolLLMUsageWithMedia_UnmeasurablePDFCannotProceed(t *testing.T) {
	mediaChargeIsTheBlocker(t, 200_000, mediabudget.Payload{Kind: mediabudget.KindDocument, MIME: "application/pdf", Size: 5_000_000})
}

// The same 1000-page ceiling must stay reachable: an agent whose window can
// hold 258,000 tokens still gets to read an unmeasurable PDF.
func TestReserveToolLLMUsageWithMedia_UnmeasurablePDFAllowedUnderLargeWindow(t *testing.T) {
	model := "gpt-4o"
	ctx := withToolAgentBudget(400_000, 8)
	pdf := mediabudget.Payload{Kind: mediabudget.KindDocument, MIME: "application/pdf", Size: 5_000_000}
	if _, err := reserveToolLLMUsageWithMedia(ctx, nil, "read_document", "openai", model, mediaTestRequest(model), pdf); err != nil {
		t.Fatalf("expected the 1000-page ceiling to fit a 400k window, got %v", err)
	}
}

// A few hundred KB of docx is priced as text, well inside an ordinary window.
func TestReserveToolLLMUsageWithMedia_NonPDFDocumentFitsOrdinaryWindow(t *testing.T) {
	model := "gpt-4o"
	ctx := withToolAgentBudget(200_000, 8)
	docx := mediabudget.Payload{
		Kind: mediabudget.KindDocument,
		MIME: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		Size: 400_000,
	}
	if _, err := reserveToolLLMUsageWithMedia(ctx, nil, "read_document", "openai", model, mediaTestRequest(model), docx); err != nil {
		t.Fatalf("expected a 400KB docx to pass under a 200k window, got %v", err)
	}
}

// A video nothing can measure is refused outright, with the missing binary
// named, rather than charged a number derived from its bytes.
func TestReserveToolLLMUsageWithMedia_UnmeasurableVideoIsRefused(t *testing.T) {
	t.Cleanup(mediabudget.SetProbesForTest(nil, nil))

	model := "gpt-4o"
	ctx := withToolAgentBudget(2_000_000, 8)
	video := mediabudget.Payload{Kind: mediabudget.KindVideo, MIME: "video/mp4", Size: 20_000, Path: "/does/not/matter"}

	_, err := reserveToolLLMUsageWithMedia(ctx, nil, "read_video", "openai", model, mediaTestRequest(model), video)
	if !errors.Is(err, mediabudget.ErrDurationUnmeasurable) {
		t.Fatalf("expected ErrDurationUnmeasurable even under a 2M window, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "ffprobe") {
		t.Fatalf("error %q does not name the missing probe", err)
	}
}
