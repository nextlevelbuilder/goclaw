package agent

import (
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

func TestLimitHistoryTurns_NoLimit(t *testing.T) {
	msgs := []providers.Message{
		{Role: "user", Content: "m1"},
		{Role: "assistant", Content: "r1"},
		{Role: "user", Content: "m2"},
		{Role: "assistant", Content: "r2"},
	}
	got := limitHistoryTurns(msgs, 0)
	if len(got) != 4 {
		t.Errorf("expected 4 messages, got %d", len(got))
	}
}

func TestLimitHistoryTurns_KeepLast2(t *testing.T) {
	msgs := []providers.Message{
		{Role: "user", Content: "m1"},
		{Role: "assistant", Content: "r1"},
		{Role: "user", Content: "m2"},
		{Role: "assistant", Content: "r2"},
		{Role: "user", Content: "m3"},
		{Role: "assistant", Content: "r3"},
	}
	got := limitHistoryTurns(msgs, 2)

	if len(got) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(got))
	}
	if got[0].Content != "m2" {
		t.Errorf("expected m2, got %s", got[0].Content)
	}
}

func TestLimitHistoryTurns_KeepLast1(t *testing.T) {
	msgs := []providers.Message{
		{Role: "user", Content: "m1"},
		{Role: "assistant", Content: "r1"},
		{Role: "user", Content: "m2"},
		{Role: "assistant", Content: "r2"},
	}
	got := limitHistoryTurns(msgs, 1)

	if len(got) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(got))
	}
	if got[0].Content != "m2" {
		t.Errorf("expected m2, got %s", got[0].Content)
	}
}

func TestLimitHistoryTurns_WithToolMessages(t *testing.T) {
	msgs := []providers.Message{
		{Role: "user", Content: "m1"},
		{Role: "assistant", Content: "r1", ToolCalls: []providers.ToolCall{{ID: "tc1", Name: "read_file"}}},
		{Role: "tool", Content: "result1", ToolCallID: "tc1"},
		{Role: "assistant", Content: "final1"},
		{Role: "user", Content: "m2"},
		{Role: "assistant", Content: "r2"},
	}
	got := limitHistoryTurns(msgs, 1)

	if len(got) != 2 {
		t.Fatalf("expected 2 messages (last turn), got %d", len(got))
	}
	if got[0].Content != "m2" {
		t.Errorf("expected m2, got %s", got[0].Content)
	}
}

func TestLimitHistoryTurns_Empty(t *testing.T) {
	got := limitHistoryTurns(nil, 5)
	if len(got) != 0 {
		t.Errorf("expected empty, got %d", len(got))
	}
}

func TestLimitHistoryTurns_LimitExceedsTotal(t *testing.T) {
	msgs := []providers.Message{
		{Role: "user", Content: "m1"},
		{Role: "assistant", Content: "r1"},
	}
	got := limitHistoryTurns(msgs, 100)
	if len(got) != 2 {
		t.Errorf("expected 2, got %d", len(got))
	}
}

func TestSanitizeHistory_Empty(t *testing.T) {
	got, _ := sanitizeHistory(nil)
	if len(got) != 0 {
		t.Errorf("expected empty, got %d", len(got))
	}
}

func TestSanitizeHistory_DropsLeadingOrphanedTools(t *testing.T) {
	msgs := []providers.Message{
		{Role: "tool", Content: "orphan1", ToolCallID: "tc1"},
		{Role: "tool", Content: "orphan2", ToolCallID: "tc2"},
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
	}
	got, _ := sanitizeHistory(msgs)
	if len(got) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(got))
	}
	if got[0].Role != "user" {
		t.Errorf("expected user, got %s", got[0].Role)
	}
}

func TestSanitizeHistory_MatchesToolResults(t *testing.T) {
	msgs := []providers.Message{
		{Role: "user", Content: "do something"},
		{Role: "assistant", Content: "", ToolCalls: []providers.ToolCall{
			{ID: "tc1", Name: "read_file"},
			{ID: "tc2", Name: "write_file"},
		}},
		{Role: "tool", Content: "file data", ToolCallID: "tc1"},
		{Role: "tool", Content: "written", ToolCallID: "tc2"},
		{Role: "assistant", Content: "done"},
	}
	got, _ := sanitizeHistory(msgs)
	if len(got) != 5 {
		t.Fatalf("expected 5, got %d", len(got))
	}
}

