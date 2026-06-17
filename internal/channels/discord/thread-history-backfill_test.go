package discord

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
)

func TestDiscordThreadBackfillPrependsPriorMessages(t *testing.T) {
	server := newDiscordThreadBackfillServer(t, discordThreadBackfillFixture{
		historyJSON: `[
			{"id":"m2","channel_id":"thread-1","content":"second prior","author":{"id":"user-2","username":"Bob"}},
			{"id":"m1","channel_id":"thread-1","content":"first prior","author":{"id":"user-1","username":"Alice"}}
		]`,
	})
	defer server.Close()
	ch, mb := newThreadBackfillTestChannel(t, server)

	ch.handleMessage(ch.session, mentionedThreadMessage("current-1", "current request"))

	msg := consumeThreadBackfillInbound(t, mb)
	assertContainsInOrder(t, msg.Content,
		"[Discord thread messages before this mention - for context]",
		"Alice: first prior",
		"Bob: second prior",
		"current request",
	)
}

func TestDiscordThreadBackfillIncludesPriorAttachments(t *testing.T) {
	server := newDiscordThreadBackfillServer(t, discordThreadBackfillFixture{
		historyJSON: `[
			{"id":"m1","channel_id":"thread-1","content":"see attached","author":{"id":"user-1","username":"Alice"},"attachments":[
				{"id":"att-1","filename":"diagram.png","content_type":"image/png","size":4,"url":"__SERVER__/cdn/diagram.png"},
				{"id":"att-2","filename":"brief.pdf","content_type":"application/pdf","size":5,"url":"__SERVER__/cdn/brief.pdf"}
			]}
		]`,
		media: map[string]string{
			"/cdn/diagram.png": "png!",
			"/cdn/brief.pdf":   "%PDF-",
		},
	})
	defer server.Close()
	ch, mb := newThreadBackfillTestChannel(t, server)

	ch.handleMessage(ch.session, mentionedThreadMessage("current-1", "use earlier attachments"))

	msg := consumeThreadBackfillInbound(t, mb)
	if len(msg.Media) != 2 {
		t.Fatalf("media count = %d, want 2: %#v", len(msg.Media), msg.Media)
	}
	assertContainsInOrder(t, msg.Content,
		"see attached",
		"<media:image",
		"<media:document name=\"brief.pdf\">",
		"use earlier attachments",
	)
	want := map[string]string{
		"diagram.png": "image/png",
		"brief.pdf":   "application/pdf",
	}
	for _, file := range msg.Media {
		if got, ok := want[file.Filename]; !ok || got != file.MimeType {
			t.Fatalf("unexpected media file: %#v", file)
		}
	}
}

func TestDiscordThreadBackfillFallsBackWhenHistoryUnavailable(t *testing.T) {
	server := newDiscordThreadBackfillServer(t, discordThreadBackfillFixture{
		historyStatus: http.StatusForbidden,
		historyJSON:   `{"message":"Missing Access"}`,
	})
	defer server.Close()
	ch, mb := newThreadBackfillTestChannel(t, server)

	ch.handleMessage(ch.session, mentionedThreadMessage("current-1", "current only"))

	msg := consumeThreadBackfillInbound(t, mb)
	if !strings.Contains(msg.Content, "current only") {
		t.Fatalf("content = %q, want current message", msg.Content)
	}
	if strings.Contains(msg.Content, "Discord thread messages before") {
		t.Fatalf("unexpected backfill context after history error: %q", msg.Content)
	}
}

func TestDiscordThreadBackfillSkipsNonThreadChannels(t *testing.T) {
	server := newDiscordThreadBackfillServer(t, discordThreadBackfillFixture{
		channelJSON: `{"id":"thread-1","guild_id":"guild-1","type":0}`,
	})
	defer server.Close()
	ch, mb := newThreadBackfillTestChannel(t, server)

	ch.handleMessage(ch.session, mentionedThreadMessage("current-1", "regular channel request"))

	msg := consumeThreadBackfillInbound(t, mb)
	if !strings.Contains(msg.Content, "regular channel request") {
		t.Fatalf("content = %q, want current message", msg.Content)
	}
	if strings.Contains(msg.Content, "Discord thread messages before") {
		t.Fatalf("unexpected backfill context for non-thread channel: %q", msg.Content)
	}
	if len(msg.Media) != 0 {
		t.Fatalf("media count = %d, want 0: %#v", len(msg.Media), msg.Media)
	}
}

