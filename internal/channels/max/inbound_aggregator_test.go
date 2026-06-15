package max

import (
	"context"
	"sync"
	"testing"
	"time"
)

// mkMsg constructs a minimal Message suitable for aggregator tests.
// senderID and chatID drive the per-buffer key; text and attachments
// drive the merge logic.
func mkMsg(senderID, chatID int64, text string, attachmentTypes ...string) Message {
	atts := make([]Attachment, 0, len(attachmentTypes))
	for _, t := range attachmentTypes {
		atts = append(atts, Attachment{Type: t})
	}
	return Message{
		Sender: &User{UserID: senderID},
		Recipient: &Recipient{
			ChatType: "dialog",
			ChatID:   chatID,
		},
		Body: &MessageBody{
			Text:        text,
			Attachments: atts,
		},
	}
}

// collectFlush returns a flushFn that appends each flushed message to a
// slice protected by a Mutex (concurrency-safe), and a getter for the
// slice contents. Tests use this to assert what flushFn observed.
func collectFlush() (func(context.Context, Message, bool), func() []Message) {
	var mu sync.Mutex
	var got []Message
	fn := func(_ context.Context, m Message, _ bool) {
		mu.Lock()
		got = append(got, m)
		mu.Unlock()
	}
	getter := func() []Message {
		mu.Lock()
		defer mu.Unlock()
		out := make([]Message, len(got))
		copy(out, got)
		return out
	}
	return fn, getter
}

// TestAggregator_SingleMessage_FlushedAfterWindow verifies that a lone
// Push results in exactly one flush after the silence window.
func TestAggregator_SingleMessage_FlushedAfterWindow(t *testing.T) {
	flushFn, getMsgs := collectFlush()
	a := newInboundAggregator(50*time.Millisecond, 100, 1000, flushFn)

	ok := a.Push(context.Background(), mkMsg(100, 200, "hello"), false)
	if !ok {
		t.Fatal("Push returned false; expected true")
	}

	// Before window elapses, nothing flushed yet.
	if n := len(getMsgs()); n != 0 {
		t.Errorf("got %d flushes before window; expected 0", n)
	}

	time.Sleep(100 * time.Millisecond)

	msgs := getMsgs()
	if len(msgs) != 1 {
		t.Fatalf("got %d flushes after window; expected 1", len(msgs))
	}
	if msgs[0].Body.Text != "hello" {
		t.Errorf("flushed text = %q; want %q", msgs[0].Body.Text, "hello")
	}
}

// TestAggregator_TextThenFile_Coalesced verifies the primary bug scenario:
// text and file arrive within the window, and ONE merged message is
// dispatched (not two separate ones).
func TestAggregator_TextThenFile_Coalesced(t *testing.T) {
	flushFn, getMsgs := collectFlush()
	a := newInboundAggregator(80*time.Millisecond, 100, 1000, flushFn)

	a.Push(context.Background(), mkMsg(100, 200, "please review this"), false)
	time.Sleep(20 * time.Millisecond)
	a.Push(context.Background(), mkMsg(100, 200, "", "file"), false)

	// Within window: nothing yet.
	if n := len(getMsgs()); n != 0 {
		t.Errorf("flushed prematurely: %d", n)
	}

	time.Sleep(150 * time.Millisecond)

	msgs := getMsgs()
	if len(msgs) != 1 {
		t.Fatalf("got %d flushes; expected 1 (merged)", len(msgs))
	}
	m := msgs[0]
	if m.Body.Text != "please review this" {
		t.Errorf("merged text = %q; want \"please review this\"", m.Body.Text)
	}
	if len(m.Body.Attachments) != 1 || m.Body.Attachments[0].Type != "file" {
		t.Errorf("merged attachments = %+v; want 1 file", m.Body.Attachments)
	}
}

// TestAggregator_ThreeRapid_AllMerged verifies that 3+ rapid pushes
// (within the rolling window) collapse to one merged message.
func TestAggregator_ThreeRapid_AllMerged(t *testing.T) {
	flushFn, getMsgs := collectFlush()
	a := newInboundAggregator(80*time.Millisecond, 100, 1000, flushFn)

	a.Push(context.Background(), mkMsg(100, 200, "part1"), false)
	time.Sleep(15 * time.Millisecond)
	a.Push(context.Background(), mkMsg(100, 200, "part2", "image"), false)
	time.Sleep(15 * time.Millisecond)
	a.Push(context.Background(), mkMsg(100, 200, "part3", "file"), false)

	time.Sleep(200 * time.Millisecond)

	msgs := getMsgs()
	if len(msgs) != 1 {
		t.Fatalf("got %d flushes; expected 1", len(msgs))
	}
	if msgs[0].Body.Text != "part1\npart2\npart3" {
		t.Errorf("merged text = %q", msgs[0].Body.Text)
	}
	if len(msgs[0].Body.Attachments) != 2 {
		t.Errorf("merged attachments = %d; want 2", len(msgs[0].Body.Attachments))
	}
}

