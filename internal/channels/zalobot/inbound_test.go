package zalobot

import (
	"context"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
)

func TestStripBotMention(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		input    string
		botName  string
		username string
		want     string
		matched  bool
	}{
		{
			name:     "mention at start",
			input:    "@MyBot xin chào",
			botName:  "MyBot",
			username: "my_bot",
			want:     "xin chào",
			matched:  true,
		},
		{
			name:     "case insensitive mention",
			input:    "@mybot tóm tắt văn bản",
			botName:  "MyBot",
			username: "my_bot",
			want:     "tóm tắt văn bản",
			matched:  true,
		},
		{
			name:     "username mention",
			input:    "@my_bot help me",
			botName:  "MyBot",
			username: "my_bot",
			want:     "help me",
			matched:  true,
		},
		{
			name:     "no mention",
			input:    "tin nhắn này không tag",
			botName:  "MyBot",
			username: "my_bot",
			want:     "tin nhắn này không tag",
			matched:  false,
		},
		{
			name:     "mention in middle",
			input:    "alo @MyBot ơi giúp với",
			botName:  "MyBot",
			username: "my_bot",
			want:     "alo ơi giúp với",
			matched:  true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, matched := StripBotMention(tc.input, tc.botName, tc.username)
			if matched != tc.matched {
				t.Errorf("matched = %v, want %v", matched, tc.matched)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestProcessUpdate_DirectMessage(t *testing.T) {
	t.Parallel()

	msgBus := bus.New()
	base := channels.NewBaseChannel(channels.TypeZaloBot, msgBus, nil)
	base.SetName("zalo_bot")

	bot := &BotUser{ID: "bot_1", Name: "MyBot", Username: "my_bot"}
	cfg := config.ZaloBotConfig{
		DMPolicy: "open",
	}

	update := Update{
		UpdateID: 1,
		Message: &Message{
			MessageID: "msg_dm_1",
			From: User{
				ID:   "user_100",
				Name: "Nguyen Van A",
			},
			Chat: Chat{
				ID:       "chat_100",
				ChatType: "DIRECT",
			},
			Text: "chào bot",
		},
	}

	handler := NewInboundHandler(base, bot, cfg, nil)
	err := handler.ProcessUpdate(context.Background(), update)
	if err != nil {
		t.Fatalf("ProcessUpdate failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	msg, ok := msgBus.ConsumeInbound(ctx)
	if !ok {
		t.Fatal("expected inbound message on bus, got none")
	}

	if msg.SenderID != "user_100" {
		t.Errorf("expected SenderID=user_100, got %s", msg.SenderID)
	}
	if msg.Content != "chào bot" {
		t.Errorf("expected Content='chào bot', got %q", msg.Content)
	}
	if msg.Metadata["chat_type"] != "direct" {
		t.Errorf("expected metadata chat_type=direct, got %s", msg.Metadata["chat_type"])
	}
	if msg.Metadata["peer_kind"] != "direct" {
		t.Errorf("expected metadata peer_kind=direct, got %s", msg.Metadata["peer_kind"])
	}
}

func TestProcessUpdate_GroupMessageMention(t *testing.T) {
	t.Parallel()

	msgBus := bus.New()
	base := channels.NewBaseChannel(channels.TypeZaloBot, msgBus, nil)
	base.SetName("zalo_bot")

	reqMention := true
	bot := &BotUser{ID: "bot_1", Name: "MyBot", Username: "my_bot"}
	cfg := config.ZaloBotConfig{
		GroupPolicy:    "open",
		RequireMention: &reqMention,
	}

	update := Update{
		UpdateID: 2,
		Message: &Message{
			MessageID: "msg_grp_1",
			From: User{
				ID:   "user_200",
				Name: "Tran Van B",
			},
			Chat: Chat{
				ID:       "grp_500",
				ChatType: "GROUP",
			},
			Text: "@MyBot xử lý báo cáo",
		},
	}

	handler := NewInboundHandler(base, bot, cfg, nil)
	err := handler.ProcessUpdate(context.Background(), update)
	if err != nil {
		t.Fatalf("ProcessUpdate failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	msg, ok := msgBus.ConsumeInbound(ctx)
	if !ok {
		t.Fatal("expected inbound message on bus for group mention, got none")
	}

	if msg.SenderID != "user_200" {
		t.Errorf("expected SenderID=user_200, got %s", msg.SenderID)
	}
	if msg.Content != "xử lý báo cáo" {
		t.Errorf("expected stripped content 'xử lý báo cáo', got %q", msg.Content)
	}
	if msg.Metadata["chat_type"] != "group" {
		t.Errorf("expected chat_type=group, got %s", msg.Metadata["chat_type"])
	}
	if msg.Metadata["group_id"] != "grp_500" {
		t.Errorf("expected group_id=grp_500, got %s", msg.Metadata["group_id"])
	}
	if msg.Metadata["peer_kind"] != "group" {
		t.Errorf("expected peer_kind=group, got %s", msg.Metadata["peer_kind"])
	}
}

func TestProcessUpdate_GroupMessageNoMentionIgnored(t *testing.T) {
	t.Parallel()

	msgBus := bus.New()
	base := channels.NewBaseChannel(channels.TypeZaloBot, msgBus, nil)
	base.SetName("zalo_bot")

	reqMention := true
	bot := &BotUser{ID: "bot_1", Name: "MyBot", Username: "my_bot"}
	cfg := config.ZaloBotConfig{
		GroupPolicy:    "open",
		RequireMention: &reqMention,
	}

	update := Update{
		UpdateID: 3,
		Message: &Message{
			MessageID: "msg_grp_2",
			From: User{
				ID:   "user_200",
				Name: "Tran Van B",
			},
			Chat: Chat{
				ID:       "grp_500",
				ChatType: "GROUP",
			},
			Text: "tin nhắn không mention",
		},
	}

	handler := NewInboundHandler(base, bot, cfg, nil)
	err := handler.ProcessUpdate(context.Background(), update)
	if err != nil {
		t.Fatalf("ProcessUpdate failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if msg, ok := msgBus.ConsumeInbound(ctx); ok {
		t.Fatalf("expected no message on bus, got: %+v", msg)
	}
}