func TestDiscordThreadBackfillDoesNotDuplicatePendingThreadHistory(t *testing.T) {
	server := newDiscordThreadBackfillServer(t, discordThreadBackfillFixture{
		historyJSON: `[
			{"id":"prior-1","channel_id":"thread-1","content":"same prior context","author":{"id":"user-1","username":"Alice"},"attachments":[
				{"id":"att-1","filename":"diagram.png","content_type":"image/png","size":4,"url":"__SERVER__/cdn/diagram.png"}
			]}
		]`,
		media: map[string]string{
			"/cdn/diagram.png": "png!",
		},
	})
	defer server.Close()
	ch, mb := newThreadBackfillTestChannel(t, server)

	ch.handleMessage(ch.session, &discordgo.MessageCreate{Message: &discordgo.Message{
		ID:        "prior-1",
		ChannelID: "thread-1",
		GuildID:   "guild-1",
		Content:   "same prior context",
		Author:    &discordgo.User{ID: "user-1", Username: "Alice"},
		Attachments: []*discordgo.MessageAttachment{{
			ID:          "att-1",
			Filename:    "diagram.png",
			ContentType: "image/png",
			Size:        4,
			URL:         server.URL + "/cdn/diagram.png",
		}},
		Timestamp: time.Now(),
	}})
	ch.handleMessage(ch.session, mentionedThreadMessage("current-1", "current request"))

	msg := consumeThreadBackfillInbound(t, mb)
	if got := strings.Count(msg.Content, "same prior context"); got != 1 {
		t.Fatalf("prior context count = %d, want 1:\n%s", got, msg.Content)
	}
	if got := len(msg.Media); got != 1 {
		t.Fatalf("media count = %d, want 1: %#v", got, msg.Media)
	}
}

type discordThreadBackfillFixture struct {
	channelJSON   string
	historyStatus int
	historyJSON   string
	media         map[string]string
}

func newDiscordThreadBackfillServer(t *testing.T, fixture discordThreadBackfillFixture) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(nil)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/channels/thread-1":
			body := fixture.channelJSON
			if body == "" {
				body = `{"id":"thread-1","guild_id":"guild-1","type":11,"parent_id":"parent-1"}`
			}
			_, _ = fmt.Fprint(w, body)
		case r.Method == http.MethodGet && r.URL.Path == "/channels/thread-1/messages":
			if got := r.URL.Query().Get("before"); got != "current-1" {
				t.Fatalf("before query = %q, want current-1", got)
			}
			if got := r.URL.Query().Get("limit"); got != "25" {
				t.Fatalf("limit query = %q, want 25", got)
			}
			status := fixture.historyStatus
			if status == 0 {
				status = http.StatusOK
			}
			w.WriteHeader(status)
			body := strings.ReplaceAll(fixture.historyJSON, "__SERVER__", server.URL)
			_, _ = fmt.Fprint(w, body)
		case r.Method == http.MethodPost && r.URL.Path == "/channels/thread-1/typing":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/channels/thread-1/messages":
			_, _ = fmt.Fprint(w, `{"id":"placeholder-1","channel_id":"thread-1","content":"Thinking..."}`)
		case r.Method == http.MethodGet && fixture.media != nil:
			if body, ok := fixture.media[r.URL.Path]; ok {
				if strings.HasSuffix(r.URL.Path, ".png") {
					w.Header().Set("Content-Type", "image/png")
				}
				_, _ = fmt.Fprint(w, body)
				return
			}
			http.NotFound(w, r)
		default:
			t.Fatalf("unexpected Discord test request: %s %s", r.Method, r.URL.String())
		}
	})
	server.Config.Handler = handler
	return server
}

func newThreadBackfillTestChannel(t *testing.T, server *httptest.Server) (*Channel, *bus.MessageBus) {
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

	mb := bus.New()
	ch := &Channel{
		BaseChannel: channels.NewBaseChannel(channels.TypeDiscord, mb, nil),
		session:     session,
		botUserID:   "bot-1",
		config:      config.DiscordConfig{GroupPolicy: "open", MediaMaxBytes: 8 * 1024 * 1024},
	}
	ch.SetRequireMention(true)
	ch.SetGroupHistory(channels.MakeHistory(channels.TypeDiscord, nil, ch.TenantID()))
	ch.SetHistoryLimit(channels.DefaultGroupHistoryLimit)
	ch.SetRunning(true)
	return ch, mb
}

func mentionedThreadMessage(id, content string) *discordgo.MessageCreate {
	return &discordgo.MessageCreate{Message: &discordgo.Message{
		ID:        id,
		ChannelID: "thread-1",
		GuildID:   "guild-1",
		Content:   "<@bot-1> " + content,
		Author:    &discordgo.User{ID: "current-user", Username: "Current"},
		Mentions:  []*discordgo.User{{ID: "bot-1"}},
		Timestamp: time.Now(),
	}}
}

func consumeThreadBackfillInbound(t *testing.T, mb *bus.MessageBus) bus.InboundMessage {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	msg, ok := mb.ConsumeInbound(ctx)
	if !ok {
		t.Fatal("timed out waiting for inbound message")
	}
	return msg
}

func assertContainsInOrder(t *testing.T, text string, parts ...string) {
	t.Helper()
	offset := 0
	for _, part := range parts {
		idx := strings.Index(text[offset:], part)
		if idx < 0 {
			t.Fatalf("content missing %q after offset %d:\n%s", part, offset, text)
		}
		offset += idx + len(part)
	}
}
