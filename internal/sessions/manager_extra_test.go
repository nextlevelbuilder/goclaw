package sessions

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// TestSetHistory replaces session history entirely.
func TestSetHistory(t *testing.T) {
	m := NewManager("")
	ctx := context.Background()
	key := "agent:a1:s1"

	m.GetOrCreate(ctx, key)
	m.AddMessage(ctx, key, providers.Message{Role: "user", Content: "old"})

	newMsgs := []providers.Message{
		{Role: "user", Content: "new1"},
		{Role: "assistant", Content: "new2"},
	}
	m.SetHistory(ctx, key, newMsgs)

	history := m.GetHistory(ctx, key)
	if len(history) != 2 {
		t.Fatalf("expected 2 messages after SetHistory, got %d", len(history))
	}
	if history[0].Content != "new1" || history[1].Content != "new2" {
		t.Errorf("unexpected history content: %+v", history)
	}
}

// TestSetHistory_NonExistentSession is a silent no-op.
func TestSetHistory_NonExistentSession(t *testing.T) {
	m := NewManager("")
	// Must not panic
	m.SetHistory(context.Background(), "nonexistent", []providers.Message{{Role: "user", Content: "x"}})
}

// TestGetSummary_NonExistentSession returns empty string.
func TestGetSummary_NonExistentSession(t *testing.T) {
	m := NewManager("")
	got := m.GetSummary(context.Background(), "nonexistent")
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

// TestGetCompactionCount_NonExistentSession returns 0.
func TestGetCompactionCount_NonExistentSession(t *testing.T) {
	m := NewManager("")
	got := m.GetCompactionCount(context.Background(), "nonexistent")
	if got != 0 {
		t.Errorf("expected 0, got %d", got)
	}
}

// TestGetContextWindow_NonExistentSession returns 0.
func TestGetContextWindow_NonExistentSession(t *testing.T) {
	m := NewManager("")
	got := m.GetContextWindow(context.Background(), "nonexistent")
	if got != 0 {
		t.Errorf("expected 0, got %d", got)
	}
}

// TestGetLastPromptTokens_NonExistentSession returns (0, 0).
func TestGetLastPromptTokens_NonExistentSession(t *testing.T) {
	m := NewManager("")
	tokens, msgCount := m.GetLastPromptTokens(context.Background(), "nonexistent")
	if tokens != 0 || msgCount != 0 {
		t.Errorf("expected (0, 0), got (%d, %d)", tokens, msgCount)
	}
}

// TestTruncateHistory_NonExistentSession is a silent no-op.
func TestTruncateHistory_NonExistentSession(t *testing.T) {
	m := NewManager("")
	// Must not panic
	m.TruncateHistory(context.Background(), "nonexistent", 5)
}

// TestTruncateHistory_LessThanKeep keeps all messages when count < keepLast.
func TestTruncateHistory_LessThanKeep(t *testing.T) {
	m := NewManager("")
	ctx := context.Background()
	key := "agent:a1:s1"

	m.AddMessage(ctx, key, providers.Message{Role: "user", Content: "only"})
	m.TruncateHistory(ctx, key, 10) // keepLast > actual count

	history := m.GetHistory(ctx, key)
	if len(history) != 1 {
		t.Fatalf("expected 1 message (no truncation needed), got %d", len(history))
	}
}

// TestLastUsedChannel_Empty returns ("","") for an empty manager.
func TestLastUsedChannel_Empty(t *testing.T) {
	m := NewManager("")
	ch, chatID := m.LastUsedChannel(context.Background(), "my-agent")
	if ch != "" || chatID != "" {
		t.Errorf("expected ('', ''), got (%q, %q)", ch, chatID)
	}
}

// TestLastUsedChannel_FindsMostRecent returns the most recently updated channel session.
func TestLastUsedChannel_FindsMostRecent(t *testing.T) {
	m := NewManager("")
	ctx := context.Background()

	key1 := "agent:my-agent:telegram:direct:111"
	key2 := "agent:my-agent:telegram:direct:222"

	s1 := m.GetOrCreate(ctx, key1)
	s1.Updated = time.Now().Add(-10 * time.Second)

	s2 := m.GetOrCreate(ctx, key2)
	s2.Updated = time.Now()

	ch, chatID := m.LastUsedChannel(ctx, "my-agent")
	if ch != "telegram" {
		t.Errorf("expected channel 'telegram', got %q", ch)
	}
	if chatID != "222" {
		t.Errorf("expected chatID '222', got %q", chatID)
	}
}

// TestLastUsedChannel_SkipsCronAndSubagent skips non-channel sessions.
func TestLastUsedChannel_SkipsCronAndSubagent(t *testing.T) {
	m := NewManager("")
	ctx := context.Background()

	m.GetOrCreate(ctx, "agent:my-agent:cron:job-1")
	m.GetOrCreate(ctx, "agent:my-agent:subagent:worker")

	ch, chatID := m.LastUsedChannel(ctx, "my-agent")
	if ch != "" || chatID != "" {
		t.Errorf("expected ('', '') for only cron/subagent sessions, got (%q, %q)", ch, chatID)
	}
}

// TestLastUsedChannel_AgentIsolation ensures different agents don't cross-contaminate.
func TestLastUsedChannel_AgentIsolation(t *testing.T) {
	m := NewManager("")
	ctx := context.Background()

	m.GetOrCreate(ctx, "agent:agent-A:telegram:direct:100")
	m.GetOrCreate(ctx, "agent:agent-B:telegram:direct:200")

	ch, chatID := m.LastUsedChannel(ctx, "agent-A")
	if ch != "telegram" || chatID != "100" {
		t.Errorf("agent-A: expected (telegram, 100), got (%q, %q)", ch, chatID)
	}
}

// TestSave_MissingSession_NoError returns nil for a key that doesn't exist.
func TestSave_MissingSession_NoError(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)
	ctx := context.Background()

	if err := m.Save(ctx, "agent:ghost:s1"); err != nil {
		t.Fatalf("expected nil error for missing session, got %v", err)
	}
}

