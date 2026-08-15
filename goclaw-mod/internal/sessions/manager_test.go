package sessions

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// --- SessionKey ---

func TestSessionKey(t *testing.T) {
	got := SessionKey("agent-123", "telegram:direct:chat456")
	want := "agent:agent-123:telegram:direct:chat456"
	if got != want {
		t.Fatalf("SessionKey = %q, want %q", got, want)
	}
}

// --- GetOrCreate idempotency ---

func TestGetOrCreate_Idempotent(t *testing.T) {
	m := NewManager("")
	ctx := context.Background()

	s1 := m.GetOrCreate(ctx, "agent:a1:scope1")
	s2 := m.GetOrCreate(ctx, "agent:a1:scope1")

	if s1 != s2 {
		t.Fatal("GetOrCreate should return same session pointer for same key")
	}
}

func TestGetOrCreate_DifferentKeys(t *testing.T) {
	m := NewManager("")
	ctx := context.Background()

	s1 := m.GetOrCreate(ctx, "agent:a1:scope1")
	s2 := m.GetOrCreate(ctx, "agent:a2:scope2")

	if s1 == s2 {
		t.Fatal("different keys should return different sessions")
	}
}

// --- AddMessage ---

func TestAddMessage_CreatesSession(t *testing.T) {
	m := NewManager("")
	ctx := context.Background()

	m.AddMessage(ctx, "agent:a1:s1", providers.Message{Role: "user", Content: "hello"})

	history := m.GetHistory(ctx, "agent:a1:s1")
	if len(history) != 1 {
		t.Fatalf("expected 1 message, got %d", len(history))
	}
	if history[0].Content != "hello" {
		t.Fatalf("content mismatch: got %q", history[0].Content)
	}
}

func TestAddMessage_AppendsToExisting(t *testing.T) {
	m := NewManager("")
	ctx := context.Background()

	m.AddMessage(ctx, "key1", providers.Message{Role: "user", Content: "msg1"})
	m.AddMessage(ctx, "key1", providers.Message{Role: "assistant", Content: "msg2"})
	m.AddMessage(ctx, "key1", providers.Message{Role: "user", Content: "msg3"})

	history := m.GetHistory(ctx, "key1")
	if len(history) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(history))
	}
}

// --- Concurrent AddMessage: messages must not be lost ---

func TestAddMessage_ConcurrentSafety(t *testing.T) {
	m := NewManager("")
	ctx := context.Background()
	key := "agent:concurrent:test"

	const n = 100
	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func(i int) {
			defer wg.Done()
			m.AddMessage(ctx, key, providers.Message{Role: "user", Content: "msg"})
		}(i)
	}
	wg.Wait()

	history := m.GetHistory(ctx, key)
	if len(history) != n {
		t.Fatalf("expected %d messages (no loss), got %d", n, len(history))
	}
}

// --- GetHistory returns defensive copy ---

func TestGetHistory_DefensiveCopy(t *testing.T) {
	m := NewManager("")
	ctx := context.Background()
	key := "agent:a1:s1"

	m.AddMessage(ctx, key, providers.Message{Role: "user", Content: "original"})

	history := m.GetHistory(ctx, key)
	history[0].Content = "mutated" // mutate the copy

	// Original should be unchanged
	original := m.GetHistory(ctx, key)
	if original[0].Content != "original" {
		t.Fatal("GetHistory should return a copy, not a reference to internal slice")
	}
}

func TestGetHistory_NonExistentSession(t *testing.T) {
	m := NewManager("")
	history := m.GetHistory(context.Background(), "nonexistent")
	if history != nil {
		t.Fatalf("expected nil for non-existent session, got %v", history)
	}
}

// --- SetLabel / SetSummary on missing session: silent no-op ---

func TestSetLabel_MissingSession_SilentNoOp(t *testing.T) {
	m := NewManager("")
	// Should not panic
	m.SetLabel(context.Background(), "nonexistent", "label")
}

func TestSetSummary_MissingSession_SilentNoOp(t *testing.T) {
	m := NewManager("")
	m.SetSummary(context.Background(), "nonexistent", "summary")
}

// --- Metadata accumulation ---

func TestAccumulateTokens(t *testing.T) {
	m := NewManager("")
	ctx := context.Background()
	key := "agent:a1:s1"

	m.GetOrCreate(ctx, key)
	m.AccumulateTokens(ctx, key, 100, 50)
	m.AccumulateTokens(ctx, key, 200, 80)

	m.mu.RLock()
	s := m.sessions[key]
	m.mu.RUnlock()

	if s.InputTokens != 300 {
		t.Fatalf("input tokens: got %d, want 300", s.InputTokens)
	}
	if s.OutputTokens != 130 {
		t.Fatalf("output tokens: got %d, want 130", s.OutputTokens)
	}
}

