package agent

import (
	"fmt"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

func buildFallbackHistory(turns int, filler int) []providers.Message {
	msgs := make([]providers.Message, 0, turns*2)
	for i := 0; i < turns; i++ {
		msgs = append(msgs, providers.Message{
			Role:    "user",
			Content: fmt.Sprintf("turn %d request %s", i, strings.Repeat("x", filler)),
		})
		msgs = append(msgs, providers.Message{
			Role:    "assistant",
			Content: fmt.Sprintf("turn %d answer %s", i, strings.Repeat("y", filler)),
		})
	}
	return msgs
}

// The summarizer needs its own LLM call, so on a big session it reliably times
// out. Before this fallback existed the compaction returned nothing, the caller
// found history still over budget and aborted the run — permanently wedging the
// session. The extract must be non-empty so the run can proceed.
func TestExtractiveCompactionSummaryProducesBoundedSummary(t *testing.T) {
	t.Parallel()
	msgs := buildFallbackHistory(40, 400)

	got := extractiveCompactionSummary(msgs, 20000)

	if strings.TrimSpace(got) == "" {
		t.Fatal("extract is empty; the run would abort with no answer")
	}
	budget := dynamicSummaryMax(estimateCompactionSpanTokens(msgs)) * extractiveCompactionRunesPerToken
	if n := len([]rune(got)); n > budget+500 {
		t.Errorf("extract is %d runes, want <= ~%d so the recount drops under budget", n, budget)
	}
}

// The extract stands in for an LLM summary, so it must fit where that summary
// would have fit. Sizing it off inputCap instead produced a 41k-token extract on
// a 200k-window session — 5x the 8192-token ceiling every real summary obeys —
// so the compacted history stayed over budget and the run aborted anyway. The
// fallback fired and bought nothing. Bound the extract by the OUTPUT budget.
func TestExtractiveCompactionSummaryStaysWithinLLMSummaryCeiling(t *testing.T) {
	t.Parallel()
	// Mirrors the live wedge: a very large span with a very large input cap.
	msgs := buildFallbackHistory(150, 1200)
	const inputCap = 161808

	got := extractiveCompactionSummary(msgs, inputCap)

	gotTokens := len([]rune(got)) / extractiveCompactionRunesPerToken
	ceiling := dynamicSummaryMax(estimateCompactionSpanTokens(msgs))
	if gotTokens > ceiling {
		t.Errorf("extract is ~%d tokens, want <= %d (what the LLM path would emit); "+
			"an oversized extract leaves history over budget and the run aborts",
			gotTokens, ceiling)
	}
	// Hard ceiling: no extract may ever exceed dynamicSummaryMax's own clamp.
	if gotTokens > 8192 {
		t.Errorf("extract is ~%d tokens, above the 8192-token summary clamp", gotTokens)
	}
	if strings.TrimSpace(got) == "" {
		t.Fatal("extract is empty; the run would abort with no answer")
	}
}

// A tiny input cap must still bound the extract: it caps what one summarizer
// request could accept, so the extract can never exceed it.
func TestExtractiveCompactionSummaryRespectsSmallInputCap(t *testing.T) {
	t.Parallel()
	msgs := buildFallbackHistory(40, 400)
	const inputCap = 300

	got := extractiveCompactionSummary(msgs, inputCap)

	if gotTokens := len([]rune(got)) / extractiveCompactionRunesPerToken; gotTokens > inputCap {
		t.Errorf("extract is ~%d tokens, want <= inputCap %d", gotTokens, inputCap)
	}
}

// Recency matters: the agent resumes from the newest turns, so those must
// survive and the oldest are the ones to drop.
func TestExtractiveCompactionSummaryKeepsMostRecentTurns(t *testing.T) {
	t.Parallel()
	msgs := buildFallbackHistory(40, 400)

	got := extractiveCompactionSummary(msgs, 20000)

	if !strings.Contains(got, "turn 39 answer") {
		t.Error("extract dropped the most recent turn")
	}
	if strings.Contains(got, "turn 0 request") {
		t.Error("extract kept the oldest turn; budget should have spent on recent context")
	}
	if !strings.Contains(got, "Omitted") {
		t.Error("extract does not disclose that older turns were dropped")
	}
}

// A history that fits needs no dropping at all.
func TestExtractiveCompactionSummaryKeepsEverythingWhenItFits(t *testing.T) {
	t.Parallel()
	msgs := buildFallbackHistory(2, 10)

	got := extractiveCompactionSummary(msgs, 20000)

	for _, want := range []string{"turn 0 request", "turn 1 answer"} {
		if !strings.Contains(got, want) {
			t.Errorf("extract lost %q even though the whole span fits", want)
		}
	}
	if strings.Contains(got, "Omitted") {
		t.Error("extract claims turns were omitted when everything fit")
	}
}

func TestExtractiveCompactionSummaryHandlesEmptyInput(t *testing.T) {
	t.Parallel()
	if got := extractiveCompactionSummary(nil, 20000); got != "" {
		t.Errorf("extract of empty history = %q, want empty", got)
	}
}

// Tool calls and their results carry the technical payload the agent needs;
// the extract renders whole units so pairing is never split.
func TestExtractiveCompactionSummaryPreservesToolPayload(t *testing.T) {
	t.Parallel()
	msgs := []providers.Message{
		{Role: "user", Content: "find the config"},
		{Role: "assistant", ToolCalls: []providers.ToolCall{
			{ID: "c1", Name: "grep", Arguments: map[string]any{"pattern": "listen_addr"}},
		}},
		{Role: "tool", ToolCallID: "c1", Content: "config.yml:12: listen_addr: 0.0.0.0:8080"},
		{Role: "assistant", Content: "found it at config.yml:12"},
	}

	got := extractiveCompactionSummary(msgs, 20000)

	for _, want := range []string{"grep", "listen_addr", "config.yml:12"} {
		if !strings.Contains(got, want) {
			t.Errorf("extract lost %q from the tool exchange", want)
		}
	}
}
