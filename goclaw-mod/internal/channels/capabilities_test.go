package channels

import (
	"context"
	"errors"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// --- IsMediaCapable ---

func TestIsMediaCapable_KnownCapableTypes(t *testing.T) {
	t.Parallel()
	capable := []string{
		TypeTelegram, TypeDiscord, TypeWhatsApp, TypeFeishu,
		TypeSlack, TypeZaloPersonal, TypePancake, TypeFacebook,
	}
	for _, ct := range capable {
		if !IsMediaCapable(ct) {
			t.Errorf("IsMediaCapable(%q) = false, want true", ct)
		}
	}
}

func TestIsMediaCapable_UnsupportedTypes(t *testing.T) {
	t.Parallel()
	unsupported := []string{
		TypeZaloOA, "unknown", "", "cli", "system",
	}
	for _, ct := range unsupported {
		if IsMediaCapable(ct) {
			t.Errorf("IsMediaCapable(%q) = true, want false", ct)
		}
	}
}

// --- SendMediaToChannel ---

// mockChannel implements Channel for testing SendMediaToChannel.
type mockChannel struct {
	BaseChannel
	channelType string
	lastMsg     bus.OutboundMessage
	sendErr     error
}

func newMockChannel(name, channelType string) *mockChannel {
	mc := &mockChannel{channelType: channelType}
	mc.BaseChannel = BaseChannel{name: name}
	return mc
}

func (m *mockChannel) Type() string                                     { return m.channelType }
func (m *mockChannel) Start(_ context.Context) error                    { return nil }
func (m *mockChannel) Stop(_ context.Context) error                     { return nil }
func (m *mockChannel) IsRunning() bool                                  { return true }
func (m *mockChannel) IsAllowed(_ string) bool                          { return true }
func (m *mockChannel) Send(_ context.Context, msg bus.OutboundMessage) error {
	m.lastMsg = msg
	return m.sendErr
}

func TestSendMediaToChannel_PassesMediaToAdapter(t *testing.T) {
	t.Parallel()

	mb := bus.New()
	mgr := NewManager(mb)

	ch := newMockChannel("telegram-test", TypeTelegram)
	mgr.channels["telegram-test"] = ch

	media := []bus.MediaAttachment{
		{URL: "/tmp/test.jpg", ContentType: "image/jpeg", Caption: "hello"},
	}

	err := mgr.SendMediaToChannel(context.Background(), "telegram-test", "chat123", "text", media)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ch.lastMsg.Media) != 1 {
		t.Fatalf("expected 1 media attachment, got %d", len(ch.lastMsg.Media))
	}
	if ch.lastMsg.Media[0].URL != "/tmp/test.jpg" {
		t.Errorf("media URL mismatch: got %q", ch.lastMsg.Media[0].URL)
	}
	if ch.lastMsg.Content != "text" {
		t.Errorf("content mismatch: got %q", ch.lastMsg.Content)
	}
	if ch.lastMsg.ChatID != "chat123" {
		t.Errorf("chatID mismatch: got %q", ch.lastMsg.ChatID)
	}
}

func TestSendMediaToChannel_ReturnsErrMediaUnsupported_ForZaloOA(t *testing.T) {
	t.Parallel()

	mb := bus.New()
	mgr := NewManager(mb)

	ch := newMockChannel("zalo-oa-test", TypeZaloOA)
	mgr.channels["zalo-oa-test"] = ch

	media := []bus.MediaAttachment{{URL: "/tmp/img.png", ContentType: "image/png"}}
	err := mgr.SendMediaToChannel(context.Background(), "zalo-oa-test", "chat1", "", media)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrMediaUnsupported) {
		t.Errorf("expected ErrMediaUnsupported, got: %v", err)
	}
}

func TestSendMediaToChannel_ErrorOnEmptyMedia(t *testing.T) {
	t.Parallel()

	mb := bus.New()
	mgr := NewManager(mb)

	ch := newMockChannel("telegram-test", TypeTelegram)
	mgr.channels["telegram-test"] = ch

	err := mgr.SendMediaToChannel(context.Background(), "telegram-test", "chat1", "text", nil)
	if err == nil {
		t.Fatal("expected error for empty media, got nil")
	}
}

func TestSendMediaToChannel_ErrorOnChannelNotFound(t *testing.T) {
	t.Parallel()

	mb := bus.New()
	mgr := NewManager(mb)

	media := []bus.MediaAttachment{{URL: "/tmp/img.jpg", ContentType: "image/jpeg"}}
	err := mgr.SendMediaToChannel(context.Background(), "nonexistent", "chat1", "", media)
	if err == nil {
		t.Fatal("expected error for unknown channel, got nil")
	}
}

func TestSendToChannel_UnchangedByNewMethod(t *testing.T) {
	t.Parallel()

	mb := bus.New()
	mgr := NewManager(mb)

	ch := newMockChannel("telegram-test", TypeTelegram)
	mgr.channels["telegram-test"] = ch

	err := mgr.SendToChannel(context.Background(), "telegram-test", "chat1", "hello world")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ch.lastMsg.Content != "hello world" {
		t.Errorf("content mismatch: got %q", ch.lastMsg.Content)
	}
	if len(ch.lastMsg.Media) != 0 {
		t.Errorf("expected no media, got %d attachments", len(ch.lastMsg.Media))
	}
}
