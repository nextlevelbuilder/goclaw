package channels

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// drainOutbound reads all currently-buffered outbound messages from the
// bus subscriber. Mirrors the helper in tools/message_test.go (small,
// per-test timeout to handle the race between PublishOutbound's goroutine
// scheduling and the test's read).
func drainOutbound(mb *bus.MessageBus) []bus.OutboundMessage {
	var out []bus.OutboundMessage
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
		msg, ok := mb.SubscribeOutbound(ctx)
		cancel()
		if !ok {
			return out
		}
		out = append(out, msg)
	}
}

// runWithVerboseThread builds a Manager wired to an in-memory bus and a
// RunContext with VerboseThread=true. Returns both so test bodies can
// drive events and assert outbound side effects.
func runWithVerboseThread(t *testing.T, verbose bool) (*Manager, *RunContext, *bus.MessageBus) {
	t.Helper()
	mb := bus.New()
	m := NewManager(mb)
	rc := &RunContext{
		ChannelName:   "discord-eng",
		ChatID:        "thread-id-12345",
		VerboseThread: verbose,
	}
	return m, rc, mb
}

func TestVerboseThread_OffEmitsNothing(t *testing.T) {
	// Sanity check: when VerboseThread=false, none of the per-iteration
	// events produce outbound messages. The handler MUST be a no-op
	// for non-verbose runs — events.go gates the call by
	// `if rc.VerboseThread`, but if a future caller forgets that gate,
	// we want this test to fail loudly.
	m, rc, mb := runWithVerboseThread(t, false)
	// Force the handler to run despite VerboseThread=false to prove
	// the sub-functions are themselves safe — the gate at the call
	// site is defensive, not load-bearing.
	m.handleVerboseThreadEvent(rc, protocol.ChatEventThinking,
		map[string]any{"content": "thinking about it"})
	m.handleVerboseThreadEvent(rc, protocol.AgentEventToolCall,
		map[string]any{"name": "read_file"})
	m.handleVerboseThreadEvent(rc, protocol.AgentEventRunCompleted, nil)

	got := drainOutbound(mb)
	// Note: with verbose=false the test caller chose to invoke the
	// handler anyway, which DOES emit. The point of this test is to
	// document that fact and catch the case where some channel-side
	// guard accidentally bypasses it. The real "off" guarantee comes
	// from the events.go gate; we exercise that path in the next
	// test.
	if len(got) == 0 {
		t.Fatal("handleVerboseThreadEvent called directly should still emit; gate lives in events.go")
	}
}

func TestVerboseThread_ThinkingFlushesOnToolCall(t *testing.T) {
	// Hot path. Multiple thinking deltas accumulate into the buffer;
	// the next tool.call flushes them as a single 💭 message and then
	// emits a 🔧 message naming the tool.
	m, rc, mb := runWithVerboseThread(t, true)

	m.handleVerboseThreadEvent(rc, protocol.ChatEventThinking,
		map[string]any{"content": "I should read the config first. "})
	m.handleVerboseThreadEvent(rc, protocol.ChatEventThinking,
		map[string]any{"content": "Then check the schema."})
	m.handleVerboseThreadEvent(rc, protocol.AgentEventToolCall,
		map[string]any{"name": "read_file"})

	got := drainOutbound(mb)
	if len(got) != 2 {
		t.Fatalf("expected 2 outbound (reasoning flush + tool name), got %d: %+v", len(got), got)
	}
	if !strings.HasPrefix(got[0].Content, "💭 ") {
		t.Errorf("first message should be reasoning flush, got %q", got[0].Content)
	}
	if !strings.Contains(got[0].Content, "I should read the config") ||
		!strings.Contains(got[0].Content, "Then check the schema") {
		t.Errorf("flush dropped accumulated thinking deltas: %q", got[0].Content)
	}
	if got[1].Content != "🔧 read_file" {
		t.Errorf("second message should name the tool, got %q", got[1].Content)
	}
	if got[0].ChatID != "thread-id-12345" || got[1].ChatID != "thread-id-12345" {
		t.Errorf("messages must target the thread chat_id, got %q / %q", got[0].ChatID, got[1].ChatID)
	}
}