// TestAggregator_DifferentSenders_Independent verifies the per-key buffer
// isolation: two senders in the same chat don't get merged.
func TestAggregator_DifferentSenders_Independent(t *testing.T) {
	flushFn, getMsgs := collectFlush()
	a := newInboundAggregator(50*time.Millisecond, 100, 1000, flushFn)

	a.Push(context.Background(), mkMsg(100, 200, "from sender A"), false)
	a.Push(context.Background(), mkMsg(101, 200, "from sender B"), false)

	time.Sleep(150 * time.Millisecond)

	msgs := getMsgs()
	if len(msgs) != 2 {
		t.Fatalf("got %d flushes; expected 2 (independent senders)", len(msgs))
	}
	// Order may vary; just confirm we got both
	texts := map[string]bool{msgs[0].Body.Text: true, msgs[1].Body.Text: true}
	if !texts["from sender A"] || !texts["from sender B"] {
		t.Errorf("missing one of the messages; got: %v", texts)
	}
}

// TestAggregator_DifferentChats_Independent: same sender in different
// chats. Each chat has its own buffer.
func TestAggregator_DifferentChats_Independent(t *testing.T) {
	flushFn, getMsgs := collectFlush()
	a := newInboundAggregator(50*time.Millisecond, 100, 1000, flushFn)

	a.Push(context.Background(), mkMsg(100, 200, "in chat 200"), false)
	a.Push(context.Background(), mkMsg(100, 300, "in chat 300"), false)

	time.Sleep(150 * time.Millisecond)

	if n := len(getMsgs()); n != 2 {
		t.Fatalf("got %d flushes; expected 2 (independent chats)", n)
	}
}

// TestAggregator_PerBufferOverflow_RejectsAfterCap: pushing more than
// maxPerBuf items into a single buffer returns false on overflow.
func TestAggregator_PerBufferOverflow_RejectsAfterCap(t *testing.T) {
	flushFn, _ := collectFlush()
	a := newInboundAggregator(1*time.Second, 3, 1000, flushFn) // cap=3

	// First 3 pushes accepted
	for i := 0; i < 3; i++ {
		if !a.Push(context.Background(), mkMsg(100, 200, "msg"), false) {
			t.Fatalf("Push %d rejected unexpectedly", i)
		}
	}
	// 4th push should be rejected (overflow)
	if a.Push(context.Background(), mkMsg(100, 200, "msg"), false) {
		t.Error("Push past cap returned true; expected false (overflow)")
	}
}

// TestAggregator_GlobalOverflow_RejectsAfterCap: pushing across more
// distinct keys than maxBuffers returns false on global overflow.
func TestAggregator_GlobalOverflow_RejectsAfterCap(t *testing.T) {
	flushFn, _ := collectFlush()
	a := newInboundAggregator(1*time.Second, 100, 2, flushFn) // global cap=2

	// First 2 distinct buffers (chat 1, chat 2) accepted
	if !a.Push(context.Background(), mkMsg(100, 1, "a"), false) {
		t.Fatal("first key rejected")
	}
	if !a.Push(context.Background(), mkMsg(100, 2, "b"), false) {
		t.Fatal("second key rejected")
	}
	// 3rd distinct key — global overflow
	if a.Push(context.Background(), mkMsg(100, 3, "c"), false) {
		t.Error("third distinct key accepted; expected reject")
	}
}

// TestAggregator_Stop_FlushesPending verifies that Stop() synchronously
// drains any pending buffers before returning.
func TestAggregator_Stop_FlushesPending(t *testing.T) {
	flushFn, getMsgs := collectFlush()
	a := newInboundAggregator(5*time.Second, 100, 1000, flushFn) // long window

	a.Push(context.Background(), mkMsg(100, 200, "hello"), false)
	a.Push(context.Background(), mkMsg(101, 200, "world"), false)

	// No flushes yet (window is 5s, we're not waiting)
	if n := len(getMsgs()); n != 0 {
		t.Errorf("flushed before Stop: %d", n)
	}

	a.Stop()

	// Stop drains synchronously — flushes should be visible immediately.
	msgs := getMsgs()
	if len(msgs) != 2 {
		t.Fatalf("after Stop got %d flushes; expected 2", len(msgs))
	}
}

