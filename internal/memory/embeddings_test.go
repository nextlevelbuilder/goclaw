package memory

import (
	"strings"
	"testing"
)

func TestEvaluateRawMessages(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		stats := EvaluateRawMessages(nil)
		if stats.TotalMsgs != 0 {
			t.Errorf("TotalMsgs = %d, want 0", stats.TotalMsgs)
		}
		if stats.AvgBodyLen != 0 {
			t.Errorf("AvgBodyLen = %d, want 0", stats.AvgBodyLen)
		}
	})

	t.Run("single", func(t *testing.T) {
		stats := EvaluateRawMessages([]string{"hello"})
		if stats.TotalMsgs != 1 {
			t.Fatalf("TotalMsgs = %d, want 1", stats.TotalMsgs)
		}
		if stats.AvgBodyLen != 5 {
			t.Errorf("AvgBodyLen = %d, want 5", stats.AvgBodyLen)
		}
		if stats.MaxBodyLen != 5 {
			t.Errorf("MaxBodyLen = %d, want 5", stats.MaxBodyLen)
		}
		if stats.P95BodyLen != 5 {
			t.Errorf("P95BodyLen = %d, want 5", stats.P95BodyLen)
		}
	})

	t.Run("multiple", func(t *testing.T) {
		texts := []string{"hi", "hello world", "a", "test message here", "ok"}
		stats := EvaluateRawMessages(texts)
		if stats.TotalMsgs != 5 {
			t.Fatalf("TotalMsgs = %d, want 5", stats.TotalMsgs)
		}
		if stats.MaxBodyLen != 17 { // "test message here"
			t.Errorf("MaxBodyLen = %d, want 17", stats.MaxBodyLen)
		}
		// lengths: 1, 2, 4, 11, 17. P95 index = 5*95/100 = 4 -> lengths[4] = 17
		if stats.P95BodyLen != 17 {
			t.Errorf("P95BodyLen = %d, want 17", stats.P95BodyLen)
		}
		// avg = (2+11+1+17+2)/5 = 33/5 = 6
		if stats.AvgBodyLen != 6 {
			t.Errorf("AvgBodyLen = %d, want 6", stats.AvgBodyLen)
		}
	})

	t.Run("percentile", func(t *testing.T) {
		// 20 messages, lengths 1..20. P95 index = 20*95/100 = 19 -> lengths[19] = 20
		texts := make([]string, 20)
		for i := range texts {
			texts[i] = strings.Repeat("x", i+1)
		}
		stats := EvaluateRawMessages(texts)
		if stats.P95BodyLen != 20 {
			t.Errorf("P95BodyLen = %d, want 20", stats.P95BodyLen)
		}
		if stats.MaxBodyLen != 20 {
			t.Errorf("MaxBodyLen = %d, want 20", stats.MaxBodyLen)
		}
		if stats.AvgBodyLen != 10 { // (1+2+...+20)/20 = 210/20 = 10
			t.Errorf("AvgBodyLen = %d, want 10", stats.AvgBodyLen)
		}
	})
}

func TestEvaluateChunkParams(t *testing.T) {
	t.Run("embeddinggemma", func(t *testing.T) {
		maxChunkLen, chunkOverlap := EvaluateChunkParams("embeddinggemma-300m", ChunkEvalStats{})
		// 8192 * 0.85 * 4 = 27852
		const expected = 27852
		if maxChunkLen != expected {
			t.Errorf("maxChunkLen = %d, want %d", maxChunkLen, expected)
		}
		if chunkOverlap != expected/5 {
			t.Errorf("chunkOverlap = %d, want %d", chunkOverlap, expected/5)
		}
	})

	t.Run("text-embedding-3-small", func(t *testing.T) {
		maxChunkLen, chunkOverlap := EvaluateChunkParams("text-embedding-3-small", ChunkEvalStats{})
		// 8191 * 0.85 * 4 = 27849
		const expected = 27849
		if maxChunkLen != expected {
			t.Errorf("maxChunkLen = %d, want %d", maxChunkLen, expected)
		}
		if chunkOverlap != expected/5 {
			t.Errorf("chunkOverlap = %d, want %d", chunkOverlap, expected/5)
		}
	})

	t.Run("unknown model uses default", func(t *testing.T) {
		maxChunkLen, chunkOverlap := EvaluateChunkParams("custom-embed-v1", ChunkEvalStats{})
		// default 8192 * 0.85 * 4 = 27852
		const expected = 27852
		if maxChunkLen != expected {
			t.Errorf("maxChunkLen = %d, want %d", maxChunkLen, expected)
		}
		if chunkOverlap != expected/5 {
			t.Errorf("chunkOverlap = %d, want %d", chunkOverlap, expected/5)
		}
	})

	t.Run("positive values", func(t *testing.T) {
		maxChunkLen, chunkOverlap := EvaluateChunkParams("embeddinggemma", ChunkEvalStats{})
		if maxChunkLen <= 0 {
			t.Errorf("maxChunkLen = %d, want > 0", maxChunkLen)
		}
		if chunkOverlap <= 0 {
			t.Errorf("chunkOverlap = %d, want > 0", chunkOverlap)
		}
	})

	t.Run("overlap is ~20 percent", func(t *testing.T) {
		maxChunkLen, chunkOverlap := EvaluateChunkParams("embeddinggemma", ChunkEvalStats{})
		ratio := float64(chunkOverlap) / float64(maxChunkLen)
		if ratio < 0.19 || ratio > 0.21 {
			t.Errorf("overlap ratio = %.2f, want ~0.20", ratio)
		}
	})
}