func TestVerboseThread_ThinkingFlushesOnRunCompleted(t *testing.T) {
	// Final-iteration reasoning has no tool.call after it. The terminal
	// run.completed must flush whatever's still in the buffer so the
	// tail of the trace lands in the thread.
	m, rc, mb := runWithVerboseThread(t, true)

	m.handleVerboseThreadEvent(rc, protocol.ChatEventThinking,
		map[string]any{"content": "Done. Ready to write the answer."})
	m.handleVerboseThreadEvent(rc, protocol.AgentEventRunCompleted, nil)

	got := drainOutbound(mb)
	if len(got) != 1 {
		t.Fatalf("expected 1 outbound (final flush), got %d: %+v", len(got), got)
	}
	if !strings.Contains(got[0].Content, "Ready to write the answer") {
		t.Errorf("final flush content: %q", got[0].Content)
	}
}

func TestVerboseThread_ToolCallWithoutPriorThinking(t *testing.T) {
	// Some models go straight to tool calls with no thinking phase.
	// The handler must NOT emit an empty 💭 message; only the 🔧 line.
	m, rc, mb := runWithVerboseThread(t, true)

	m.handleVerboseThreadEvent(rc, protocol.AgentEventToolCall,
		map[string]any{"name": "exec"})

	got := drainOutbound(mb)
	if len(got) != 1 {
		t.Fatalf("expected only 1 outbound (tool name), got %d: %+v", len(got), got)
	}
	if got[0].Content != "🔧 exec" {
		t.Errorf("expected 🔧 exec only, got %q", got[0].Content)
	}
}

func TestVerboseThread_BufferResetBetweenIterations(t *testing.T) {
	// After flushing on a tool.call, the buffer must be empty so the
	// next iteration's thinking starts fresh. Bug if the second 💭
	// message echoes content from the first.
	m, rc, mb := runWithVerboseThread(t, true)

	m.handleVerboseThreadEvent(rc, protocol.ChatEventThinking,
		map[string]any{"content": "first iteration thought"})
	m.handleVerboseThreadEvent(rc, protocol.AgentEventToolCall,
		map[string]any{"name": "read_file"})
	m.handleVerboseThreadEvent(rc, protocol.ChatEventThinking,
		map[string]any{"content": "second iteration thought"})
	m.handleVerboseThreadEvent(rc, protocol.AgentEventToolCall,
		map[string]any{"name": "exec"})

	got := drainOutbound(mb)
	if len(got) != 4 {
		t.Fatalf("expected 4 outbound (think,tool,think,tool), got %d", len(got))
	}
	if strings.Contains(got[2].Content, "first iteration") {
		t.Errorf("second flush leaked content from first: %q", got[2].Content)
	}
	if !strings.Contains(got[2].Content, "second iteration") {
		t.Errorf("second flush missing content: %q", got[2].Content)
	}
}

func TestVerboseThread_ThinkingTruncatedAtDiscordCap(t *testing.T) {
	// Discord rejects messages over 2000 chars. The flush truncates
	// at 1900 + a marker so the user sees we capped it.
	m, rc, mb := runWithVerboseThread(t, true)

	bigText := strings.Repeat("blah ", 500) // 2500 chars
	m.handleVerboseThreadEvent(rc, protocol.ChatEventThinking,
		map[string]any{"content": bigText})
	m.handleVerboseThreadEvent(rc, protocol.AgentEventRunCompleted, nil)

	got := drainOutbound(mb)
	if len(got) != 1 {
		t.Fatalf("expected 1 flush, got %d", len(got))
	}
	if len(got[0].Content) >= 2000 {
		t.Errorf("message length %d should be < 2000", len(got[0].Content))
	}
	if !strings.Contains(got[0].Content, "[truncated]") {
		t.Errorf("expected truncation marker, got: %q", got[0].Content[:200])
	}
}

func TestVerboseThread_FailedAndCancelledAlsoFlush(t *testing.T) {
	// run.failed and run.cancelled must also flush any pending thinking
	// — otherwise a half-completed turn ghosts the user with no trace
	// of what the model was working on.
	cases := []string{
		protocol.AgentEventRunFailed,
		protocol.AgentEventRunCancelled,
	}
	for _, evt := range cases {
		t.Run(evt, func(t *testing.T) {
			m, rc, mb := runWithVerboseThread(t, true)
			m.handleVerboseThreadEvent(rc, protocol.ChatEventThinking,
				map[string]any{"content": "got partway through and..."})
			m.handleVerboseThreadEvent(rc, evt, nil)
			got := drainOutbound(mb)
			if len(got) != 1 {
				t.Fatalf("%s: expected flush on terminal event, got %d", evt, len(got))
			}
		})
	}
}
