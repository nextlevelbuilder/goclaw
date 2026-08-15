package pipeline

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

func TestMessageBuffer_All_OrderIsSystemHistoryPending(t *testing.T) {
	t.Parallel()
	sys := providers.Message{Role: "system", Content: "system"}
	h1 := providers.Message{Role: "user", Content: "h1"}
	h2 := providers.Message{Role: "assistant", Content: "h2"}
	p1 := providers.Message{Role: "user", Content: "p1"}

	mb := NewMessageBuffer(sys)
	mb.SetHistory([]providers.Message{h1, h2})
	mb.AppendPending(p1)

	all := mb.All()
	if len(all) != 4 {
		t.Fatalf("All() len = %d, want 4", len(all))
	}
	if all[0].Content != "system" {
		t.Errorf("all[0] = %q, want system", all[0].Content)
	}
	if all[1].Content != "h1" {
		t.Errorf("all[1] = %q, want h1", all[1].Content)
	}
	if all[2].Content != "h2" {
		t.Errorf("all[2] = %q, want h2", all[2].Content)
	}
	if all[3].Content != "p1" {
		t.Errorf("all[3] = %q, want p1", all[3].Content)
	}
}

func TestMessageBuffer_All_EmptyBuffer(t *testing.T) {
	t.Parallel()
	sys := providers.Message{Role: "system", Content: "sys"}
	mb := NewMessageBuffer(sys)

	all := mb.All()
	if len(all) != 1 {
		t.Fatalf("All() len = %d, want 1 (system only)", len(all))
	}
	if all[0].Content != "sys" {
		t.Errorf("all[0] = %q, want sys", all[0].Content)
	}
}

func TestMessageBuffer_AppendPending_AddsToEnd(t *testing.T) {
	t.Parallel()
	mb := NewMessageBuffer(providers.Message{Role: "system", Content: "s"})

	mb.AppendPending(providers.Message{Role: "user", Content: "a"})
	mb.AppendPending(providers.Message{Role: "user", Content: "b"})

	pending := mb.Pending()
	if len(pending) != 2 {
		t.Fatalf("Pending() len = %d, want 2", len(pending))
	}
	if pending[0].Content != "a" || pending[1].Content != "b" {
		t.Errorf("pending order wrong: %v", pending)
	}
}

func TestMessageBuffer_FlushPending_MovesAndClears(t *testing.T) {
	t.Parallel()
	mb := NewMessageBuffer(providers.Message{Role: "system", Content: "s"})
	mb.AppendPending(providers.Message{Role: "user", Content: "p1"})
	mb.AppendPending(providers.Message{Role: "assistant", Content: "p2"})

	flushed := mb.FlushPending()

	if len(flushed) != 2 {
		t.Fatalf("FlushPending returned %d messages, want 2", len(flushed))
	}
	if flushed[0].Content != "p1" || flushed[1].Content != "p2" {
		t.Errorf("flushed order wrong: %v", flushed)
	}

	// pending should be cleared
	if len(mb.Pending()) != 0 {
		t.Errorf("Pending after flush = %d, want 0", len(mb.Pending()))
	}

	// history should contain the flushed messages
	if len(mb.History()) != 2 {
		t.Errorf("History after flush = %d, want 2", len(mb.History()))
	}
}

func TestMessageBuffer_FlushPending_EmptyPending(t *testing.T) {
	t.Parallel()
	mb := NewMessageBuffer(providers.Message{Role: "system", Content: "s"})

	flushed := mb.FlushPending()
	if len(flushed) != 0 {
		t.Errorf("FlushPending on empty = %d, want 0", len(flushed))
	}
}

func TestMessageBuffer_ReplaceHistory_ClearsPending(t *testing.T) {
	t.Parallel()
	mb := NewMessageBuffer(providers.Message{Role: "system", Content: "s"})
	mb.AppendPending(providers.Message{Role: "user", Content: "pending"})
	mb.SetHistory([]providers.Message{
		{Role: "user", Content: "old"},
	})

	newHistory := []providers.Message{
		{Role: "user", Content: "compacted"},
	}
	mb.ReplaceHistory(newHistory)

	if len(mb.History()) != 1 || mb.History()[0].Content != "compacted" {
		t.Errorf("History after ReplaceHistory = %v", mb.History())
	}
	if len(mb.Pending()) != 0 {
		t.Errorf("Pending after ReplaceHistory = %d, want 0", len(mb.Pending()))
	}
}

func TestMessageBuffer_HistoryLen(t *testing.T) {
	t.Parallel()
	mb := NewMessageBuffer(providers.Message{Role: "system", Content: "s"})
	if mb.HistoryLen() != 0 {
		t.Errorf("HistoryLen initial = %d, want 0", mb.HistoryLen())
	}

	mb.SetHistory([]providers.Message{
		{Role: "user", Content: "a"},
		{Role: "assistant", Content: "b"},
	})
	if mb.HistoryLen() != 2 {
		t.Errorf("HistoryLen = %d, want 2", mb.HistoryLen())
	}
}

func TestMessageBuffer_TotalLen(t *testing.T) {
	t.Parallel()
	mb := NewMessageBuffer(providers.Message{Role: "system", Content: "s"})
	// just system = 1
	if mb.TotalLen() != 1 {
		t.Errorf("TotalLen initial = %d, want 1", mb.TotalLen())
	}

	mb.SetHistory([]providers.Message{
		{Role: "user", Content: "h"},
	})
	mb.AppendPending(providers.Message{Role: "assistant", Content: "p"})

	// system(1) + history(1) + pending(1) = 3
	if mb.TotalLen() != 3 {
		t.Errorf("TotalLen = %d, want 3", mb.TotalLen())
	}
}

func TestMessageBuffer_SetSystem_UpdatesSystem(t *testing.T) {
	t.Parallel()
	mb := NewMessageBuffer(providers.Message{Role: "system", Content: "original"})
	mb.SetSystem(providers.Message{Role: "system", Content: "updated"})

	if mb.System().Content != "updated" {
		t.Errorf("System() = %q, want updated", mb.System().Content)
	}
	// All() should return new system
	all := mb.All()
	if all[0].Content != "updated" {
		t.Errorf("All()[0] = %q, want updated", all[0].Content)
	}
}

func TestMessageBuffer_FlushPending_AccumulatesHistory(t *testing.T) {
	t.Parallel()
	mb := NewMessageBuffer(providers.Message{Role: "system", Content: "s"})

	// first flush
	mb.AppendPending(providers.Message{Role: "user", Content: "a"})
	mb.FlushPending()

	// second flush
	mb.AppendPending(providers.Message{Role: "assistant", Content: "b"})
	mb.FlushPending()

	if mb.HistoryLen() != 2 {
		t.Errorf("HistoryLen after 2 flushes = %d, want 2", mb.HistoryLen())
	}
}
