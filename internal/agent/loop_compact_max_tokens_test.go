package agent

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// capturingProvider records every ChatRequest passed to Chat.
// Distinct from stubProvider in intent_classify_test.go (that one ignores the request).
type capturingProvider struct {
	captured []providers.ChatRequest
	response string
}

func (c *capturingProvider) Chat(_ context.Context, req providers.ChatRequest) (*providers.ChatResponse, error) {
	c.captured = append(c.captured, req)
	return &providers.ChatResponse{Content: c.response}, nil
}
func (c *capturingProvider) ChatStream(_ context.Context, req providers.ChatRequest, _ func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	c.captured = append(c.captured, req)
	return &providers.ChatResponse{Content: c.response}, nil
}
func (c *capturingProvider) DefaultModel() string { return "capturing-model" }
func (c *capturingProvider) Name() string         { return "capturing" }

// TestCompactMessagesInPlace_MaxTokensDynamic verifies that compactMessagesInPlace
// passes max_tokens == dynamicSummaryMax(estimatedInputTokens) to the provider.
func TestCompactMessagesInPlace_MaxTokensDynamic(t *testing.T) {
	cap := &capturingProvider{response: "Summary of conversation."}

	loop := &Loop{
		provider: cap,
		model:    "claude-3-5-sonnet",
		// tokenCounter nil → estimateSummaryInputTokens uses rune/3 fallback
	}

	// Build 10 dummy messages (>= 6 required by compactMessagesInPlace).
	msgs := make([]providers.Message, 10)
	for i := range msgs {
		if i%2 == 0 {
			msgs[i] = providers.Message{Role: "user", Content: "user message"}
		} else {
			msgs[i] = providers.Message{Role: "assistant", Content: "assistant reply"}
		}
	}

	result := loop.compactMessagesInPlace(context.Background(), msgs)
	if result == nil {
		t.Fatal("compactMessagesInPlace returned nil; expected compaction to succeed")
	}

	if len(cap.captured) != 1 {
		t.Fatalf("provider.Chat called %d time(s), want 1", len(cap.captured))
	}

	req := cap.captured[0]
	maxTokensRaw, ok := req.Options["max_tokens"]
	if !ok {
		t.Fatal("Options[\"max_tokens\"] not set in ChatRequest")
	}

	maxTokens, ok := maxTokensRaw.(int)
	if !ok {
		t.Fatalf("Options[\"max_tokens\"] type = %T, want int", maxTokensRaw)
	}

	// Compute expected using the same formula the implementation uses.
	// With keepCount=4 and 10 messages, splitIdx=6 (first 6 messages summarised).
	// tokenCounter nil → rune/3 fallback.
	expectedIn := loop.estimateSummaryInputTokens(msgs[:6])
	wantMax := dynamicSummaryMax(expectedIn)
	if maxTokens != wantMax {
		t.Errorf("max_tokens = %d, want %d (dynamicSummaryMax(%d))", maxTokens, wantMax, expectedIn)
	}
}
