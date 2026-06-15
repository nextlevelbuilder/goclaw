package max

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// =====================================================================
// Helpers
// =====================================================================

// newReactionChannel returns a Channel + counter of /chats/.../actions calls.
func newReactionChannel(t *testing.T) (*Channel, *int64) {
	t.Helper()
	var counter int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Count POST /chats/<id>/actions calls.
		atomic.AddInt64(&counter, 1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	creds := instanceCreds{BotToken: "tok", BotID: 256747471, Username: "test"}
	cfg := instanceConfig{Mode: "polling", PollingTimeout: 30, DMPolicy: "open"}
	c, err := New("max-reactions-test", creds, cfg, bus.New(), nil, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.client = NewClient("tok", WithBaseURL(srv.URL), WithMaxRetries(1))
	// Provide a run context so refresher goroutines have something to bind to.
	c.runCtxMu.Lock()
	c.pollRunCtx = context.Background()
	c.runCtxMu.Unlock()
	return c, &counter
}

// =====================================================================
// reactionAction mapping
// =====================================================================

func TestReactionAction(t *testing.T) {
	tests := []struct {
		status, want string
	}{
		{"thinking", "typing_on"},
		{"tool_exec", "typing_on"},
		{"compacting", "typing_on"},
		{"stall", "typing_on"},
		{"done", ""},
		{"error", ""},
		{"unknown", ""},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			if got := reactionAction(tt.status); got != tt.want {
				t.Errorf("reactionAction(%q) = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

// =====================================================================
// OnReactionEvent — basic flow
// =====================================================================

func TestOnReactionEvent_TerminalStatus_NoOp(t *testing.T) {
	c, counter := newReactionChannel(t)

	// "done" is terminal — should NOT POST any action.
	err := c.OnReactionEvent(context.Background(), "188289857", "mid.x", "done")
	if err != nil {
		t.Fatalf("OnReactionEvent: %v", err)
	}
	// Give it a moment to make sure no goroutine fires anyway.
	time.Sleep(50 * time.Millisecond)
	if got := atomic.LoadInt64(counter); got != 0 {
		t.Errorf("expected 0 calls for terminal status, got %d", got)
	}
}

func TestOnReactionEvent_Thinking_PostsImmediately(t *testing.T) {
	c, counter := newReactionChannel(t)
	defer c.stopAllReactionRefreshers()

	err := c.OnReactionEvent(context.Background(), "188289857", "mid.x", "thinking")
	if err != nil {
		t.Fatalf("OnReactionEvent: %v", err)
	}

	// One immediate POST is expected; assert quickly.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if atomic.LoadInt64(counter) >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if atomic.LoadInt64(counter) < 1 {
		t.Errorf("expected at least 1 call, got %d", atomic.LoadInt64(counter))
	}
}

func TestOnReactionEvent_NonNumericChatID_NoOp(t *testing.T) {
	c, counter := newReactionChannel(t)
	defer c.stopAllReactionRefreshers()

	err := c.OnReactionEvent(context.Background(), "not-a-number", "mid.x", "thinking")
	if err != nil {
		t.Fatalf("OnReactionEvent: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if got := atomic.LoadInt64(counter); got != 0 {
		t.Errorf("expected 0 calls for non-numeric chatID, got %d", got)
	}
}

func TestOnReactionEvent_ZeroChatID_NoOp(t *testing.T) {
	c, counter := newReactionChannel(t)
	defer c.stopAllReactionRefreshers()

	err := c.OnReactionEvent(context.Background(), "0", "mid.x", "thinking")
	if err != nil {
		t.Fatalf("OnReactionEvent: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if got := atomic.LoadInt64(counter); got != 0 {
		t.Errorf("expected 0 calls for zero chatID, got %d", got)
	}
}

// =====================================================================
// ClearReaction — stops refresher
// =====================================================================

func TestClearReaction_StopsRefresher(t *testing.T) {
	c, _ := newReactionChannel(t)
	chatID := "188289857"

	// Spawn a refresher.
	if err := c.OnReactionEvent(context.Background(), chatID, "mid.x", "thinking"); err != nil {
		t.Fatalf("OnReactionEvent: %v", err)
	}

	// Verify it was registered.
	if _, ok := c.reactionRefreshers.Load(chatID); !ok {
		t.Fatal("expected a refresher to be registered")
	}

	// Clear it.
	if err := c.ClearReaction(context.Background(), chatID, "mid.x"); err != nil {
		t.Fatalf("ClearReaction: %v", err)
	}

	// Should be removed.
	if _, ok := c.reactionRefreshers.Load(chatID); ok {
		t.Error("refresher should be removed after ClearReaction")
	}
}

func TestClearReaction_NoActiveRefresher_NoOp(t *testing.T) {
	c, _ := newReactionChannel(t)
	// No refresher exists.
	err := c.ClearReaction(context.Background(), "999", "mid.x")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

// =====================================================================
// stopAllReactionRefreshers — Stop() integration
// =====================================================================

func TestStopAllReactionRefreshers_NoActive(t *testing.T) {
	c, _ := newReactionChannel(t)
	// No refreshers — should return immediately without panic.
	c.stopAllReactionRefreshers()
}

func TestStopAllReactionRefreshers_MultipleActive(t *testing.T) {
	c, _ := newReactionChannel(t)

	// Spawn refreshers for several chats.
	for _, chatID := range []string{"100", "200", "300"} {
		if err := c.OnReactionEvent(context.Background(), chatID, "mid.x", "thinking"); err != nil {
			t.Fatalf("OnReactionEvent: %v", err)
		}
	}

	// Stop all should clear them.
	c.stopAllReactionRefreshers()

	count := 0
	c.reactionRefreshers.Range(func(any, any) bool {
		count++
		return true
	})
	if count != 0 {
		t.Errorf("expected 0 refreshers after stopAll, got %d", count)
	}
}

// =====================================================================
// OnReactionEvent — replacing active refresher (status change)
// =====================================================================

func TestOnReactionEvent_StatusChange_ReplacesRefresher(t *testing.T) {
	c, _ := newReactionChannel(t)
	defer c.stopAllReactionRefreshers()

	chatID := "188289857"
	if err := c.OnReactionEvent(context.Background(), chatID, "mid.x", "thinking"); err != nil {
		t.Fatalf("OnReactionEvent: %v", err)
	}
	first, ok := c.reactionRefreshers.Load(chatID)
	if !ok {
		t.Fatal("first refresher should exist")
	}

	// Status change should replace, not stack.
	if err := c.OnReactionEvent(context.Background(), chatID, "mid.x", "tool_exec"); err != nil {
		t.Fatalf("OnReactionEvent: %v", err)
	}
	second, ok := c.reactionRefreshers.Load(chatID)
	if !ok {
		t.Fatal("second refresher should exist")
	}

	if first == second {
		t.Error("expected a new refresher instance after status change")
	}
}