// TestAggregator_PushAfterStop_Rejected: once Stop is called, subsequent
// Push calls return false and do not enqueue.
func TestAggregator_PushAfterStop_Rejected(t *testing.T) {
	flushFn, getMsgs := collectFlush()
	a := newInboundAggregator(50*time.Millisecond, 100, 1000, flushFn)

	a.Stop()

	ok := a.Push(context.Background(), mkMsg(100, 200, "after stop"), false)
	if ok {
		t.Error("Push after Stop returned true; expected false")
	}

	time.Sleep(100 * time.Millisecond)
	if n := len(getMsgs()); n != 0 {
		t.Errorf("got %d flushes after Stop+Push; expected 0", n)
	}
}

// TestAggregator_NilFields_Rejected: messages missing Sender or Recipient
// are rejected (caller should fall through to direct dispatch).
func TestAggregator_NilFields_Rejected(t *testing.T) {
	flushFn, _ := collectFlush()
	a := newInboundAggregator(50*time.Millisecond, 100, 1000, flushFn)

	cases := []struct {
		name string
		msg  Message
	}{
		{"nil sender", Message{Recipient: &Recipient{ChatID: 1}, Body: &MessageBody{}}},
		{"nil recipient", Message{Sender: &User{UserID: 1}, Body: &MessageBody{}}},
		{"both nil", Message{Body: &MessageBody{}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if a.Push(context.Background(), tc.msg, false) {
				t.Errorf("Push with %s accepted; expected reject", tc.name)
			}
		})
	}
}

// TestAggregator_TimerExtendedOnPush: each Push within the window should
// reset the silence timer — verify the FINAL push timestamp drives the
// flush, not the first.
func TestAggregator_TimerExtendedOnPush(t *testing.T) {
	flushFn, getMsgs := collectFlush()
	a := newInboundAggregator(80*time.Millisecond, 100, 1000, flushFn)

	// Push every 40ms (well under 80ms window) for 240ms total = 6 pushes
	for i := 0; i < 6; i++ {
		a.Push(context.Background(), mkMsg(100, 200, "tick"), false)
		time.Sleep(40 * time.Millisecond)
	}

	// At this point T=240ms. The timer was reset on each Push, so last
	// Push at T=200ms — the flush should fire around T=280ms.
	// We just checked T=240ms; no flush yet.
	if n := len(getMsgs()); n != 0 {
		t.Errorf("flushed before timer extended-out: %d", n)
	}

	time.Sleep(150 * time.Millisecond) // wait past T=80ms after last Push

	msgs := getMsgs()
	if len(msgs) != 1 {
		t.Fatalf("got %d flushes; expected 1 (single merged)", len(msgs))
	}
	// All 6 "tick" merged into "tick\ntick\ntick\ntick\ntick\ntick"
	if msgs[0].Body.Text != "tick\ntick\ntick\ntick\ntick\ntick" {
		t.Errorf("merged text = %q", msgs[0].Body.Text)
	}
}

// TestAggregator_StopIdempotent verifies that calling Stop twice is safe.
func TestAggregator_StopIdempotent(t *testing.T) {
	flushFn, _ := collectFlush()
	a := newInboundAggregator(50*time.Millisecond, 100, 1000, flushFn)

	a.Push(context.Background(), mkMsg(100, 200, "x"), false)
	a.Stop()
	a.Stop() // must not panic or block
}

// TestAggregator_EmptyTextAndAttachmentsMerge: merging messages where
// some have only text and others have only attachments should produce
// a merge with both fields populated.
func TestAggregator_EmptyTextAndAttachmentsMerge(t *testing.T) {
	flushFn, getMsgs := collectFlush()
	a := newInboundAggregator(60*time.Millisecond, 100, 1000, flushFn)

	a.Push(context.Background(), mkMsg(100, 200, "describe this", /*no attachments*/), false)
	a.Push(context.Background(), mkMsg(100, 200, "" /*no text*/, "image"), false)
	a.Push(context.Background(), mkMsg(100, 200, "" /*no text*/, "file"), false)

	time.Sleep(150 * time.Millisecond)

	msgs := getMsgs()
	if len(msgs) != 1 {
		t.Fatalf("got %d flushes; expected 1", len(msgs))
	}
	m := msgs[0]
	if m.Body.Text != "describe this" {
		t.Errorf("merged text = %q; want \"describe this\" (empty-only msgs skipped)", m.Body.Text)
	}
	if len(m.Body.Attachments) != 2 {
		t.Errorf("merged attachments = %d; want 2", len(m.Body.Attachments))
	}
}