// --- CompactionCount tracking ---

func TestCompactionCount(t *testing.T) {
	m := NewManager("")
	ctx := context.Background()
	key := "agent:a1:s1"

	m.GetOrCreate(ctx, key)

	if m.GetCompactionCount(ctx, key) != 0 {
		t.Fatal("initial compaction count should be 0")
	}

	m.IncrementCompaction(ctx, key)
	m.IncrementCompaction(ctx, key)

	if m.GetCompactionCount(ctx, key) != 2 {
		t.Fatalf("compaction count: got %d, want 2", m.GetCompactionCount(ctx, key))
	}

	// MemoryFlushCompactionCount tracks separately
	if m.GetMemoryFlushCompactionCount(ctx, key) != 0 {
		t.Fatal("memory flush compaction should start at 0")
	}

	m.SetMemoryFlushDone(ctx, key)
	if m.GetMemoryFlushCompactionCount(ctx, key) != 2 {
		t.Fatalf("memory flush should match current compaction count: got %d", m.GetMemoryFlushCompactionCount(ctx, key))
	}
}

func TestGetMemoryFlushCompactionCount_NonExistent(t *testing.T) {
	m := NewManager("")
	got := m.GetMemoryFlushCompactionCount(context.Background(), "nonexistent")
	if got != -1 {
		t.Fatalf("expected -1 for non-existent session, got %d", got)
	}
}

// --- TruncateHistory ---

func TestTruncateHistory(t *testing.T) {
	m := NewManager("")
	ctx := context.Background()
	key := "agent:a1:s1"

	for range 10 {
		m.AddMessage(ctx, key, providers.Message{Role: "user", Content: "msg"})
	}

	m.TruncateHistory(ctx, key, 3)
	history := m.GetHistory(ctx, key)
	if len(history) != 3 {
		t.Fatalf("expected 3 messages after truncate, got %d", len(history))
	}
}

func TestTruncateHistory_KeepZero(t *testing.T) {
	m := NewManager("")
	ctx := context.Background()
	key := "agent:a1:s1"

	m.AddMessage(ctx, key, providers.Message{Role: "user", Content: "msg"})
	m.TruncateHistory(ctx, key, 0)

	history := m.GetHistory(ctx, key)
	if len(history) != 0 {
		t.Fatalf("expected 0 messages, got %d", len(history))
	}
}

// --- Reset ---

func TestReset_ClearsHistoryAndSummary(t *testing.T) {
	m := NewManager("")
	ctx := context.Background()
	key := "agent:a1:s1"

	m.AddMessage(ctx, key, providers.Message{Role: "user", Content: "msg"})
	m.SetSummary(ctx, key, "summary text")

	m.Reset(ctx, key)

	history := m.GetHistory(ctx, key)
	if len(history) != 0 {
		t.Fatalf("expected empty history after reset, got %d messages", len(history))
	}
	if m.GetSummary(ctx, key) != "" {
		t.Fatal("expected empty summary after reset")
	}
}

// --- Delete ---

func TestDelete_RemovesSession(t *testing.T) {
	m := NewManager("")
	ctx := context.Background()
	key := "agent:a1:s1"

	m.GetOrCreate(ctx, key)
	if err := m.Delete(ctx, key); err != nil {
		t.Fatalf("delete error: %v", err)
	}

	history := m.GetHistory(ctx, key)
	if history != nil {
		t.Fatal("expected nil history after delete")
	}
}

// --- List ---

func TestList_FiltersByAgentID(t *testing.T) {
	m := NewManager("")
	ctx := context.Background()

	m.GetOrCreate(ctx, "agent:a1:scope1")
	m.GetOrCreate(ctx, "agent:a1:scope2")
	m.GetOrCreate(ctx, "agent:a2:scope1")

	all := m.List(ctx, "")
	if len(all) != 3 {
		t.Fatalf("expected 3 sessions, got %d", len(all))
	}

	filtered := m.List(ctx, "a1")
	if len(filtered) != 2 {
		t.Fatalf("expected 2 sessions for agent a1, got %d", len(filtered))
	}
}

// --- ContextWindow ---

func TestContextWindow_SetAndGet(t *testing.T) {
	m := NewManager("")
	ctx := context.Background()
	key := "agent:a1:s1"

	m.GetOrCreate(ctx, key)
	if m.GetContextWindow(ctx, key) != 0 {
		t.Fatal("initial context window should be 0")
	}

	m.SetContextWindow(ctx, key, 200000)
	if m.GetContextWindow(ctx, key) != 200000 {
		t.Fatalf("context window mismatch: got %d", m.GetContextWindow(ctx, key))
	}
}

// --- Persistence: Save and load roundtrip ---

