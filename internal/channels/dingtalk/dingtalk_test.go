package dingtalk

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/open-dingtalk/dingtalk-stream-sdk-go/chatbot"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

// fakeTransport stands in for the Stream-mode socket. It records lifecycle
// calls and lets a test hand a payload straight to the channel's inbound
// handler, which is how the whole inbound pipeline is exercised without a
// network connection.
type fakeTransport struct {
	startErr   error
	startDelay time.Duration
	started    atomic.Int32
	closed     atomic.Int32
	handler    chatbot.IChatBotMessageHandler
}

func (f *fakeTransport) Start(context.Context) error {
	f.started.Add(1)
	if f.startDelay > 0 {
		time.Sleep(f.startDelay)
	}
	return f.startErr
}

func (f *fakeTransport) Close() { f.closed.Add(1) }

// deliver drives an inbound message through the channel exactly as the SDK would.
func (f *fakeTransport) deliver(ctx context.Context, data *chatbot.BotCallbackDataModel) ([]byte, error) {
	return f.handler(ctx, data)
}

// newTestChannel builds a channel wired to a fakeTransport.
func newTestChannel(t *testing.T) (*Channel, *fakeTransport) {
	t.Helper()
	return newTestChannelCfg(t, Config{ClientID: "k", ClientSecret: "s"})
}

func newTestChannelCfg(t *testing.T, cfg Config) (*Channel, *fakeTransport) {
	t.Helper()
	ch, err := New(cfg, bus.New(), nil, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ft := &fakeTransport{}
	ch.newTransport = func(h chatbot.IChatBotMessageHandler) streamTransport {
		ft.handler = h
		return ft
	}
	return ch, ft
}

func TestNew_ValidatesConfig(t *testing.T) {
	if _, err := New(Config{}, bus.New(), nil, nil, nil); err == nil {
		t.Fatal("want error on missing credentials")
	}
	if _, err := New(Config{ClientID: "k", ClientSecret: "s", GroupReplyMode: "bogus"},
		bus.New(), nil, nil, nil); err == nil {
		t.Fatal("want error on bad group_reply_mode")
	}
}

// New must seed BaseChannel.requireMention from the config, or the shared
// mention gate reads false while the config says true.
func TestNew_SeedsRequireMention(t *testing.T) {
	ch, _ := newTestChannel(t)
	if !ch.RequireMention() {
		t.Error("BaseChannel.RequireMention() = false, want true (config default)")
	}

	no := false
	off, err := New(Config{ClientID: "k", ClientSecret: "s", RequireMention: &no},
		bus.New(), nil, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if off.RequireMention() {
		t.Error("BaseChannel.RequireMention() = true, want false (explicit config)")
	}
}

func TestStartStop_Lifecycle(t *testing.T) {
	ch, ft := newTestChannel(t)
	ctx := context.Background()

	if ch.IsRunning() {
		t.Fatal("running before Start")
	}
	if err := ch.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !ch.IsRunning() {
		t.Fatal("not running after Start")
	}
	if got := ft.started.Load(); got != 1 {
		t.Errorf("transport.Start called %d times, want 1", got)
	}
	if err := ch.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if ch.IsRunning() {
		t.Fatal("still running after Stop")
	}
	if got := ft.closed.Load(); got != 1 {
		t.Errorf("transport.Close called %d times, want 1", got)
	}
}

// A dial failure must surface, not be swallowed: the manager records it as a
// start failure and the operator sees a broken instance on the dashboard.
// The channel must not report running, and must not leave the history flusher
// spinning.
func TestStart_TransportFailure(t *testing.T) {
	ch, ft := newTestChannel(t)
	ft.startErr = errors.New("dial tcp: connection refused")

	err := ch.Start(context.Background())
	if err == nil {
		t.Fatal("want error from Start, got nil")
	}
	if ch.IsRunning() {
		t.Error("channel reports running after a failed Start")
	}
	if h := ch.HealthSnapshot(); h.State != channels.ChannelHealthStateFailed {
		t.Errorf("health state = %v, want failed", h.State)
	}
	// Stop after a failed Start is what the InstanceLoader does on timeout.
	if err := ch.Stop(context.Background()); err != nil {
		t.Fatalf("Stop after failed Start: %v", err)
	}
}

// The manager may Stop a channel that already stopped (shutdown races, failed
// Start). Closing stopCh twice would panic, so Stop is guarded by sync.Once.
func TestStop_Idempotent(t *testing.T) {
	ch, _ := newTestChannel(t)
	ctx := context.Background()
	if err := ch.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := ch.Stop(ctx); err != nil {
		t.Fatalf("first Stop: %v", err)
	}
	if err := ch.Stop(ctx); err != nil {
		t.Fatalf("second Stop panicked or errored: %v", err)
	}
}

// Stop without a preceding Start happens when Start fails partway. It must not
// panic on the nil transport or the unstarted history flusher.
func TestStop_WithoutStart(t *testing.T) {
	ch, _ := newTestChannel(t)
	if err := ch.Stop(context.Background()); err != nil {
		t.Fatalf("Stop before Start: %v", err)
	}
}

// Returning from the SDK callback is the ack DingTalk waits for. Phase 3 fills
// this in; today it must at least ack rather than error.
func TestHandleBotMessage_Acks(t *testing.T) {
	ch, ft := newTestChannel(t)
	if err := ch.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = ch.Stop(context.Background()) })

	_, err := ft.deliver(context.Background(), &chatbot.BotCallbackDataModel{MsgId: "m1"})
	if err != nil {
		t.Fatalf("inbound handler returned error (DingTalk would treat this as a nack): %v", err)
	}
}

