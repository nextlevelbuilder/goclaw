package channels

import (
	"context"
	"errors"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

type fakeHealthChannel struct {
	*BaseChannel
	startErr error
}

func newFakeHealthChannel(name string) *fakeHealthChannel {
	return &fakeHealthChannel{
		BaseChannel: NewBaseChannel(name, bus.New(), nil),
	}
}

func (c *fakeHealthChannel) Start(context.Context) error {
	if c.startErr != nil {
		return c.startErr
	}
	c.SetRunning(true)
	return nil
}

func (c *fakeHealthChannel) Stop(context.Context) error {
	c.SetRunning(false)
	return nil
}

func (c *fakeHealthChannel) Send(context.Context, bus.OutboundMessage) error { return nil }

func TestManagerGetStatusIncludesPreRegistrationFailures(t *testing.T) {
	mgr := NewManager(bus.New())

	mgr.RecordFailure("telegram-main", "", errors.New(`telego: getMe: api: 401 "Unauthorized"`))

	raw, ok := mgr.GetStatus()["telegram-main"]
	if !ok {
		t.Fatal("expected failed instance in status map")
	}
	status, ok := raw.(ChannelHealth)
	if !ok {
		t.Fatalf("expected ChannelHealth entry, got %T", raw)
	}
	if status.State != ChannelHealthStateFailed {
		t.Fatalf("expected failed state, got %q", status.State)
	}
	if status.FailureKind != ChannelFailureKindAuth {
		t.Fatalf("expected auth failure kind, got %q", status.FailureKind)
	}
}

func TestManagerStartAllPromotesHealthyChannels(t *testing.T) {
	mgr := NewManager(bus.New())
	channel := newFakeHealthChannel("telegram-main")
	mgr.RegisterChannel("telegram-main", channel)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := mgr.StartAll(ctx); err != nil {
		t.Fatalf("StartAll returned error: %v", err)
	}

	raw := mgr.GetStatus()["telegram-main"]
	status, ok := raw.(ChannelHealth)
	if !ok {
		t.Fatalf("expected ChannelHealth entry, got %T", raw)
	}
	if !status.Running {
		t.Fatal("expected running=true")
	}
	if status.State != ChannelHealthStateHealthy {
		t.Fatalf("expected healthy state, got %q", status.State)
	}
}

func TestManagerStartAllCapturesStartupFailures(t *testing.T) {
	mgr := NewManager(bus.New())
	channel := newFakeHealthChannel("telegram-main")
	channel.startErr = errors.New(`telego: getUpdates: api: 401 "Unauthorized"`)
	mgr.RegisterChannel("telegram-main", channel)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := mgr.StartAll(ctx); err != nil {
		t.Fatalf("StartAll returned error: %v", err)
	}

	raw := mgr.GetStatus()["telegram-main"]
	status, ok := raw.(ChannelHealth)
	if !ok {
		t.Fatalf("expected ChannelHealth entry, got %T", raw)
	}
	if status.State != ChannelHealthStateFailed {
		t.Fatalf("expected failed state, got %q", status.State)
	}
	if status.FailureKind != ChannelFailureKindAuth {
		t.Fatalf("expected auth failure kind, got %q", status.FailureKind)
	}
	if status.FailureCount < 1 {
		t.Fatalf("expected failure count to increment, got %d", status.FailureCount)
	}
}