func TestSave_LoadAll_Roundtrip(t *testing.T) {
	dir := t.TempDir()

	// Create manager and save a session
	m1 := NewManager(dir)
	ctx := context.Background()
	key := "agent:a1:scope1"

	m1.AddMessage(ctx, key, providers.Message{Role: "user", Content: "hello"})
	m1.AddMessage(ctx, key, providers.Message{Role: "assistant", Content: "hi"})
	m1.SetLabel(ctx, key, "test-label")
	m1.AccumulateTokens(ctx, key, 100, 50)

	if err := m1.Save(ctx, key); err != nil {
		t.Fatalf("save error: %v", err)
	}

	// Create new manager from same directory → should load the session
	m2 := NewManager(dir)
	history := m2.GetHistory(ctx, key)
	if len(history) != 2 {
		t.Fatalf("expected 2 messages after load, got %d", len(history))
	}
	if history[0].Content != "hello" || history[1].Content != "hi" {
		t.Fatal("message content mismatch after load")
	}

	m2.mu.RLock()
	s := m2.sessions[key]
	m2.mu.RUnlock()
	if s.Label != "test-label" {
		t.Fatalf("label mismatch: got %q", s.Label)
	}
	if s.InputTokens != 100 {
		t.Fatalf("input tokens mismatch: got %d", s.InputTokens)
	}
}

func TestDelete_RemovesFile(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)
	ctx := context.Background()
	key := "agent:a1:scope1"

	m.AddMessage(ctx, key, providers.Message{Role: "user", Content: "msg"})
	_ = m.Save(ctx, key)

	// File should exist
	files, _ := os.ReadDir(dir)
	if len(files) == 0 {
		t.Fatal("expected session file to exist")
	}

	_ = m.Delete(ctx, key)

	// File should be gone
	files, _ = os.ReadDir(dir)
	jsonCount := 0
	for _, f := range files {
		if !f.IsDir() {
			jsonCount++
		}
	}
	if jsonCount != 0 {
		t.Fatalf("expected no session files after delete, got %d", jsonCount)
	}
}

// --- UpdateMetadata ---

func TestUpdateMetadata_PartialUpdate(t *testing.T) {
	m := NewManager("")
	ctx := context.Background()
	key := "agent:a1:s1"

	m.GetOrCreate(ctx, key)
	m.UpdateMetadata(ctx, key, "claude-3", "anthropic", "telegram")

	m.mu.RLock()
	s := m.sessions[key]
	m.mu.RUnlock()

	if s.Model != "claude-3" || s.Provider != "anthropic" || s.Channel != "telegram" {
		t.Fatalf("metadata mismatch: model=%q provider=%q channel=%q", s.Model, s.Provider, s.Channel)
	}

	// Partial update: only model, rest unchanged
	m.UpdateMetadata(ctx, key, "gpt-4", "", "")
	m.mu.RLock()
	s = m.sessions[key]
	m.mu.RUnlock()

	if s.Model != "gpt-4" {
		t.Fatalf("model should update: got %q", s.Model)
	}
	if s.Provider != "anthropic" {
		t.Fatalf("provider should stay unchanged: got %q", s.Provider)
	}
}

// --- SetSpawnInfo ---

func TestSetSpawnInfo(t *testing.T) {
	m := NewManager("")
	ctx := context.Background()
	key := "agent:a1:s1"

	m.GetOrCreate(ctx, key)
	m.SetSpawnInfo(ctx, key, "parent-run-123", 2)

	m.mu.RLock()
	s := m.sessions[key]
	m.mu.RUnlock()

	if s.SpawnedBy != "parent-run-123" {
		t.Fatalf("spawnedBy: got %q", s.SpawnedBy)
	}
	if s.SpawnDepth != 2 {
		t.Fatalf("spawnDepth: got %d", s.SpawnDepth)
	}
}

// --- LastPromptTokens ---

func TestLastPromptTokens(t *testing.T) {
	m := NewManager("")
	ctx := context.Background()
	key := "agent:a1:s1"

	m.GetOrCreate(ctx, key)
	tokens, msgCount := m.GetLastPromptTokens(ctx, key)
	if tokens != 0 || msgCount != 0 {
		t.Fatal("initial values should be 0")
	}

	m.SetLastPromptTokens(ctx, key, 5000, 42)
	tokens, msgCount = m.GetLastPromptTokens(ctx, key)
	if tokens != 5000 || msgCount != 42 {
		t.Fatalf("got tokens=%d, msgCount=%d", tokens, msgCount)
	}
}

// --- sanitizeFilename ---

func TestSanitizeFilename(t *testing.T) {
	got := sanitizeFilename("agent:a1:telegram:direct:123")
	want := "agent_a1_telegram_direct_123"
	if got != want {
		t.Fatalf("sanitizeFilename = %q, want %q", got, want)
	}
}
