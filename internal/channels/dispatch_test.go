package channels

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// Reproduces the false-success bug: a cross-target forward (message tool,
// forward=true) whose destination chat ID is invalid must notify the ORIGIN
// chat with the real failure — not retry against the same broken
// destination, and not silently drop a text-only failure.
func TestHandleSendFailure_ForwardNotifiesOrigin(t *testing.T) {
	t.Parallel()

	mgr := NewManager(bus.New())
	origin := newMockChannel("bunny-zalo-personal", TypeZaloPersonal)
	mgr.channels["bunny-zalo-personal"] = origin

	badTarget := bus.OutboundMessage{
		Channel: "bunny-zalo-personal",
		ChatID:  "Ban Điều Hành", // display name passed as chat ID — invalid
		Content: "Anh Tài ơi, xem giúp comment khách hàng nhé.",
		Metadata: map[string]string{
			bus.MetaForwardOriginChannel: "bunny-zalo-personal",
			bus.MetaForwardOriginChatID:  "747300108647389888",
		},
	}

	mgr.handleSendFailure(context.Background(), origin, badTarget, errors.New("inner error code 114: Tham số không hợp lệ"))

	if origin.lastMsg.ChatID != "747300108647389888" {
		t.Fatalf("notice went to %q, want the origin chat, not the broken destination %q", origin.lastMsg.ChatID, badTarget.ChatID)
	}
	if origin.lastMsg.Content == "" {
		t.Fatal("expected a non-empty failure notice content")
	}
}

