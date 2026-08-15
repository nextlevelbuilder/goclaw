package discord

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/bwmarrin/discordgo"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/channels/typing"
)

func TestSendStopsTypingAfterPlaceholderEditSucceeds(t *testing.T) {
	var stopCalled atomic.Bool
	var stopBeforeRequest atomic.Bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/channels/channel-1/messages/placeholder-1" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if stopCalled.Load() {
			stopBeforeRequest.Store(true)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"placeholder-1","channel_id":"channel-1","content":"done"}`))
	}))
	defer server.Close()

	ch := newTestChannel(t, server)

	ctrl := typing.New(typing.Options{
		StopFn: func() error {
			stopCalled.Store(true)
			return nil
		},
	})
	ch.typingCtrls.Store("channel-1", ctrl)
	ch.placeholders.Store("inbound-1", "placeholder-1")

	err := ch.Send(context.Background(), bus.OutboundMessage{
		Channel: "discord",
		ChatID:  "channel-1",
		Content: "done",
		Metadata: map[string]string{
			"placeholder_key": "inbound-1",
		},
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if !stopCalled.Load() {
		t.Fatal("expected typing controller to stop after successful placeholder edit")
	}
	if stopBeforeRequest.Load() {
		t.Fatal("typing controller stopped before placeholder edit request")
	}
	if _, ok := ch.typingCtrls.Load("channel-1"); ok {
		t.Fatal("expected typing controller to be removed after successful delivery")
	}
}

func TestSendKeepsTypingActiveWhenDeliveryFails(t *testing.T) {
	var stopCalled atomic.Bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/channels/channel-1/messages" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	ch := newTestChannel(t, server)

	ctrl := typing.New(typing.Options{
		StopFn: func() error {
			stopCalled.Store(true)
			return nil
		},
	})
	ch.typingCtrls.Store("channel-1", ctrl)

	err := ch.Send(context.Background(), bus.OutboundMessage{
		Channel: "discord",
		ChatID:  "channel-1",
		Content: "done",
	})
	if err == nil {
		t.Fatal("expected Send() to return an error")
	}
	if stopCalled.Load() {
		t.Fatal("typing controller stopped even though Discord delivery failed")
	}
	if stored, ok := ch.typingCtrls.Load("channel-1"); !ok || stored != ctrl {
		t.Fatal("expected typing controller to remain active after delivery failure")
	}
}

func newTestChannel(t *testing.T, server *httptest.Server) *Channel {
	t.Helper()

	prevEndpointChannels := discordgo.EndpointChannels
	discordgo.EndpointChannels = server.URL + "/channels/"
	t.Cleanup(func() {
		discordgo.EndpointChannels = prevEndpointChannels
	})

	session, err := discordgo.New("Bot test-token")
	if err != nil {
		t.Fatalf("discordgo.New() error = %v", err)
	}
	session.Client = server.Client()

	ch := &Channel{
		BaseChannel: channels.NewBaseChannel(channels.TypeDiscord, nil, nil),
		session:     session,
	}
	ch.SetRunning(true)
	return ch
}
