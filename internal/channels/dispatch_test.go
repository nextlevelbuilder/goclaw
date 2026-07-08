package channels

import (
	"context"
	"errors"
	"testing"

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