func TestSanitizeHistory_SynthesizesMissingToolResult(t *testing.T) {
	msgs := []providers.Message{
		{Role: "user", Content: "do something"},
		{Role: "assistant", Content: "", ToolCalls: []providers.ToolCall{
			{ID: "tc1", Name: "read_file"},
			{ID: "tc2", Name: "write_file"},
		}},
		{Role: "tool", Content: "file data", ToolCallID: "tc1"},
		// tc2 is missing
		{Role: "user", Content: "next"},
	}
	got, _ := sanitizeHistory(msgs)

	// user + assistant + tc1 result + synthesized tc2 result + user
	if len(got) != 5 {
		t.Fatalf("expected 5, got %d", len(got))
	}

	// The synthesized message should be for tc2
	foundSynthesized := false
	for _, m := range got {
		if m.ToolCallID == "tc2" && m.Role == "tool" {
			foundSynthesized = true
			if m.Content != "[Tool result missing — session was compacted]" {
				t.Errorf("unexpected synthesized content: %s", m.Content)
			}
		}
	}
	if !foundSynthesized {
		t.Error("missing synthesized tool result for tc2")
	}
}

func TestSanitizeHistory_DropsMismatchedToolResult(t *testing.T) {
	msgs := []providers.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "", ToolCalls: []providers.ToolCall{
			{ID: "tc1", Name: "read_file"},
		}},
		{Role: "tool", Content: "ok", ToolCallID: "tc1"},
		{Role: "tool", Content: "stray", ToolCallID: "unknown_id"},
		{Role: "user", Content: "next"},
	}
	got, _ := sanitizeHistory(msgs)

	// The stray tool message should be dropped, tc1 result kept
	for _, m := range got {
		if m.ToolCallID == "unknown_id" {
			t.Error("mismatched tool result should be dropped")
		}
	}
}

func TestSanitizeHistory_DropsOrphanedToolMidHistory(t *testing.T) {
	msgs := []providers.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
		{Role: "tool", Content: "orphan mid", ToolCallID: "tc_orphan"},
		{Role: "user", Content: "bye"},
	}
	got, _ := sanitizeHistory(msgs)

	for _, m := range got {
		if m.ToolCallID == "tc_orphan" {
			t.Error("orphaned mid-history tool should be dropped")
		}
	}
	if len(got) != 3 {
		t.Errorf("expected 3, got %d", len(got))
	}
}

func TestSanitizeHistory_DedupsDuplicateIDsAcrossTurns(t *testing.T) {
	msgs := []providers.Message{
		{Role: "user", Content: "turn 1"},
		{Role: "assistant", Content: "", ToolCalls: []providers.ToolCall{
			{ID: "call_abc", Name: "read_file"},
		}},
		{Role: "tool", Content: "result1", ToolCallID: "call_abc"},
		{Role: "user", Content: "turn 2"},
		{Role: "assistant", Content: "", ToolCalls: []providers.ToolCall{
			{ID: "call_abc", Name: "write_file"}, // duplicate ID from earlier turn
		}},
		{Role: "tool", Content: "result2", ToolCallID: "call_abc"},
		{Role: "assistant", Content: "done"},
	}
	got, _ := sanitizeHistory(msgs)

	// Collect all tool call IDs (from assistant messages)
	seen := make(map[string]bool)
	for _, m := range got {
		for _, tc := range m.ToolCalls {
			if seen[tc.ID] {
				t.Errorf("duplicate tool call ID in sanitized output: %s", tc.ID)
			}
			seen[tc.ID] = true
		}
	}

	// Both tool results should be present (paired correctly)
	toolResults := 0
	for _, m := range got {
		if m.Role == "tool" {
			toolResults++
		}
	}
	if toolResults != 2 {
		t.Errorf("expected 2 tool results, got %d", toolResults)
	}
}

