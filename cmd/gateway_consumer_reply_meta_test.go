package cmd

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

// quoteOptInChannel implements channels.Channel + channels.DMQuoteChannel.
type quoteOptInChannel struct{ name string }

func (q *quoteOptInChannel) Name() string                                          { return q.name }
func (q *quoteOptInChannel) Type() string                                          { return q.name }
func (q *quoteOptInChannel) Start(ctx context.Context) error                       { return nil }
func (q *quoteOptInChannel) Stop(ctx context.Context) error                        { return nil }
func (q *quoteOptInChannel) Send(ctx context.Context, _ bus.OutboundMessage) error { return nil }
func (q *quoteOptInChannel) IsRunning() bool                                       { return true }
func (q *quoteOptInChannel) IsAllowed(_ string) bool                               { return true }
func (q *quoteOptInChannel) QuoteInboundOnDM() bool                                { return true }

// plainChannel implements only Channel.
type plainChannel struct{ name string }

func (p *plainChannel) Name() string                                          { return p.name }
func (p *plainChannel) Type() string                                          { return p.name }
func (p *plainChannel) Start(ctx context.Context) error                       { return nil }
func (p *plainChannel) Stop(ctx context.Context) error                        { return nil }
func (p *plainChannel) Send(ctx context.Context, _ bus.OutboundMessage) error { return nil }
func (p *plainChannel) IsRunning() bool                                       { return true }
func (p *plainChannel) IsAllowed(_ string) bool                               { return true }

func TestBuildOutboundReplyMeta_DMOptedIn(t *testing.T) {
	t.Parallel()
	mgr := channels.NewManager(bus.New())
	mgr.RegisterChannel("zalo_oa", &quoteOptInChannel{name: "zalo_oa"})

	out := buildOutboundReplyMeta(map[string]string{"message_id": "mid-1"}, "zalo_oa", false, mgr)
	if out["reply_to_message_id"] != "mid-1" {
		t.Errorf("reply_to_message_id = %q, want mid-1", out["reply_to_message_id"])
	}
}

func TestBuildOutboundReplyMeta_DMNotOptedIn(t *testing.T) {
	t.Parallel()
	mgr := channels.NewManager(bus.New())
	mgr.RegisterChannel("telegram", &plainChannel{name: "telegram"})

	out := buildOutboundReplyMeta(map[string]string{"message_id": "mid-1"}, "telegram", false, mgr)
	if _, ok := out["reply_to_message_id"]; ok {
		t.Errorf("reply_to_message_id must not be stamped on telegram DM, got out=%v", out)
	}
}

func TestBuildOutboundReplyMeta_GroupAlwaysStamps(t *testing.T) {
	t.Parallel()
	mgr := channels.NewManager(bus.New())
	mgr.RegisterChannel("telegram", &plainChannel{name: "telegram"})

	out := buildOutboundReplyMeta(map[string]string{"message_id": "mid-2"}, "telegram", true, mgr)
	if out["reply_to_message_id"] != "mid-2" {
		t.Errorf("group must stamp regardless of capability; got %q", out["reply_to_message_id"])
	}
}

func TestBuildOutboundReplyMeta_NoMessageID(t *testing.T) {
	t.Parallel()
	mgr := channels.NewManager(bus.New())
	mgr.RegisterChannel("zalo_oa", &quoteOptInChannel{name: "zalo_oa"})

	out := buildOutboundReplyMeta(map[string]string{}, "zalo_oa", false, mgr)
	if _, ok := out["reply_to_message_id"]; ok {
		t.Errorf("missing message_id must not produce a quote; got out=%v", out)
	}
}

func TestBuildOutboundReplyMeta_NilManager(t *testing.T) {
	t.Parallel()
	out := buildOutboundReplyMeta(map[string]string{"message_id": "x"}, "anything", false, nil)
	if _, ok := out["reply_to_message_id"]; ok {
		t.Errorf("nil manager DM must not stamp; got out=%v", out)
	}
}