func TestDispatchOutboundAcknowledgesRealSendAndDeduplicatesWorkflow(t *testing.T) {
	mb := bus.New()
	mgr := NewManager(mb)
	channel := newMockChannel("telegram-main", TypeTelegram)
	mgr.channels["telegram-main"] = channel
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go mgr.dispatchOutbound(ctx)

	ack := make(chan error, 2)
	message := bus.OutboundMessage{
		Channel: "telegram-main", ChatID: "chat-1", Content: "result",
		Metadata:    map[string]string{"workflow_delivery_id": "workflow-1"},
		DeliveryAck: func(err error) { ack <- err },
	}
	mb.PublishOutbound(message)
	select {
	case err := <-ack:
		if err != nil {
			t.Fatalf("delivery ack error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for delivery ack")
	}
	mb.PublishOutbound(message)
	select {
	case err := <-ack:
		if err != nil {
			t.Fatalf("duplicate ack error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for duplicate ack")
	}
	if channel.sendCount != 1 {
		t.Fatalf("channel send count=%d, want one", channel.sendCount)
	}
}

func TestDispatchOutboundReportsSendFailureToWorkflowAck(t *testing.T) {
	mb := bus.New()
	mgr := NewManager(mb)
	channel := newMockChannel("telegram-main", TypeTelegram)
	channel.sendErr = errors.New("send failed")
	mgr.channels["telegram-main"] = channel
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go mgr.dispatchOutbound(ctx)

	ack := make(chan error, 1)
	mb.PublishOutbound(bus.OutboundMessage{
		Channel: "telegram-main", ChatID: "chat-1", Content: "result",
		Metadata:    map[string]string{"workflow_delivery_id": "workflow-failed"},
		DeliveryAck: func(err error) { ack <- err },
	})
	select {
	case err := <-ack:
		if err == nil {
			t.Fatal("failed channel send must return an ack error")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for failed delivery ack")
	}
}

func TestWorkflowDeliveryDedupePrunesExpiredMarkers(t *testing.T) {
	mgr := NewManager(bus.New())
	now := time.Now()
	if duplicate := mgr.markWorkflowDelivery("workflow-old", now); duplicate {
		t.Fatal("first delivery must not be treated as duplicate")
	}
	if duplicate := mgr.markWorkflowDelivery("workflow-old", now.Add(time.Minute)); !duplicate {
		t.Fatal("marker must remain active inside the dedupe TTL")
	}
	if duplicate := mgr.markWorkflowDelivery("workflow-new", now.Add(workflowDeliveryDedupeTTL+time.Second)); duplicate {
		t.Fatal("new delivery must not be treated as duplicate")
	}
	if _, exists := mgr.workflowDelivery.Load("workflow-old"); exists {
		t.Fatal("expired workflow marker was not pruned")
	}
}

func TestWorkflowDeliveryDedupeIsBounded(t *testing.T) {
	mgr := NewManager(bus.New())
	now := time.Now()
	for i := 0; i < workflowDeliveryDedupeMaxEntries; i++ {
		mgr.workflowDelivery.Store(i, now.Add(time.Duration(i+1)*time.Second))
	}
	if duplicate := mgr.markWorkflowDelivery("workflow-new", now); duplicate {
		t.Fatal("new delivery must not be treated as duplicate")
	}
	count := 0
	mgr.workflowDelivery.Range(func(_, _ any) bool {
		count++
		return true
	})
	if count > workflowDeliveryDedupeMaxEntries {
		t.Fatalf("dedupe entries=%d, max=%d", count, workflowDeliveryDedupeMaxEntries)
	}
}

// Non-forward media failures keep the pre-existing behavior: retry-notify
// the SAME chat (no forward metadata present, so there's no separate origin).
func TestHandleSendFailure_NonForwardMediaNotifiesSameChat(t *testing.T) {
	t.Parallel()

	mgr := NewManager(bus.New())
	ch := newMockChannel("telegram-main", TypeTelegram)
	mgr.channels["telegram-main"] = ch

	msg := bus.OutboundMessage{
		Channel: "telegram-main",
		ChatID:  "chat-1",
		Media:   []bus.MediaAttachment{{URL: "/tmp/x.png"}},
	}

	mgr.handleSendFailure(context.Background(), ch, msg, errors.New("file is too big"))

	if ch.lastMsg.ChatID != "chat-1" {
		t.Fatalf("notice ChatID = %q, want chat-1 (same chat)", ch.lastMsg.ChatID)
	}
}

// Non-forward TEXT-ONLY failures are still dropped (pre-existing behavior,
// unrelated to the forward bug this file otherwise fixes) — no channel
// exists to receive a notice, so Send must not be called again.
func TestHandleSendFailure_NonForwardTextOnlyDropped(t *testing.T) {
	t.Parallel()

	mgr := NewManager(bus.New())
	ch := newMockChannel("telegram-main", TypeTelegram)
	mgr.channels["telegram-main"] = ch

	msg := bus.OutboundMessage{
		Channel: "telegram-main",
		ChatID:  "chat-1",
		Content: "hello",
	}

	mgr.handleSendFailure(context.Background(), ch, msg, errors.New("chat not found"))

	if ch.lastMsg.Content != "" {
		t.Fatalf("expected no notice sent for non-forward text-only failure, got: %+v", ch.lastMsg)
	}
}

// countingChannel is a thread-safe Channel for concurrency tests: mockChannel's
// Send is not safe for concurrent use.
type countingChannel struct {
	BaseChannel

	mu        sync.Mutex
	sends     int
	sendErr   error
	failFirst bool
}

func (c *countingChannel) Type() string                  { return TypeTelegram }
func (c *countingChannel) Start(_ context.Context) error { return nil }
func (c *countingChannel) Stop(_ context.Context) error  { return nil }
func (c *countingChannel) IsRunning() bool               { return true }
func (c *countingChannel) IsAllowed(_ string) bool       { return true }

func (c *countingChannel) Send(_ context.Context, _ bus.OutboundMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sends++
	if c.sendErr != nil {
		return c.sendErr
	}
	if c.failFirst && c.sends == 1 {
		return errors.New("transient send failure")
	}
	return nil
}

func (c *countingChannel) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sends
}

// Exactly-once under a thundering herd: N dispatches of the SAME
// workflow_delivery_id arriving at once must produce exactly ONE channel Send,
// and every duplicate must still be acked (nil) so the workflow recovery loop
// never waits on a lease for an outcome dispatch already decided.
func TestDeliverOutbound_ConcurrentDuplicatesDeliverOnce(t *testing.T) {
	t.Parallel()

	mgr := NewManager(bus.New())
	ch := &countingChannel{}
	ch.BaseChannel = BaseChannel{name: "telegram-main"}
	mgr.channels["telegram-main"] = ch

	const racers = 32
	var wg sync.WaitGroup
	acks := make(chan error, racers)
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mgr.deliverOutbound(context.Background(), bus.OutboundMessage{
				Channel:     "telegram-main",
				ChatID:      "chat-1",
				Content:     "result",
				Metadata:    map[string]string{"workflow_delivery_id": "workflow-herd"},
				DeliveryAck: func(err error) { acks <- err },
			})
		}()
	}
	wg.Wait()
	close(acks)

	if got := ch.count(); got != 1 {
		t.Fatalf("channel sends=%d, want exactly 1 (exactly-once under concurrency)", got)
	}
	var nilAcks, errAcks int
	for err := range acks {
		if err == nil {
			nilAcks++
		} else {
			errAcks++
		}
	}
	if errAcks != 0 {
		t.Fatalf("%d duplicates acked with an error; duplicates must ack nil", errAcks)
	}
	if nilAcks != racers {
		t.Fatalf("nil acks=%d, want %d (every duplicate must be acked)", nilAcks, racers)
	}
}