func TestFactory_AllowListWiredToBaseChannel(t *testing.T) {
	ch, err := Factory("dt", json.RawMessage(validCreds),
		json.RawMessage(`{"allow_from":["staff1"]}`), bus.New(), nil)
	if err != nil {
		t.Fatalf("Factory: %v", err)
	}
	if !ch.IsAllowed("staff1") {
		t.Error("staff1 should be allowed")
	}
	if ch.IsAllowed("intruder") {
		t.Error("intruder should not be allowed")
	}
}

// The InstanceLoader runs Start on its own goroutine and calls Stop from
// another when Start overruns its timeout (instance_loader.go:411-454), so Stop
// can read c.transport while Start is still assigning it. Under -race this
// caught a real data race.
func TestStartStop_ConcurrentIsRaceFree(t *testing.T) {
	for range 20 {
		ch, ft := newTestChannel(t)
		// A Start that blocks long enough for Stop to overlap it.
		ft.startDelay = 5 * time.Millisecond

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = ch.Start(context.Background())
		}()
		go func() {
			defer wg.Done()
			_ = ch.Stop(context.Background())
		}()
		wg.Wait()
	}
}

// A saturated bus must not pin inbound goroutines past shutdown.
func TestInbound_PublishGivesUpWhenChannelStops(t *testing.T) {
	msgBus := bus.New()
	ch, err := New(Config{ClientID: "k", ClientSecret: "s", DMPolicy: "open", GroupPolicy: "open"},
		msgBus, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ft := &fakeTransport{}
	ch.newTransport = func(h chatbot.IChatBotMessageHandler) streamTransport {
		ft.handler = h
		return ft
	}
	if err := ch.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	for msgBus.TryPublishInbound(bus.InboundMessage{ChatID: "filler"}) {
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		ch.processMessage(ch.runCtx, &chatbot.BotCallbackDataModel{
			MsgId: "m1", Msgtype: "text", ConversationType: conversationTypeDirect,
			SenderStaffId: "staff-1", Text: chatbot.BotCallbackDataTextModel{Content: "hi"},
		})
	}()

	// Stop cancels runCtx; the blocked publish must give up rather than leak.
	if err := ch.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("processMessage still pinned on a saturated bus after Stop")
	}
}