// TestSave_NoStorage_NoError returns nil when storage path is "".
func TestSave_NoStorage_NoError(t *testing.T) {
	m := NewManager("")
	ctx := context.Background()
	key := "agent:a1:s1"

	m.GetOrCreate(ctx, key)
	if err := m.Save(ctx, key); err != nil {
		t.Fatalf("expected nil error with no storage, got %v", err)
	}
}

// TestDelete_NonExistentKey returns nil (idempotent).
func TestDelete_NonExistentKey(t *testing.T) {
	m := NewManager("")
	if err := m.Delete(context.Background(), "nonexistent"); err != nil {
		t.Fatalf("expected nil error deleting non-existent session, got %v", err)
	}
}

// TestDelete_WithStorage_NonExistentFile returns nil when the file was never saved.
func TestDelete_WithStorage_NonExistentFile(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)
	ctx := context.Background()

	m.GetOrCreate(ctx, "agent:a1:s1")
	// Delete without prior Save — file does not exist on disk.
	if err := m.Delete(ctx, "agent:a1:s1"); err != nil {
		t.Fatalf("expected nil for non-existent file, got %v", err)
	}
}

// TestNewManager_LoadAll_SkipsBadFiles verifies corrupt JSON files are silently skipped.
func TestNewManager_LoadAll_SkipsBadFiles(t *testing.T) {
	dir := t.TempDir()

	// Write a corrupt JSON file directly.
	if err := os.WriteFile(dir+"/corrupt.json", []byte("not valid json"), 0644); err != nil {
		t.Fatalf("setup: write corrupt file: %v", err)
	}

	// NewManager must not crash; corrupt file is skipped.
	m := NewManager(dir)
	all := m.List(context.Background(), "")
	if len(all) != 0 {
		t.Errorf("expected 0 sessions (corrupt file skipped), got %d", len(all))
	}
}

// TestNewManager_LoadAll_SkipsNonJSON verifies non-JSON files are ignored.
func TestNewManager_LoadAll_SkipsNonJSON(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(dir+"/readme.txt", []byte("ignore me"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	m := NewManager(dir)
	if len(m.List(context.Background(), "")) != 0 {
		t.Error("expected 0 sessions (non-JSON skipped)")
	}
}