func TestSanitizeHistory_DedupsDuplicateIDsWithinTurn(t *testing.T) {
	msgs := []providers.Message{
		{Role: "user", Content: "do two things"},
		{Role: "assistant", Content: "", ToolCalls: []providers.ToolCall{
			{ID: "call_abc", Name: "read_file"},
			{ID: "call_abc", Name: "write_file"}, // same ID within turn
		}},
		{Role: "tool", Content: "result1", ToolCallID: "call_abc"},
		{Role: "tool", Content: "result2", ToolCallID: "call_abc"},
		{Role: "assistant", Content: "done"},
	}
	got, dropped := sanitizeHistory(msgs)
	if dropped != 1 {
		t.Errorf("expected 1 dropped (dedup counts as change), got %d", dropped)
	}

	// Both tool results must be present and paired correctly
	toolResults := 0
	for _, m := range got {
		if m.Role == "tool" {
			toolResults++
		}
	}
	if toolResults != 2 {
		t.Errorf("expected 2 tool results, got %d", toolResults)
	}

	// All tool call IDs must be unique
	seen := make(map[string]bool)
	for _, m := range got {
		for _, tc := range m.ToolCalls {
			if seen[tc.ID] {
				t.Errorf("duplicate tool call ID: %s", tc.ID)
			}
			seen[tc.ID] = true
		}
	}

	// Each tool result ID must match a tool call ID
	callIDs := make(map[string]bool)
	for _, m := range got {
		for _, tc := range m.ToolCalls {
			callIDs[tc.ID] = true
		}
	}
	for _, m := range got {
		if m.Role == "tool" && !callIDs[m.ToolCallID] {
			t.Errorf("tool result ID %s has no matching tool call", m.ToolCallID)
		}
	}
}

func TestSanitizeHistory_NoDedupWhenIDsUnique(t *testing.T) {
	msgs := []providers.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "", ToolCalls: []providers.ToolCall{
			{ID: "tc1", Name: "read_file"},
		}},
		{Role: "tool", Content: "ok", ToolCallID: "tc1"},
		{Role: "user", Content: "next"},
		{Role: "assistant", Content: "", ToolCalls: []providers.ToolCall{
			{ID: "tc2", Name: "write_file"},
		}},
		{Role: "tool", Content: "ok", ToolCallID: "tc2"},
	}
	got, dropped := sanitizeHistory(msgs)
	if dropped != 0 {
		t.Errorf("expected 0 dropped, got %d", dropped)
	}
	if len(got) != 6 {
		t.Errorf("expected 6 messages, got %d", len(got))
	}
	// IDs should be unchanged
	if got[1].ToolCalls[0].ID != "tc1" {
		t.Errorf("expected tc1, got %s", got[1].ToolCalls[0].ID)
	}
	if got[4].ToolCalls[0].ID != "tc2" {
		t.Errorf("expected tc2, got %s", got[4].ToolCalls[0].ID)
	}
}

func TestSanitizeHistory_MergesConsecutiveUserMessages(t *testing.T) {
	msgs := []providers.Message{
		{Role: "user", Content: "hello"},
		{Role: "user", Content: "world"},
		{Role: "assistant", Content: "hi there"},
	}
	got, dropped := sanitizeHistory(msgs)
	if len(got) != 2 {
		t.Fatalf("expected 2 messages after merge, got %d", len(got))
	}
	if got[0].Content != "hello\n\nworld" {
		t.Errorf("expected merged content, got %q", got[0].Content)
	}
	if dropped != 1 {
		t.Errorf("expected 1 dropped, got %d", dropped)
	}
}

func TestSanitizeHistory_MergesConsecutiveAssistantMessages(t *testing.T) {
	msgs := []providers.Message{
		{Role: "user", Content: "question"},
		{Role: "assistant", Content: "part 1"},
		{Role: "assistant", Content: "part 2"},
		{Role: "user", Content: "follow-up"},
	}
	got, dropped := sanitizeHistory(msgs)
	if len(got) != 3 {
		t.Fatalf("expected 3 messages after merge, got %d", len(got))
	}
	if got[1].Content != "part 1\n\npart 2" {
		t.Errorf("expected merged assistant content, got %q", got[1].Content)
	}
	if dropped != 1 {
		t.Errorf("expected 1 dropped, got %d", dropped)
	}
}

