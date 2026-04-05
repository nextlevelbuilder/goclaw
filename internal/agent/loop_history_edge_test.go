package agent

import (
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// --- sanitizeHistory: all-tool-message history ---
// After aggressive truncation, history could be ALL tool messages.
// Should return nil (no useful messages), not crash.

func TestSanitizeHistory_AllToolMessages(t *testing.T) {
	msgs := []providers.Message{
		{Role: "tool", Content: "result1", ToolCallID: "tc1"},
		{Role: "tool", Content: "result2", ToolCallID: "tc2"},
		{Role: "tool", Content: "result3", ToolCallID: "tc3"},
	}

	result, dropped := sanitizeHistory(msgs)
	if result != nil {
		t.Fatalf("all-tool history should return nil, got %d messages", len(result))
	}
	if dropped != 3 {
		t.Fatalf("expected 3 dropped, got %d", dropped)
	}
}

// --- sanitizeHistory: single user message (no tools) ---

func TestSanitizeHistory_SingleUserMessage(t *testing.T) {
	msgs := []providers.Message{
		{Role: "user", Content: "hello"},
	}

	result, dropped := sanitizeHistory(msgs)
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	if dropped != 0 {
		t.Fatalf("expected 0 dropped, got %d", dropped)
	}
}

// --- sanitizeHistory: assistant with tool_calls but ALL results missing ---
// Every tool_call should get a synthesized "[Tool result missing]" placeholder.

func TestSanitizeHistory_AllToolResultsMissing(t *testing.T) {
	msgs := []providers.Message{
		{Role: "user", Content: "do something"},
		{Role: "assistant", Content: "", ToolCalls: []providers.ToolCall{
			{ID: "tc1", Name: "read_file", Arguments: map[string]any{"path": "a.go"}},
			{ID: "tc2", Name: "read_file", Arguments: map[string]any{"path": "b.go"}},
			{ID: "tc3", Name: "read_file", Arguments: map[string]any{"path": "c.go"}},
		}},
		// No tool results at all — all missing
		{Role: "user", Content: "next message"},
	}

	result, dropped := sanitizeHistory(msgs)

	// Should have: user + assistant + 3 synthesized tool results + user
	if len(result) != 6 {
		t.Fatalf("expected 6 messages (user + assistant + 3 synth + user), got %d", len(result))
	}
	if dropped != 3 {
		t.Fatalf("expected 3 synthesized (counted as dropped), got %d", dropped)
	}

	// Verify synthesized messages
	for i := 2; i <= 4; i++ {
		if result[i].Role != "tool" {
			t.Fatalf("message %d: expected role 'tool', got %q", i, result[i].Role)
		}
		if !strings.Contains(result[i].Content, "missing") {
			t.Fatalf("message %d: expected 'missing' placeholder, got %q", i, result[i].Content)
		}
	}
}

// --- sanitizeHistory: interleaved tool results from wrong assistant ---
// tool_result IDs that don't match the preceding assistant's tool_calls
// should be dropped.

func TestSanitizeHistory_ToolResultsFromWrongAssistant(t *testing.T) {
	msgs := []providers.Message{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "", ToolCalls: []providers.ToolCall{
			{ID: "tc_a", Name: "read_file", Arguments: map[string]any{}},
		}},
		// Tool result with wrong ID (from a different assistant turn)
		{Role: "tool", Content: "wrong result", ToolCallID: "tc_z"},
		// Correct result
		// (missing — will be synthesized)
		{Role: "user", Content: "next"},
	}

	result, dropped := sanitizeHistory(msgs)

	// tc_z dropped, tc_a synthesized
	if dropped != 2 {
		t.Fatalf("expected 2 dropped (1 mismatched + 1 synthesized), got %d", dropped)
	}

	// Should have: user + assistant + synth(tc_a) + user
	if len(result) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(result))
	}
	if result[2].ToolCallID != "tc_a" {
		t.Fatalf("synthesized result should have tc_a, got %q", result[2].ToolCallID)
	}
}

// --- sanitizeHistory: multiple tool_calls with partial results ---
// 3 tool_calls but only 1 result provided → 2 synthesized

func TestSanitizeHistory_PartialToolResults(t *testing.T) {
	msgs := []providers.Message{
		{Role: "user", Content: "go"},
		{Role: "assistant", Content: "", ToolCalls: []providers.ToolCall{
			{ID: "tc1", Name: "read_file", Arguments: map[string]any{"path": "a"}},
			{ID: "tc2", Name: "write_file", Arguments: map[string]any{"path": "b"}},
			{ID: "tc3", Name: "exec", Arguments: map[string]any{"cmd": "ls"}},
		}},
		{Role: "tool", Content: "content of a", ToolCallID: "tc1"},
		// tc2 and tc3 missing
		{Role: "user", Content: "done"},
	}

	result, dropped := sanitizeHistory(msgs)

	// user + assistant + tc1_result + tc2_synth + tc3_synth + user
	if len(result) != 6 {
		t.Fatalf("expected 6 messages, got %d", len(result))
	}
	if dropped != 2 {
		t.Fatalf("expected 2 synthesized, got %d", dropped)
	}
	// Verify order: real result first, then synthesized
	if result[2].ToolCallID != "tc1" || strings.Contains(result[2].Content, "missing") {
		t.Fatal("tc1 should be the real result, not synthesized")
	}
	if result[3].ToolCallID != "tc2" || !strings.Contains(result[3].Content, "missing") {
		t.Fatal("tc2 should be synthesized")
	}
	if result[4].ToolCallID != "tc3" || !strings.Contains(result[4].Content, "missing") {
		t.Fatal("tc3 should be synthesized")
	}
}

