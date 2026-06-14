package channels

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// posterChannel implements Channel + ChannelPoster.
type posterChannel struct {
	name       string
	gotChat    string
	gotButtons []PostButton
	msgID      int64
}

func (p *posterChannel) Name() string                                { return p.name }
func (p *posterChannel) Type() string                                { return "telegram" }
func (p *posterChannel) Start(context.Context) error                 { return nil }
func (p *posterChannel) Stop(context.Context) error                  { return nil }
func (p *posterChannel) Send(context.Context, bus.OutboundMessage) error { return nil }
func (p *posterChannel) IsRunning() bool                             { return true }
func (p *posterChannel) IsAllowed(string) bool                       { return true }
func (p *posterChannel) SendChannelPost(_ context.Context, chatID, _, _ string, buttons []PostButton) (int64, error) {
	p.gotChat = chatID
	p.gotButtons = buttons
	return p.msgID, nil
}

// plainChannel implements Channel but NOT ChannelPoster.
type plainChannel struct{ name string }

func (p *plainChannel) Name() string                                { return p.name }
func (p *plainChannel) Type() string                                { return "discord" }
func (p *plainChannel) Start(context.Context) error                 { return nil }
func (p *plainChannel) Stop(context.Context) error                  { return nil }
func (p *plainChannel) Send(context.Context, bus.OutboundMessage) error { return nil }
func (p *plainChannel) IsRunning() bool                             { return true }
func (p *plainChannel) IsAllowed(string) bool                       { return true }

func TestManager_PublishChannelPost(t *testing.T) {
	poster := &posterChannel{name: "tg", msgID: 4242}
	m := &Manager{channels: map[string]Channel{"tg": poster, "dc": &plainChannel{name: "dc"}}}

	id, err := m.PublishChannelPost(context.Background(), "tg", "-100123", "/img.png", "cap",
		[]PostButton{{Label: "Play now", URL: "https://t.me/Bot?startapp"}})
	if err != nil {
		t.Fatalf("PublishChannelPost: %v", err)
	}
	if id != 4242 {
		t.Fatalf("expected message id 4242, got %d", id)
	}
	if poster.gotChat != "-100123" || len(poster.gotButtons) != 1 {
		t.Fatalf("send args not forwarded: chat=%q buttons=%d", poster.gotChat, len(poster.gotButtons))
	}

	// Unknown channel → error.
	if _, err := m.PublishChannelPost(context.Background(), "nope", "c", "i", "cap", nil); err == nil {
		t.Error("expected error for unknown channel")
	}
	// Channel that can't post rich content → error.
	if _, err := m.PublishChannelPost(context.Background(), "dc", "c", "i", "cap", nil); err == nil {
		t.Error("expected error for non-poster channel")
	}
}