func TestSanitizeHistory_DedupClearsRawAssistantContent(t *testing.T) {
	// When dedup rewrites tool call IDs, RawAssistantContent (which has the old IDs)
	// must be cleared so the provider uses the corrected ToolCalls.
	rawContent := []byte(`[{"type":"text","text":"ok"},{"type":"tool_use","id":"tool_1","name":"search","input":{}}]`)
	msgs := []providers.Message{
		{Role: "user", Content: "first"},
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "tool_1", Name: "search"}}, RawAssistantContent: rawContent},
		{Role: "tool", Content: "result1", ToolCallID: "tool_1"},
		{Role: "assistant", Content: "ok"},
		{Role: "user", Content: "second"},
		// Second turn has same tool_1 ID — triggers dedup
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "tool_1", Name: "search"}}, RawAssistantContent: rawContent},
		{Role: "tool", Content: "result2", ToolCallID: "tool_1"},
		{Role: "assistant", Content: "done"},
	}
	got, _ := sanitizeHistory(msgs)
	// First assistant should keep RawAssistantContent (no dedup needed)
	if got[1].RawAssistantContent == nil {
		t.Error("first assistant should keep RawAssistantContent")
	}
	// Second assistant (index 5) should have RawAssistantContent cleared due to dedup
	if got[5].RawAssistantContent != nil {
		t.Error("dedup'd assistant should have RawAssistantContent cleared")
	}
	// Tool result for second turn should have dedup'd ID
	if got[6].ToolCallID == "tool_1" {
		t.Errorf("expected dedup'd tool_call_id, got %q", got[6].ToolCallID)
	}
}

func TestSanitizeHistory_NoMergeForToolCallMessages(t *testing.T) {
	// Assistant messages WITH tool_calls should NOT be merged
	msgs := []providers.Message{
		{Role: "user", Content: "do something"},
		{Role: "assistant", Content: "ok", ToolCalls: []providers.ToolCall{{ID: "tc1", Name: "test"}}},
		{Role: "tool", Content: "result", ToolCallID: "tc1"},
		{Role: "assistant", Content: "done"},
	}
	got, dropped := sanitizeHistory(msgs)
	if len(got) != 4 {
		t.Fatalf("expected 4 messages (no merge), got %d", len(got))
	}
	if dropped != 0 {
		t.Errorf("expected 0 dropped, got %d", dropped)
	}
}

func TestSanitizeHistory_MergePreservesMediaRefs(t *testing.T) {
	msgs := []providers.Message{
		{Role: "user", Content: "here's a photo", MediaRefs: []providers.MediaRef{{Kind: "image", ID: "f1"}}},
		{Role: "user", Content: "and another", MediaRefs: []providers.MediaRef{{Kind: "image", ID: "f2"}}},
		{Role: "assistant", Content: "nice pics"},
	}
	got, dropped := sanitizeHistory(msgs)
	if len(got) != 2 {
		t.Fatalf("expected 2 messages after merge, got %d", len(got))
	}
	if len(got[0].MediaRefs) != 2 {
		t.Errorf("expected 2 media refs after merge, got %d", len(got[0].MediaRefs))
	}
	if got[0].MediaRefs[0].ID != "f1" || got[0].MediaRefs[1].ID != "f2" {
		t.Errorf("media refs not preserved correctly: %+v", got[0].MediaRefs)
	}
	if dropped != 1 {
		t.Errorf("expected 1 dropped, got %d", dropped)
	}
}

