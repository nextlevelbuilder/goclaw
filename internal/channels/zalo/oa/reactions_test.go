package oa

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

// reactionTestServer is a counting http server that signals each request
// onto reqCh so tests can wait deterministically instead of fixed sleeps.
type reactionTestServer struct {
	srv     *httptest.Server
	reqCh   chan capturedRequest
	count   atomic.Int32
	mu      sync.Mutex
	bodies  []map[string]any
}

func newReactionCountingServer(t *testing.T) *reactionTestServer {
	t.Helper()
	rts := &reactionTestServer{reqCh: make(chan capturedRequest, 32)}
	rts.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rts.count.Add(1)
		req := capturedRequest{
			path:        r.URL.Path,
			contentType: r.Header.Get("Content-Type"),
			accessToken: r.Header.Get("access_token"),
			body:        body,
		}
		var parsed map[string]any
		_ = json.Unmarshal(body, &parsed)
		rts.mu.Lock()
		rts.bodies = append(rts.bodies, parsed)
		rts.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"message_id":"reaction-mid","user_id":"u"},"error":0,"message":"Success"}`))
		// Non-blocking signal so the server never deadlocks if the test
		// stops listening.
		select {
		case rts.reqCh <- req:
		default:
		}
	}))
	t.Cleanup(rts.srv.Close)
	return rts
}

func (rts *reactionTestServer) waitForRequest(t *testing.T, timeout time.Duration) capturedRequest {
	t.Helper()
	select {
	case r := <-rts.reqCh:
		return r
	case <-time.After(timeout):
		t.Fatalf("no request within %v", timeout)
		return capturedRequest{}
	}
}

func (rts *reactionTestServer) requireNoRequest(t *testing.T, window time.Duration) {
	t.Helper()
	select {
	case r := <-rts.reqCh:
		t.Fatalf("unexpected request within %v: %s", window, string(r.body))
	case <-time.After(window):
	}
}

func (rts *reactionTestServer) lastBody() map[string]any {
	rts.mu.Lock()
	defer rts.mu.Unlock()
	if len(rts.bodies) == 0 {
		return nil
	}
	return rts.bodies[len(rts.bodies)-1]
}

func newReactionChannel(t *testing.T, level string) (*Channel, *reactionTestServer) {
	t.Helper()
	rts := newReactionCountingServer(t)
	refresh, _ := newRefreshServer(t, "")
	c := newSendChannel(t, rts.srv, refresh, &fakeStore{})
	c.cfg.ReactionLevel = level
	c.cfg.ReactionTerminalDelayMinMs = 1
	c.cfg.ReactionTerminalDelayMaxMs = 1
	return c, rts
}

// --- emoji resolution ---

func TestResolveReactionEmoji_AllStatusesProduceIcon(t *testing.T) {
	t.Parallel()
	for status := range statusReactionVariants {
		icon := resolveReactionEmoji(status)
		if icon == "" {
			t.Errorf("status %q: empty icon", status)
		}
		if !zaloSupportedReactions[icon] {
			t.Errorf("status %q resolved to unsupported icon %q", status, icon)
		}
	}
}

func TestResolveReactionEmoji_FallbackOnUnsupported(t *testing.T) {
	// Mutates the package-global zaloSupportedReactions; can't run in parallel
	// with tests that resolve reactions.
	// Snapshot + restore the supported set so we can shrink it for one test.
	orig := make(map[string]bool, len(zaloSupportedReactions))
	for k, v := range zaloSupportedReactions {
		orig[k] = v
	}
	t.Cleanup(func() {
		zaloSupportedReactions = orig
	})

	// Drop the primary variant for "thinking" and confirm the resolver
	// advances to the fallback.
	primary := statusReactionVariants["thinking"][0]
	zaloSupportedReactions = map[string]bool{}
	for k, v := range orig {
		zaloSupportedReactions[k] = v
	}
	delete(zaloSupportedReactions, primary)

	icon := resolveReactionEmoji("thinking")
	if icon == primary {
		t.Errorf("expected fallback after dropping primary %q, got primary back", primary)
	}
	if icon == "" {
		t.Error("expected non-empty fallback icon")
	}
}

func TestResolveReactionEmoji_UnknownStatus(t *testing.T) {
	t.Parallel()
	if got := resolveReactionEmoji("not-a-status"); got != "" {
		t.Errorf("unknown status returned %q, want empty", got)
	}
}

// --- ReactionChannel guard contract ---

func TestChannelImplementsReactionChannel(t *testing.T) {
	t.Parallel()
	var _ channels.ReactionChannel = (*Channel)(nil)
}

// --- gate / level ---

func TestOnReactionEvent_OffShortCircuits(t *testing.T) {
	t.Parallel()
	for _, lvl := range []string{"", "off"} {
		c, rts := newReactionChannel(t, lvl)
		if err := c.OnReactionEvent(context.Background(), "user-1", "msg-1", "done"); err != nil {
			t.Fatalf("OnReactionEvent: %v", err)
		}
		rts.requireNoRequest(t, 250*time.Millisecond)
		if rts.count.Load() != 0 {
			t.Errorf("level=%q: %d requests, want 0", lvl, rts.count.Load())
		}
	}
}

func TestOnReactionEvent_MinimalSkipsIntermediate(t *testing.T) {
	t.Parallel()
	c, rts := newReactionChannel(t, "minimal")
	_ = c.OnReactionEvent(context.Background(), "u", "m", "thinking")
	_ = c.OnReactionEvent(context.Background(), "u", "m", "tool")
	rts.requireNoRequest(t, 250*time.Millisecond)
	if rts.count.Load() != 0 {
		t.Errorf("minimal mode: %d requests, want 0 for non-terminal", rts.count.Load())
	}
	_ = c.OnReactionEvent(context.Background(), "u", "m", "done")
	rts.waitForRequest(t, 500*time.Millisecond)
	if rts.count.Load() != 1 {
		t.Errorf("minimal mode: %d requests after done, want 1", rts.count.Load())
	}
}

func TestOnReactionEvent_EmptyIDsShortCircuit(t *testing.T) {
	t.Parallel()
	c, rts := newReactionChannel(t, "full")
	_ = c.OnReactionEvent(context.Background(), "", "msg", "done")
	_ = c.OnReactionEvent(context.Background(), "user", "", "done")
	rts.requireNoRequest(t, 200*time.Millisecond)
	if rts.count.Load() != 0 {
		t.Errorf("empty id: %d requests, want 0", rts.count.Load())
	}
}

// --- controller behavior ---

func TestController_TerminalDeferred(t *testing.T) {
	t.Parallel()
	c, rts := newReactionChannel(t, "full")
	_ = c.OnReactionEvent(context.Background(), "u", "m", "done")
	r := rts.waitForRequest(t, 500*time.Millisecond)
	if r.path != pathSendReaction {
		t.Errorf("path = %q", r.path)
	}
	body := rts.lastBody()
	sa, _ := body["sender_action"].(map[string]any)
	if sa["react_message_id"] != "m" {
		t.Errorf("react_message_id = %v", sa["react_message_id"])
	}
}

func TestController_TerminalRespectsDelay(t *testing.T) {
	t.Parallel()
	c, rts := newReactionChannel(t, "full")
	c.cfg.ReactionTerminalDelayMinMs = 250
	c.cfg.ReactionTerminalDelayMaxMs = 250
	start := time.Now()
	_ = c.OnReactionEvent(context.Background(), "u", "m", "done")
	rts.requireNoRequest(t, 150*time.Millisecond)
	rts.waitForRequest(t, 500*time.Millisecond)
	if elapsed := time.Since(start); elapsed < 200*time.Millisecond {
		t.Errorf("terminal fired in %v, want >= ~250ms", elapsed)
	}
}

func TestController_TerminalCancelledOnStop(t *testing.T) {
	t.Parallel()
	c, rts := newReactionChannel(t, "full")
	c.cfg.ReactionTerminalDelayMinMs = 500
	c.cfg.ReactionTerminalDelayMaxMs = 500
	_ = c.OnReactionEvent(context.Background(), "u", "m", "done")
	if err := c.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	rts.requireNoRequest(t, 800*time.Millisecond)
	if got := rts.count.Load(); got != 0 {
		t.Errorf("got %d requests after Stop, want 0", got)
	}
}

func TestController_DebouncesIntermediate(t *testing.T) {
	t.Parallel()
	c, rts := newReactionChannel(t, "full")
	for range 5 {
		_ = c.OnReactionEvent(context.Background(), "u", "m", "thinking")
	}
	// Within the 700ms debounce: no requests yet.
	rts.requireNoRequest(t, 200*time.Millisecond)
	// After debounce window: exactly one request.
	rts.waitForRequest(t, 1500*time.Millisecond)
	// Quiet window — confirm no further sends.
	rts.requireNoRequest(t, 400*time.Millisecond)
	if got := rts.count.Load(); got != 1 {
		t.Errorf("debounce: total requests = %d, want 1", got)
	}
}

func TestController_TerminalCancelsDebounce(t *testing.T) {
	t.Parallel()
	c, rts := newReactionChannel(t, "full")
	_ = c.OnReactionEvent(context.Background(), "u", "m", "thinking")
	_ = c.OnReactionEvent(context.Background(), "u", "m", "done")
	rts.waitForRequest(t, 250*time.Millisecond)
	// Past the debounce window — confirm the debounced thinking didn't fire.
	rts.requireNoRequest(t, 1*time.Second)
	if got := rts.count.Load(); got != 1 {
		t.Errorf("got %d requests, want 1 (terminal must cancel debounce)", got)
	}
}

func TestController_StopCancelsTimer(t *testing.T) {
	t.Parallel()
	c, rts := newReactionChannel(t, "full")
	_ = c.OnReactionEvent(context.Background(), "u", "m", "thinking")
	if err := c.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	// Past the 700ms debounce — Stop must have cancelled the timer.
	rts.requireNoRequest(t, 1*time.Second)
	if got := rts.count.Load(); got != 0 {
		t.Errorf("got %d requests after Stop, want 0", got)
	}
}

// TestController_UnmappedIntermediateNoOp: tool/coding/web are deliberately
// not mapped on Zalo OA (B2C noise control). They must not produce wire
// traffic, even on the debounced path.
func TestController_UnmappedIntermediateNoOp(t *testing.T) {
	t.Parallel()
	c, rts := newReactionChannel(t, "full")
	for _, st := range []string{"tool", "coding", "web"} {
		_ = c.OnReactionEvent(context.Background(), "u", "m", st)
	}
	rts.requireNoRequest(t, 1*time.Second)
	if got := rts.count.Load(); got != 0 {
		t.Errorf("got %d requests, want 0 for unmapped statuses", got)
	}
}

func TestClearReaction_SendsRemoveSentinel(t *testing.T) {
	t.Parallel()
	c, rts := newReactionChannel(t, "full")
	if err := c.ClearReaction(context.Background(), "u", "m"); err != nil {
		t.Fatalf("ClearReaction: %v", err)
	}
	rts.waitForRequest(t, 500*time.Millisecond)
	body := rts.lastBody()
	sa, _ := body["sender_action"].(map[string]any)
	if sa["react_icon"] != "/-remove" {
		t.Errorf("react_icon = %v, want /-remove", sa["react_icon"])
	}
	if sa["react_message_id"] != "m" {
		t.Errorf("react_message_id = %v", sa["react_message_id"])
	}
}

func TestClearReaction_StopsExistingController(t *testing.T) {
	t.Parallel()
	c, rts := newReactionChannel(t, "full")
	_ = c.OnReactionEvent(context.Background(), "u", "m", "thinking")
	// Clear before debounce fires; debounced reaction must NOT be sent.
	if err := c.ClearReaction(context.Background(), "u", "m"); err != nil {
		t.Fatalf("ClearReaction: %v", err)
	}
	// Drain the /-remove send.
	rts.waitForRequest(t, 500*time.Millisecond)
	// Past the debounce: nothing else.
	rts.requireNoRequest(t, 1*time.Second)
	if got := rts.count.Load(); got != 1 {
		t.Errorf("got %d requests, want 1 (only the /-remove)", got)
	}
}
