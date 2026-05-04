package channels

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// fakeQuoteChannel implements Channel + DMQuoteChannel.
type fakeQuoteChannel struct {
	name  string
	quote bool
}

func (f *fakeQuoteChannel) Name() string                                  { return f.name }
func (f *fakeQuoteChannel) Type() string                                  { return f.name }
func (f *fakeQuoteChannel) Start(ctx context.Context) error               { return nil }
func (f *fakeQuoteChannel) Stop(ctx context.Context) error                { return nil }
func (f *fakeQuoteChannel) Send(ctx context.Context, _ bus.OutboundMessage) error {
	return nil
}
func (f *fakeQuoteChannel) IsRunning() bool             { return true }
func (f *fakeQuoteChannel) IsAllowed(_ string) bool     { return true }
func (f *fakeQuoteChannel) QuoteInboundOnDM() bool      { return f.quote }

// fakePlainChannel implements only Channel — no DMQuoteChannel.
type fakePlainChannel struct{ name string }

func (f *fakePlainChannel) Name() string                                   { return f.name }
func (f *fakePlainChannel) Type() string                                   { return f.name }
func (f *fakePlainChannel) Start(ctx context.Context) error                { return nil }
func (f *fakePlainChannel) Stop(ctx context.Context) error                 { return nil }
func (f *fakePlainChannel) Send(ctx context.Context, _ bus.OutboundMessage) error {
	return nil
}
func (f *fakePlainChannel) IsRunning() bool         { return true }
func (f *fakePlainChannel) IsAllowed(_ string) bool { return true }

func TestQuoteInboundOnDM_OptedIn(t *testing.T) {
	t.Parallel()
	m := NewManager(bus.New())
	m.RegisterChannel("zalo_oa", &fakeQuoteChannel{name: "zalo_oa", quote: true})

	if !m.QuoteInboundOnDM("zalo_oa") {
		t.Fatal("zalo_oa with QuoteInboundOnDM=true should opt in")
	}
}

func TestQuoteInboundOnDM_NotImplemented(t *testing.T) {
	t.Parallel()
	m := NewManager(bus.New())
	m.RegisterChannel("telegram", &fakePlainChannel{name: "telegram"})

	if m.QuoteInboundOnDM("telegram") {
		t.Fatal("telegram does not implement DMQuoteChannel; must return false")
	}
}

func TestQuoteInboundOnDM_OptedOut(t *testing.T) {
	t.Parallel()
	m := NewManager(bus.New())
	m.RegisterChannel("opt_out", &fakeQuoteChannel{name: "opt_out", quote: false})

	if m.QuoteInboundOnDM("opt_out") {
		t.Fatal("channel that returns false must not opt in")
	}
}

func TestQuoteInboundOnDM_UnknownChannel(t *testing.T) {
	t.Parallel()
	m := NewManager(bus.New())
	if m.QuoteInboundOnDM("missing") {
		t.Fatal("unknown channel must return false")
	}
}