func TestUniquifyToolCallIDs(t *testing.T) {
	runID := "abcdef12-3456-7890-abcd-ef1234567890"

	t.Run("empty calls", func(t *testing.T) {
		got := uniquifyToolCallIDs(nil, runID, 0)
		if len(got) != 0 {
			t.Errorf("expected empty, got %d", len(got))
		}
	})

	t.Run("produces fixed-length hashed IDs", func(t *testing.T) {
		calls := []providers.ToolCall{
			{ID: "call_123", Name: "read_file"},
			{ID: "call_456", Name: "write_file"},
		}
		got := uniquifyToolCallIDs(calls, runID, 2)
		// IDs must be exactly 40 chars: "call_" (5) + 35 hex chars
		for i, tc := range got {
			if len(tc.ID) != 40 {
				t.Errorf("call %d: ID length = %d, want 40: %s", i, len(tc.ID), tc.ID)
			}
			if !strings.HasPrefix(tc.ID, "call_") {
				t.Errorf("call %d: ID should start with call_, got: %s", i, tc.ID)
			}
		}
		// Different original IDs must produce different hashed IDs
		if got[0].ID == got[1].ID {
			t.Errorf("different inputs should produce different IDs: %s", got[0].ID)
		}
	})

	t.Run("handles empty ID", func(t *testing.T) {
		calls := []providers.ToolCall{
			{ID: "", Name: "read_file"},
		}
		got := uniquifyToolCallIDs(calls, runID, 0)
		if len(got[0].ID) != 40 {
			t.Errorf("empty input: ID length = %d, want 40: %s", len(got[0].ID), got[0].ID)
		}
		if !strings.HasPrefix(got[0].ID, "call_") {
			t.Errorf("empty input: ID should start with call_, got: %s", got[0].ID)
		}
	})

	t.Run("does not mutate input", func(t *testing.T) {
		calls := []providers.ToolCall{
			{ID: "original", Name: "test"},
		}
		got := uniquifyToolCallIDs(calls, runID, 0)
		if calls[0].ID != "original" {
			t.Error("input was mutated")
		}
		if got[0].ID == "original" {
			t.Error("output should differ from input")
		}
	})

	t.Run("duplicate IDs become unique", func(t *testing.T) {
		calls := []providers.ToolCall{
			{ID: "same_id", Name: "a"},
			{ID: "same_id", Name: "b"},
		}
		got := uniquifyToolCallIDs(calls, runID, 0)
		if got[0].ID == got[1].ID {
			t.Errorf("IDs should be unique, both are: %s", got[0].ID)
		}
	})

	t.Run("includes runID and iteration in hash", func(t *testing.T) {
		calls := []providers.ToolCall{
			{ID: "same_id", Name: "read_file"},
		}
		iter0 := uniquifyToolCallIDs(calls, runID, 0)[0].ID
		iter1 := uniquifyToolCallIDs(calls, runID, 1)[0].ID
		otherRun := uniquifyToolCallIDs(calls, "12345678-1234-5678-1234-567812345678", 0)[0].ID

		if iter0 == iter1 {
			t.Errorf("iteration should affect hashed ID: %s", iter0)
		}
		if iter0 == otherRun {
			t.Errorf("runID should affect hashed ID: %s", iter0)
		}
	})
}

func TestEstimateTokens(t *testing.T) {
	msgs := []providers.Message{
		{Role: "user", Content: "Hello world!"},                // 12 chars → ~4 tokens
		{Role: "assistant", Content: "Hi there, how are you?"}, // 22 chars → ~7 tokens
	}
	got := EstimateTokens(msgs)
	if got <= 0 {
		t.Errorf("expected positive token estimate, got %d", got)
	}
}

func TestEstimateTokensIncludesToolCalls(t *testing.T) {
	// Message with tool calls should estimate higher than content-only.
	contentOnly := []providers.Message{
		{Role: "assistant", Content: "Let me search for that."},
	}
	withToolCalls := []providers.Message{
		{Role: "assistant", Content: "Let me search for that.", ToolCalls: []providers.ToolCall{
			{ID: "tc_1", Name: "web_fetch", Arguments: map[string]any{
				"url": "https://example.com/very-long-path/to/some/resource?query=test",
			}},
		}},
	}

	contentTokens := EstimateTokens(contentOnly)
	toolTokens := EstimateTokens(withToolCalls)

	if toolTokens <= contentTokens {
		t.Errorf("expected tool call tokens (%d) > content-only tokens (%d)", toolTokens, contentTokens)
	}
}

