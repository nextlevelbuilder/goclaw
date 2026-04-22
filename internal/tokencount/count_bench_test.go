package tokencount

import (
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// BenchmarkCount_100Tokens benchmarks token counting for ~100 token input.
func BenchmarkCount_100Tokens(b *testing.B) {
	tc := NewTiktokenCounter()
	text := strings.Repeat("hello world ", 10) // ~100 tokens

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		tc.Count("claude-3-5-sonnet-20241022", text)
	}
}

// BenchmarkCount_1KTokens benchmarks token counting for ~1K token input.
func BenchmarkCount_1KTokens(b *testing.B) {
	tc := NewTiktokenCounter()
	text := strings.Repeat("hello world ", 100) // ~1K tokens

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		tc.Count("claude-3-5-sonnet-20241022", text)
	}
}

// BenchmarkCount_10KTokens benchmarks token counting for ~10K token input.
func BenchmarkCount_10KTokens(b *testing.B) {
	tc := NewTiktokenCounter()
	text := strings.Repeat("hello world ", 1000) // ~10K tokens

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		tc.Count("claude-3-5-sonnet-20241022", text)
	}
}

// BenchmarkCountMessages benchmarks token counting for message lists.
// Tests with 5 messages of varying lengths.
func BenchmarkCountMessages(b *testing.B) {
	tc := NewTiktokenCounter()
	msgs := []providers.Message{
		{Role: "user", Content: strings.Repeat("hello ", 50)},
		{Role: "assistant", Content: strings.Repeat("world ", 100)},
		{Role: "user", Content: strings.Repeat("test ", 75)},
		{Role: "assistant", Content: strings.Repeat("response ", 150)},
		{Role: "user", Content: "final message"},
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		tc.CountMessages("claude-3-5-sonnet-20241022", msgs)
	}
}

// BenchmarkCountMessages_LargeHistory benchmarks token counting for a long conversation.
// Tests with 50 messages of varying lengths.
func BenchmarkCountMessages_LargeHistory(b *testing.B) {
	tc := NewTiktokenCounter()
	msgs := make([]providers.Message, 50)
	for i := range 50 {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		msgs[i] = providers.Message{
			Role:    role,
			Content: strings.Repeat("message content ", 20),
		}
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		tc.CountMessages("claude-3-5-sonnet-20241022", msgs)
	}
}

// BenchmarkModelContextWindow benchmarks looking up context window for a model.
func BenchmarkModelContextWindow(b *testing.B) {
	tc := NewTiktokenCounter()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		tc.ModelContextWindow("claude-3-5-sonnet-20241022")
	}
}