// --- sanitizeHistory: tool message between two user messages (orphaned mid-history) ---

func TestSanitizeHistory_OrphanedToolBetweenUsers(t *testing.T) {
	msgs := []providers.Message{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "response"},
		{Role: "tool", Content: "orphaned", ToolCallID: "tc_orphan"}, // no preceding tool_calls
		{Role: "user", Content: "second"},
	}

	result, dropped := sanitizeHistory(msgs)

	if dropped != 1 {
		t.Fatalf("expected 1 dropped orphan, got %d", dropped)
	}
	// user + assistant + user (orphan dropped)
	if len(result) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result))
	}
}

// --- sanitizeHistory: dedup with 2 identical IDs across 2 turns ---
// Turn 1: ID "dup" → kept as-is (first occurrence)
// Turn 2: ID "dup" → rewritten to "dup_dedup_0", tool result matched via idQueue

func TestSanitizeHistory_DedupAcrossTwoTurns(t *testing.T) {
	msgs := []providers.Message{
		// Turn 1
		{Role: "user", Content: "1"},
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "dup", Name: "read_file", Arguments: map[string]any{}}}},
		{Role: "tool", Content: "r1", ToolCallID: "dup"},
		// Turn 2 — same ID
		{Role: "user", Content: "2"},
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "dup", Name: "read_file", Arguments: map[string]any{}}}},
		{Role: "tool", Content: "r2", ToolCallID: "dup"},
	}

	result, dropped := sanitizeHistory(msgs)

	// All 6 messages should be preserved (dedup rewrites turn 2's ID)
	if len(result) != 6 {
		t.Fatalf("expected 6 messages preserved via dedup, got %d", len(result))
	}
	if dropped != 0 {
		t.Fatalf("expected 0 dropped, got %d", dropped)
	}

	// Turn 1 assistant should keep original ID
	if result[1].ToolCalls[0].ID != "dup" {
		t.Fatalf("turn 1 ID should be 'dup', got %q", result[1].ToolCalls[0].ID)
	}
	// Turn 2 assistant should have rewritten ID
	if result[4].ToolCalls[0].ID == "dup" {
		t.Fatal("turn 2 ID should be rewritten, still 'dup'")
	}
	// Turn 2 tool result should match the rewritten ID
	if result[5].ToolCallID != result[4].ToolCalls[0].ID {
		t.Fatalf("turn 2 tool result ID %q should match assistant ID %q",
			result[5].ToolCallID, result[4].ToolCalls[0].ID)
	}
}

// --- sanitizeHistory: performance with large history ---
// Verify sanitization doesn't degrade catastrophically with many messages.

func TestSanitizeHistory_LargeHistory_Performance(t *testing.T) {
	// Build 1000-message history with proper tool pairing
	msgs := make([]providers.Message, 0, 1000)
	for i := range 250 {
		tcID := "tc_" + strings.Repeat("x", 5) + "_" + string(rune('a'+i%26)) + string(rune('0'+i%10))
		msgs = append(msgs,
			providers.Message{Role: "user", Content: "question " + tcID},
			providers.Message{Role: "assistant", ToolCalls: []providers.ToolCall{
				{ID: tcID, Name: "read_file", Arguments: map[string]any{"path": "file.go"}},
			}},
			providers.Message{Role: "tool", Content: "result", ToolCallID: tcID},
			providers.Message{Role: "assistant", Content: "answer"},
		)
	}

	result, dropped := sanitizeHistory(msgs)

	if dropped != 0 {
		t.Fatalf("well-formed history should have 0 drops, got %d", dropped)
	}
	if len(result) != 1000 {
		t.Fatalf("expected 1000 messages, got %d", len(result))
	}
}

// --- sanitizeHistory: empty ToolCallID in tool result ---

func TestSanitizeHistory_EmptyToolCallID(t *testing.T) {
	msgs := []providers.Message{
		{Role: "user", Content: "go"},
		{Role: "assistant", ToolCalls: []providers.ToolCall{
			{ID: "tc1", Name: "read_file", Arguments: map[string]any{}},
		}},
		{Role: "tool", Content: "result", ToolCallID: ""}, // empty ID — won't match
	}

	result, dropped := sanitizeHistory(msgs)

	// Empty ID tool result should be dropped (mismatched)
	// tc1 should get a synthesized result
	if dropped != 2 {
		t.Fatalf("expected 2 dropped (1 mismatched + 1 synthesized), got %d", dropped)
	}
	// user + assistant + synth(tc1)
	if len(result) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result))
	}
}
