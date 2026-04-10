package agent

import (
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// BenchmarkLimitHistoryTurns_200Messages_Limit20 benchmarks limiting history to last 20 user turns.
// Simulates a conversation with 200 messages (100 user + 100 assistant).
func BenchmarkLimitHistoryTurns_200Messages_Limit20(b *testing.B) {
	msgs := makeHistoryMessages(100) // 100 user-assistant pairs = 200 messages

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		limitHistoryTurns(msgs, 20)
	}
}

// BenchmarkLimitHistoryTurns_500Messages_Limit10 benchmarks limiting history to last 10 user turns.
// Simulates a longer conversation with 500 messages.
func BenchmarkLimitHistoryTurns_500Messages_Limit10(b *testing.B) {
	msgs := makeHistoryMessages(250) // 250 user-assistant pairs = 500 messages

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		limitHistoryTurns(msgs, 10)
	}
}

// BenchmarkLimitHistoryTurns_1000Messages_Limit5 benchmarks limiting history to last 5 user turns.
// Simulates a very long conversation with 1000 messages.
func BenchmarkLimitHistoryTurns_1000Messages_Limit5(b *testing.B) {
	msgs := makeHistoryMessages(500) // 500 user-assistant pairs = 1000 messages

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		limitHistoryTurns(msgs, 5)
	}
}

// BenchmarkSanitizeHistory_Clean benchmarks sanitizing already-clean history.
// No orphaned messages, tool calls matched correctly.
func BenchmarkSanitizeHistory_Clean(b *testing.B) {
	msgs := makeHistoryMessages(50) // 50 user-assistant pairs

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sanitizeHistory(msgs)
	}
}

// BenchmarkSanitizeHistory_WithOrphaned benchmarks sanitizing history with orphaned tool messages.
// Common after history truncation.
func BenchmarkSanitizeHistory_WithOrphaned(b *testing.B) {
	msgs := makeHistoryMessages(50)
	// Insert orphaned tool messages at the start
	msgs = append([]providers.Message{
		{Role: "tool", ToolCallID: "orphan1", Content: "orphaned result 1"},
		{Role: "tool", ToolCallID: "orphan2", Content: "orphaned result 2"},
		{Role: "tool", ToolCallID: "orphan3", Content: "orphaned result 3"},
	}, msgs...)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sanitizeHistory(msgs)
	}
}

// BenchmarkSanitizeHistory_WithToolCalls benchmarks sanitizing history with tool calls and results.
// Tests the tool call matching and deduplication logic.
func BenchmarkSanitizeHistory_WithToolCalls(b *testing.B) {
	msgs := []providers.Message{
		{Role: "user", Content: "Use the database tool"},
		{Role: "assistant", Content: "I'll query the database", ToolCalls: []providers.ToolCall{
			{ID: "call_123", Name: "database", Arguments: map[string]any{"query": "SELECT * FROM users"}},
		}},
		{Role: "tool", ToolCallID: "call_123", Content: "Results: 5 users"},
		{Role: "user", Content: "What about the logs?"},
		{Role: "assistant", Content: "Let me check the logs", ToolCalls: []providers.ToolCall{
			{ID: "call_456", Name: "file_read", Arguments: map[string]any{"path": "/var/log/app.log"}},
		}},
		{Role: "tool", ToolCallID: "call_456", Content: "Log contents here"},
		{Role: "user", Content: "Thanks"},
	}

	// Repeat to make it longer
	for i := 0; i < 20; i++ {
		msgs = append(msgs, msgs[:7]...)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sanitizeHistory(msgs)
	}
}

// BenchmarkSanitizeHistory_WithMissingToolResults benchmarks sanitizing history
// with missing tool results that need to be synthesized.
func BenchmarkSanitizeHistory_WithMissingToolResults(b *testing.B) {
	msgs := []providers.Message{
		{Role: "user", Content: "Use multiple tools"},
		{Role: "assistant", Content: "I'll call multiple tools", ToolCalls: []providers.ToolCall{
			{ID: "call_1", Name: "tool1", Arguments: map[string]any{"param": "value1"}},
			{ID: "call_2", Name: "tool2", Arguments: map[string]any{"param": "value2"}},
			{ID: "call_3", Name: "tool3", Arguments: map[string]any{"param": "value3"}},
		}},
		// Only return result for first tool call
		{Role: "tool", ToolCallID: "call_1", Content: "Result 1"},
		{Role: "user", Content: "What happened?"},
	}

	// Repeat to make it longer
	for i := 0; i < 25; i++ {
		msgs = append(msgs, msgs[:5]...)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sanitizeHistory(msgs)
	}
}

// BenchmarkSanitizeHistory_ConsecutiveSameRole benchmarks merging consecutive same-role messages.
// Tests role alternation fixing.
func BenchmarkSanitizeHistory_ConsecutiveSameRole(b *testing.B) {
	msgs := []providers.Message{
		{Role: "user", Content: "First user message"},
		{Role: "user", Content: "Second user message"},
		{Role: "user", Content: "Third user message"},
		{Role: "assistant", Content: "Response"},
		{Role: "user", Content: "Next turn"},
		{Role: "assistant", Content: "First assistant response"},
		{Role: "assistant", Content: "Second assistant response"},
		{Role: "user", Content: "Final user message"},
	}

	// Repeat to make it longer
	for i := 0; i < 30; i++ {
		msgs = append(msgs, msgs[:8]...)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sanitizeHistory(msgs)
	}
}

// makeHistoryMessages creates a realistic conversation history with alternating user/assistant messages.
// Each pair includes content and some assistant messages have tool calls.
func makeHistoryMessages(pairs int) []providers.Message {
	msgs := make([]providers.Message, 0, pairs*2)
	content := strings.Repeat("message content ", 10)

	for i := 0; i < pairs; i++ {
		// Add user message
		msgs = append(msgs, providers.Message{
			Role:    "user",
			Content: content,
		})

		// Add assistant message (some with tool calls)
		assistantMsg := providers.Message{
			Role:    "assistant",
			Content: content,
		}

		if i%5 == 0 {
			assistantMsg.ToolCalls = []providers.ToolCall{
				{
					ID:   "call_" + string(rune('a'+i%26)),
					Name: "tool_name",
					Arguments: map[string]any{
						"param1": "value1",
						"param2": 42,
					},
				},
			}
		}

		msgs = append(msgs, assistantMsg)

		// If assistant had tool calls, add tool result
		if i%5 == 0 {
			msgs = append(msgs, providers.Message{
				Role:       "tool",
				ToolCallID: "call_" + string(rune('a'+i%26)),
				Content:    "Tool result content",
			})
		}
	}

	return msgs
}