func TestEstimateHistoryTokensSkipsSystem(t *testing.T) {
	msgs := []providers.Message{
		{Role: "system", Content: "You are a helpful assistant with a very long system prompt that includes tool definitions and context files..."},
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there!"},
	}

	allTokens := EstimateTokens(msgs)
	historyTokens := EstimateHistoryTokens(msgs)

	if historyTokens >= allTokens {
		t.Errorf("history tokens (%d) should be less than all tokens (%d)", historyTokens, allTokens)
	}

	// History should only count user + assistant messages.
	expectedHistory := EstimateTokens(msgs[1:])
	if historyTokens != expectedHistory {
		t.Errorf("history tokens (%d) != expected (%d)", historyTokens, expectedHistory)
	}
}

func TestEstimateOverhead(t *testing.T) {
	loop := &Loop{contextWindow: 200000}

	t.Run("no_calibration_data", func(t *testing.T) {
		history := []providers.Message{{Role: "user", Content: "Hello"}}
		overhead := loop.estimateOverhead(history, 0, 0)
		// Should use fallback: 20% of 200k = 40000
		if overhead != 40000 {
			t.Errorf("expected fallback 40000, got %d", overhead)
		}
	})

	t.Run("with_calibration", func(t *testing.T) {
		history := []providers.Message{
			{Role: "user", Content: "Hello world"},
			{Role: "assistant", Content: "Hi there!"},
		}
		// Simulate: LLM reported 50k prompt tokens for 2 messages.
		// History estimate for those 2 messages is small (~7 tokens).
		// So overhead should be ~49993.
		overhead := loop.estimateOverhead(history, 50000, 2)
		if overhead <= 0 {
			t.Errorf("expected positive overhead, got %d", overhead)
		}
		// Should be clamped to 40% of context = 80000
		maxOverhead := int(float64(200000) * 0.4)
		if overhead > maxOverhead {
			t.Errorf("overhead %d exceeds max %d", overhead, maxOverhead)
		}
	})
}

func TestPruneContextMessagesDefaultEnabled(t *testing.T) {
	// Phase 04: nil config → no pruning (opt-in). To enable, use Mode: "cache-ttl".
	// Create messages with a large tool result that exceeds soft trim threshold.
	largeContent := make([]byte, 10000)
	for i := range largeContent {
		largeContent[i] = 'x'
	}
	msgs := []providers.Message{
		{Role: "user", Content: "Search for info"},
		{Role: "assistant", Content: "I'll search.", ToolCalls: []providers.ToolCall{{ID: "tc1", Name: "web_fetch"}}},
		{Role: "tool", Content: string(largeContent), ToolCallID: "tc1"},
		{Role: "assistant", Content: "Found results."},
		{Role: "user", Content: "Thanks"},
		{Role: "assistant", Content: "You're welcome."},
		{Role: "user", Content: "More info"},
		{Role: "assistant", Content: "Sure, here you go."},
	}

	// With cache-ttl mode and small context window (to trigger soft trim ratio > 0.3),
	// pruning should trim the large tool result.
	cfg := &config.ContextPruningConfig{Mode: "cache-ttl"}
	result := pruneContextMessages(msgs, 5000, cfg, nil, "", nil)

	// The large tool result should have been trimmed.
	toolMsg := result[2]
	if len(toolMsg.Content) >= 10000 {
		t.Error("expected large tool result to be trimmed, but it was not")
	}
}

func TestPruneContextMessagesExplicitOff(t *testing.T) {
	msgs := []providers.Message{
		{Role: "user", Content: "Hello"},
	}
	cfg := &config.ContextPruningConfig{Mode: "off"}
	result := pruneContextMessages(msgs, 200000, cfg, nil, "", nil)
	// Should return original messages unchanged.
	if len(result) != len(msgs) {
		t.Errorf("expected %d messages, got %d", len(msgs), len(result))
	}
}

func TestTruncateStr(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"short", "hello", 10, "hello"},
		{"exact", "hello", 5, "hello"},
		{"truncate", "hello world", 5, "hello..."},
		{"empty", "", 5, ""},
		{"unicode", "héllo wörld", 7, "héllo ..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateStr(tt.input, tt.maxLen)
			if tt.maxLen >= len(tt.input) {
				if got != tt.input {
					t.Errorf("got %q, want %q", got, tt.input)
				}
			} else {
				if len(got) == 0 {
					t.Error("truncation returned empty")
				}
			}
		})
	}
}
