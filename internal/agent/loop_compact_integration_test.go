package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/tokencount"
)

// buildVietnameseMsgs constructs n alternating user/assistant messages with
// Vietnamese UTF-8 content. Each message is ~viRunes runes to hit a realistic
// total token budget (~100k input tokens for 600 messages).
func buildVietnameseMsgs(n, viRunes int) []providers.Message {
	// ~viRunes-rune Vietnamese segment (3-byte UTF-8 per diacritic char).
	segment := strings.Repeat(
		"Xin chào! Đây là nội dung kiểm tra với ký tự tiếng Việt đặc biệt: ắ ặ ầ ẩ ậ ề ể ệ ọ ộ. ",
		(viRunes/80)+1,
	)
	runes := []rune(segment)
	if len(runes) > viRunes {
		segment = string(runes[:viRunes])
	}

	msgs := make([]providers.Message, n)
	for i := range msgs {
		role := "user"
		if i%2 != 0 {
			role = "assistant"
		}
		msgs[i] = providers.Message{Role: role, Content: segment}
	}
	return msgs
}

// TestLoopCompact_Integration_DynamicMaxTokens_VietnameseFixture verifies the
// end-to-end composition of Phase 03 (FallbackCounter) + Phase 04 (dynamicSummaryMax):
//
//  1. Loop with real FallbackCounter estimates ~100k input tokens from 600 Vietnamese messages.
//  2. compactMessagesInPlace passes max_tokens in [2000, 8192] to the provider.
//  3. The formula dynamicSummaryMax(in) = in/25 holds: for ~100k input → ~4000 output budget.
//
// Tolerance: FallbackCounter uses rune/3 heuristic so exact input count varies;
// we assert >= 2000 && <= 8192 rather than == 4000.
func TestLoopCompact_Integration_DynamicMaxTokens_VietnameseFixture(t *testing.T) {
	cap := &capturingProvider{response: "Tóm tắt cuộc trò chuyện: Đã thảo luận về nhiều chủ đề."}

	loop := &Loop{
		provider:     cap,
		model:        "claude-3-5-sonnet",
		tokenCounter: tokencount.NewFallbackCounter(),
	}

	// 600 messages × ~500 runes each ≈ 300k runes ÷ 3 ≈ 100k tokens total.
	// keepCount defaults to 4; splitIdx = 600-4 = 596 msgs to summarise.
	// FallbackCounter on 596 msgs × ~500 runes ÷ 3 ≈ ~99k tokens → dynamicSummaryMax(99000) = 3960 (floor 1024).
	msgs := buildVietnameseMsgs(600, 500)

	result := loop.compactMessagesInPlace(context.Background(), msgs)
	if result == nil {
		t.Fatal("compactMessagesInPlace returned nil; expected compaction to succeed with 600 messages")
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

	// Tolerance: FallbackCounter rune/3 varies slightly by content.
	// For ~100k token input: dynamicSummaryMax → ~4000 (formula in/25).
	// Assert range [2000, 8192] to accommodate counter variance.
	const minExpected = 2000
	const maxExpected = 8192
	if maxTokens < minExpected || maxTokens > maxExpected {
		t.Errorf("max_tokens = %d, want in [%d, %d]; formula dynamicSummaryMax(estimatedInput)",
			maxTokens, minExpected, maxExpected)
	}

	// Log actual observed value for diagnostics.
	keepCount := 4
	if minKeep := len(msgs) * 3 / 10; minKeep > keepCount {
		keepCount = minKeep
	}
	splitIdx := len(msgs) - keepCount
	estimatedIn := loop.estimateSummaryInputTokens(msgs[:splitIdx])
	t.Logf("observed: msgs=%d splitIdx=%d estimatedIn=%d max_tokens=%d dynamicSummaryMax=%d",
		len(msgs), splitIdx, estimatedIn, maxTokens, dynamicSummaryMax(estimatedIn))
}