// A failed send must NOT burn the dedupe marker: the workflow recovery loop
// retries the same delivery id, and that retry has to actually reach the
// channel. If failDelivery forgot to release the marker, the retry would be
// swallowed as a duplicate and the workflow result would never be delivered.
func TestDeliverOutbound_FailedSendReleasesMarkerForRetry(t *testing.T) {
	t.Parallel()

	mgr := NewManager(bus.New())
	ch := &countingChannel{failFirst: true}
	ch.BaseChannel = BaseChannel{name: "telegram-main"}
	mgr.channels["telegram-main"] = ch

	deliver := func() error {
		got := make(chan error, 1)
		mgr.deliverOutbound(context.Background(), bus.OutboundMessage{
			Channel:     "telegram-main",
			ChatID:      "chat-1",
			Content:     "result",
			Metadata:    map[string]string{"workflow_delivery_id": "workflow-retry"},
			DeliveryAck: func(err error) { got <- err },
		})
		return <-got
	}

	if err := deliver(); err == nil {
		t.Fatal("first delivery should fail (transient send error)")
	}
	if err := deliver(); err != nil {
		t.Fatalf("retry after failed send must deliver, got ack error: %v", err)
	}
	if got := ch.count(); got != 2 {
		t.Fatalf("channel sends=%d, want 2 (failed first attempt + successful retry)", got)
	}
}

// A successful delivery IS terminal: a second dispatch of the same id is a
// duplicate and must be acked nil WITHOUT touching the channel again — the
// exactly-once guarantee for the happy path, including after a shard hand-off.
func TestDeliverOutbound_SuccessfulDeliveryIsExactlyOnce(t *testing.T) {
	t.Parallel()

	mgr := NewManager(bus.New())
	ch := &countingChannel{}
	ch.BaseChannel = BaseChannel{name: "telegram-main"}
	mgr.channels["telegram-main"] = ch

	for i := 0; i < 3; i++ {
		got := make(chan error, 1)
		mgr.deliverOutbound(context.Background(), bus.OutboundMessage{
			Channel:     "telegram-main",
			ChatID:      "chat-1",
			Content:     "result",
			Metadata:    map[string]string{"workflow_delivery_id": "workflow-once"},
			DeliveryAck: func(err error) { got <- err },
		})
		if err := <-got; err != nil {
			t.Fatalf("dispatch %d acked error: %v", i, err)
		}
	}
	if got := ch.count(); got != 1 {
		t.Fatalf("channel sends=%d, want exactly 1 across repeated successful deliveries", got)
	}
}
