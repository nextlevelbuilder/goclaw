package mattermost

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

const shortWait = 200 * time.Millisecond

func TestHandlePosted_DMPublishesInbound(t *testing.T) {
	fs := newFakeServer(t)
	ch, msgBus := newTestChannel(t, fs, nil)

	ch.handlePostedEvent(context.Background(), postedEvent(t, mmPost{
		ID: "p1", UserID: "u1", ChannelID: "dm1", Message: "hello",
	}, "D", nil))

	msg, ok := consumeInbound(t, msgBus, time.Second)
	if !ok {
		t.Fatal("expected inbound message")
	}
	if msg.Content != "hello" || msg.PeerKind != "direct" || msg.ChatID != "dm1" || msg.SenderID != "u1" {
		t.Fatalf("unexpected inbound: %+v", msg)
	}
	if msg.Metadata["local_key"] != "dm1" || msg.Metadata["message_thread_id"] != "" {
		t.Fatalf("DM must stay flat, got metadata %v", msg.Metadata)
	}
	if msg.Metadata["display_name"] != "Alice Smith" || msg.Metadata["username"] != "alice" {
		t.Fatalf("unexpected sender names: %v", msg.Metadata)
	}

	placeholders := fs.callsTo("POST", "/api/v4/posts")
	if len(placeholders) != 1 || placeholders[0].Body["message"] != placeholderText {
		t.Fatalf("expected one placeholder post, got %+v", placeholders)
	}
	if pID, ok := ch.placeholders.Load("dm1"); !ok || pID == "" {
		t.Fatal("placeholder not stored under local key")
	}
}

func TestHandlePosted_GroupRecordsHistoryUntilMentioned(t *testing.T) {
	fs := newFakeServer(t)
	ch, msgBus := newTestChannel(t, fs, nil)

	ch.handlePostedEvent(context.Background(), postedEvent(t, mmPost{
		ID: "p1", UserID: "u1", ChannelID: "c1", Message: "deploy failed again",
	}, "O", nil))
	if _, ok := consumeInbound(t, msgBus, shortWait); ok {
		t.Fatal("un-mentioned channel post must not reach the agent")
	}

	ch.handlePostedEvent(context.Background(), postedEvent(t, mmPost{
		ID: "p2", UserID: "u1", ChannelID: "c1", Message: "@goclawbot why?",
	}, "O", nil))

	msg, ok := consumeInbound(t, msgBus, time.Second)
	if !ok {
		t.Fatal("expected inbound message after mention")
	}
	if msg.PeerKind != "group" {
		t.Fatalf("PeerKind = %q, want group", msg.PeerKind)
	}
	for _, want := range []string{"deploy failed again", "[From: Alice Smith]", "why?"} {
		if !strings.Contains(msg.Content, want) {
			t.Fatalf("content missing %q:\n%s", want, msg.Content)
		}
	}
	if strings.Contains(msg.Content, "@goclawbot") {
		t.Fatalf("bot mention must be stripped:\n%s", msg.Content)
	}
	if msg.Metadata["local_key"] != "c1:thread:p2" || msg.Metadata["message_thread_id"] != "p2" {
		t.Fatalf("channel reply must open a thread on the trigger post, got %v", msg.Metadata)
	}
	if msg.Metadata[tools.MetaChatTitle] != "Town Square" {
		t.Fatalf("chat title not propagated: %v", msg.Metadata)
	}

	placeholders := fs.callsTo("POST", "/api/v4/posts")
	if len(placeholders) != 1 || placeholders[0].Body["root_id"] != "p2" {
		t.Fatalf("placeholder must be posted in the thread, got %+v", placeholders)
	}
}

func TestHandlePosted_ChannelWideMentionDoesNotTrigger(t *testing.T) {
	fs := newFakeServer(t)
	ch, msgBus := newTestChannel(t, fs, nil)

	// The server lists the bot in "mentions" for @channel/@here/@all.
	for i, text := range []string{"@channel deploy at 5pm", "@here status?", "@all heads up"} {
		ch.handlePostedEvent(context.Background(), postedEvent(t, mmPost{
			ID: fmt.Sprintf("p%d", i), UserID: "u1", ChannelID: "c1", Message: text,
		}, "O", []string{testBotID}))
	}
	if _, ok := consumeInbound(t, msgBus, shortWait); ok {
		t.Fatal("channel-wide mentions must not trigger the bot")
	}
}

func TestHandlePosted_SkipsOwnSystemBotAndDuplicatePosts(t *testing.T) {
	fs := newFakeServer(t)
	ch, msgBus := newTestChannel(t, fs, nil)
	ctx := context.Background()

	ch.handlePostedEvent(ctx, postedEvent(t, mmPost{ID: "p1", UserID: testBotID, ChannelID: "dm1", Message: "own"}, "D", nil))
	ch.handlePostedEvent(ctx, postedEvent(t, mmPost{ID: "p2", UserID: "u1", ChannelID: "dm1", Message: "joined", Type: "system_join_channel"}, "D", nil))
	ch.handlePostedEvent(ctx, postedEvent(t, mmPost{ID: "p3", UserID: "u2", ChannelID: "dm1", Message: "beep", Props: map[string]any{"from_bot": "true"}}, "D", nil))
	if _, ok := consumeInbound(t, msgBus, shortWait); ok {
		t.Fatal("own, system and bot posts must be ignored")
	}

	dup := postedEvent(t, mmPost{ID: "p4", UserID: "u1", ChannelID: "dm1", Message: "once"}, "D", nil)
	ch.handlePostedEvent(ctx, dup)
	ch.handlePostedEvent(ctx, dup)
	if _, ok := consumeInbound(t, msgBus, time.Second); !ok {
		t.Fatal("expected first copy to be delivered")
	}
	if _, ok := consumeInbound(t, msgBus, shortWait); ok {
		t.Fatal("replayed post must be deduplicated")
	}
}

func TestHandlePosted_ThreadParticipationAndRootContext(t *testing.T) {
	fs := newFakeServer(t)
	ch, msgBus := newTestChannel(t, fs, nil)
	ctx := context.Background()
	fs.addPost(&mmPost{ID: "r1", UserID: "u1", ChannelID: "c1", Message: "@goclawbot summarize the incident"})

	ch.handlePostedEvent(ctx, postedEvent(t, mmPost{ID: "r1", UserID: "u1", ChannelID: "c1", Message: "@goclawbot summarize the incident"}, "O", nil))
	if _, ok := consumeInbound(t, msgBus, time.Second); !ok {
		t.Fatal("expected mention to be delivered")
	}

	// Follow-up in the bot's thread without a mention.
	ch.handlePostedEvent(ctx, postedEvent(t, mmPost{ID: "p2", UserID: "u1", ChannelID: "c1", RootID: "r1", Message: "and the root cause?"}, "O", nil))
	msg, ok := consumeInbound(t, msgBus, time.Second)
	if !ok {
		t.Fatal("reply in a participated thread must not require a mention")
	}
	if msg.Metadata["local_key"] != "c1:thread:r1" {
		t.Fatalf("thread reply must share the thread session, got %v", msg.Metadata)
	}
	if !strings.Contains(msg.Content, "[Replying to Alice Smith]") || !strings.Contains(msg.Content, "summarize the incident") {
		t.Fatalf("thread root context missing:\n%s", msg.Content)
	}
}

func TestHandlePosted_ThreadTTLDisabledRequiresMention(t *testing.T) {
	fs := newFakeServer(t)
	zero := 0
	ch, msgBus := newTestChannel(t, fs, func(c *config.MattermostConfig) { c.ThreadTTL = &zero })
	ctx := context.Background()

	ch.handlePostedEvent(ctx, postedEvent(t, mmPost{ID: "r1", UserID: "u1", ChannelID: "c1", Message: "@goclawbot hi"}, "O", nil))
	if _, ok := consumeInbound(t, msgBus, time.Second); !ok {
		t.Fatal("expected mention to be delivered")
	}
	ch.handlePostedEvent(ctx, postedEvent(t, mmPost{ID: "p2", UserID: "u1", ChannelID: "c1", RootID: "r1", Message: "more"}, "O", nil))
	if _, ok := consumeInbound(t, msgBus, shortWait); ok {
		t.Fatal("thread_ttl=0 must always require a mention")
	}
}

func TestHandlePosted_AllowlistAcceptsChannelID(t *testing.T) {
	fs := newFakeServer(t)
	ch, msgBus := newTestChannel(t, fs, func(c *config.MattermostConfig) {
		c.GroupPolicy = "allowlist"
		c.AllowFrom = []string{"c-allowed"}
	})
	ctx := context.Background()

	ch.handlePostedEvent(ctx, postedEvent(t, mmPost{ID: "p1", UserID: "u1", ChannelID: "c-other", Message: "@goclawbot hi"}, "O", nil))
	if _, ok := consumeInbound(t, msgBus, shortWait); ok {
		t.Fatal("channel outside the allowlist must be rejected")
	}
	ch.handlePostedEvent(ctx, postedEvent(t, mmPost{ID: "p2", UserID: "u1", ChannelID: "c-allowed", Message: "@goclawbot hi"}, "O", nil))
	if _, ok := consumeInbound(t, msgBus, time.Second); !ok {
		t.Fatal("allowlisted channel must be accepted")
	}
}

func TestHandlePosted_GroupPairingUsesChannelTemplate(t *testing.T) {
	fs := newFakeServer(t)
	ch, msgBus := newTestChannel(t, fs, func(c *config.MattermostConfig) { c.GroupPolicy = "pairing" })
	ps := &fakePairingStore{}
	ch.SetPairingService(ps)

	ch.handlePostedEvent(context.Background(), postedEvent(t, mmPost{ID: "p1", UserID: "u1", ChannelID: "c1", Message: "@goclawbot hi"}, "O", nil))
	if _, ok := consumeInbound(t, msgBus, shortWait); ok {
		t.Fatal("unpaired channel must not reach the agent")
	}
	if len(ps.requests) != 1 || ps.requests[0] != "group:c1" {
		t.Fatalf("expected group pairing request, got %v", ps.requests)
	}
	posts := fs.callsTo("POST", "/api/v4/posts")
	if len(posts) != 1 {
		t.Fatalf("expected one pairing reply, got %d", len(posts))
	}
	if !strings.Contains(posts[0].Body["message"].(string), "This channel is not authorized") {
		t.Fatalf("channel must get the group approval template, got %q", posts[0].Body["message"])
	}
}

func TestHandlePosted_UnpairedChannelStaysQuietWithoutMention(t *testing.T) {
	fs := newFakeServer(t)
	ch, msgBus := newTestChannel(t, fs, func(c *config.MattermostConfig) { c.GroupPolicy = "pairing" })
	ps := &fakePairingStore{}
	ch.SetPairingService(ps)

	ch.handlePostedEvent(context.Background(), postedEvent(t, mmPost{ID: "p1", UserID: "u1", ChannelID: "c1", Message: "lunch anyone?"}, "O", nil))
	if _, ok := consumeInbound(t, msgBus, shortWait); ok {
		t.Fatal("unpaired channel must not reach the agent")
	}
	if len(ps.requests) != 0 || len(fs.callsTo("POST", "/api/v4/posts")) != 0 {
		t.Fatalf("chatter without a mention must not get a pairing prompt, got requests %v", ps.requests)
	}
}

func TestHandlePosted_SecondDMPostReusesPendingPlaceholder(t *testing.T) {
	fs := newFakeServer(t)
	ch, msgBus := newTestChannel(t, fs, nil)
	ctx := context.Background()

	ch.handlePostedEvent(ctx, postedEvent(t, mmPost{ID: "p1", UserID: "u1", ChannelID: "dm1", Message: "first"}, "D", nil))
	ch.handlePostedEvent(ctx, postedEvent(t, mmPost{ID: "p2", UserID: "u1", ChannelID: "dm1", Message: "second"}, "D", nil))
	for range 2 {
		if _, ok := consumeInbound(t, msgBus, time.Second); !ok {
			t.Fatal("expected both posts to be delivered")
		}
	}
	if posts := fs.callsTo("POST", "/api/v4/posts"); len(posts) != 1 {
		t.Fatalf("a pending placeholder must be reused, got %d placeholder posts", len(posts))
	}
}

func TestHandlePosted_DownloadsAttachments(t *testing.T) {
	fs := newFakeServer(t)
	ch, msgBus := newTestChannel(t, fs, nil)
	fs.addFile("f1", []byte("quarterly numbers"))

	ch.handlePostedEvent(context.Background(), postedEvent(t, mmPost{
		ID: "p1", UserID: "u1", ChannelID: "dm1", Message: "see attached",
		Metadata: &mmPostMetadata{Files: []mmFileInfo{{ID: "f1", Name: "notes.txt", Size: 17, MimeType: "text/plain"}}},
	}, "D", nil))

	msg, ok := consumeInbound(t, msgBus, time.Second)
	if !ok {
		t.Fatal("expected inbound message")
	}
	if len(msg.Media) != 1 || msg.Media[0].MimeType != "text/plain" || msg.Media[0].Filename != "notes.txt" {
		t.Fatalf("unexpected media: %+v", msg.Media)
	}
	for _, want := range []string{`<media:document name="notes.txt">`, "quarterly numbers", "see attached"} {
		if !strings.Contains(msg.Content, want) {
			t.Fatalf("content missing %q:\n%s", want, msg.Content)
		}
	}
}

func TestHandlePosted_GroupDMKindFollowsSetting(t *testing.T) {
	fs := newFakeServer(t)
	ctx := context.Background()

	asChannel, msgBus := newTestChannel(t, fs, nil)
	asChannel.handlePostedEvent(ctx, postedEvent(t, mmPost{ID: "p1", UserID: "u1", ChannelID: "g1", Message: "anyone?"}, "G", nil))
	if _, ok := consumeInbound(t, msgBus, shortWait); ok {
		t.Fatal("by default a group DM is a channel and needs a mention")
	}

	on := true
	asDM, msgBus := newTestChannel(t, fs, func(c *config.MattermostConfig) { c.GroupDMAsDirect = &on })
	asDM.handlePostedEvent(ctx, postedEvent(t, mmPost{ID: "p2", UserID: "u1", ChannelID: "g1", Message: "anyone?"}, "G", nil))
	msg, ok := consumeInbound(t, msgBus, time.Second)
	if !ok {
		t.Fatal("group_dm_as_direct must answer group DMs without a mention")
	}
	if msg.Metadata["local_key"] != "g1" || msg.Metadata["message_thread_id"] != "" {
		t.Fatalf("group DM must be answered flat, got metadata %v", msg.Metadata)
	}
	// Still a group peer: participants must not share a DM session.
	if msg.PeerKind != "group" {
		t.Fatalf("PeerKind = %q, want group", msg.PeerKind)
	}
}

func TestHandleEdited_AddedMentionTriggersOnce(t *testing.T) {
	fs := newFakeServer(t)
	ch, msgBus := newTestChannel(t, fs, nil)
	ctx := context.Background()
	now := time.Now().UnixMilli()

	original := mmPost{ID: "p1", CreateAt: now, UserID: "u1", ChannelID: "c1", Message: "who can check the deploy?"}
	ch.handlePostedEvent(ctx, postedEvent(t, original, "O", nil))
	if _, ok := consumeInbound(t, msgBus, shortWait); ok {
		t.Fatal("un-mentioned post must not reach the agent")
	}

	edited := original
	edited.Message = "@goclawbot who can check the deploy?"
	ch.handlePostEditedEvent(ctx, editedEvent(t, edited))
	msg, ok := consumeInbound(t, msgBus, time.Second)
	if !ok {
		t.Fatal("edit that adds a mention must trigger the bot")
	}
	if msg.Metadata["message_id"] != "p1" || msg.Metadata["local_key"] != "c1:thread:p1" {
		t.Fatalf("edited post must be answered in its own thread, got %v", msg.Metadata)
	}
	if msg.Metadata[tools.MetaChatTitle] != "Town Square" {
		t.Fatalf("chat title must come from the channel lookup, got %v", msg.Metadata)
	}

	edited.Message = "@goclawbot who can check the deploy today?"
	ch.handlePostEditedEvent(ctx, editedEvent(t, edited))
	if _, ok := consumeInbound(t, msgBus, shortWait); ok {
		t.Fatal("an answered post must not be re-triggered by further edits")
	}
}

func TestHandleEdited_IgnoresAnsweredDMOwnAndOldPosts(t *testing.T) {
	fs := newFakeServer(t)
	ch, msgBus := newTestChannel(t, fs, nil)
	ctx := context.Background()
	now := time.Now().UnixMilli()

	// Answered on the first send.
	answered := mmPost{ID: "p1", CreateAt: now, UserID: "u1", ChannelID: "c1", Message: "@goclawbot hi"}
	ch.handlePostedEvent(ctx, postedEvent(t, answered, "O", nil))
	if _, ok := consumeInbound(t, msgBus, time.Second); !ok {
		t.Fatal("expected mention to be delivered")
	}
	answered.Message = "@goclawbot hi there"
	ch.handlePostEditedEvent(ctx, editedEvent(t, answered))

	// DM edits.
	fs.addChannel(mmChannel{ID: "dm1", Type: "D"})
	ch.handlePostEditedEvent(ctx, editedEvent(t, mmPost{ID: "p2", CreateAt: now, UserID: "u1", ChannelID: "dm1", Message: "@goclawbot fixed typo"}))

	// Posts older than the edit window.
	old := time.Now().Add(-editWindow - time.Hour).UnixMilli()
	ch.handlePostEditedEvent(ctx, editedEvent(t, mmPost{ID: "p3", CreateAt: old, UserID: "u1", ChannelID: "c1", Message: "@goclawbot ancient"}))

	if _, ok := consumeInbound(t, msgBus, shortWait); ok {
		t.Fatal("answered, DM and old posts must not be triggered by edits")
	}

	// The bot's own streaming edits must not even cost a channel lookup.
	before := len(fs.callsTo("GET", "/api/v4/channels/c9"))
	ch.handlePostEditedEvent(ctx, editedEvent(t, mmPost{ID: "p4", CreateAt: now, UserID: testBotID, ChannelID: "c9", Message: "@goclawbot partial"}))
	if len(fs.callsTo("GET", "/api/v4/channels/c9")) != before {
		t.Fatal("own edits must be dropped before any API call")
	}
}

func TestMentionMatching(t *testing.T) {
	ch := &Channel{}
	ch.setBotIdentity(testBotID, testBotUsername)

	cases := []struct {
		text string
		want bool
	}{
		{"@goclawbot hi", true},
		{"hi @GoClawBot.", true},
		{"(@goclawbot)", true},
		{"@goclawbot2 hi", false},
		{"@goclawbot_x hi", false},
		{"mail a@goclawbot.com", false},
		{"no mention", false},
	}
	for _, tc := range cases {
		if got := ch.isBotMentioned(tc.text); got != tc.want {
			t.Errorf("isBotMentioned(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}

	if got := ch.stripBotMention("hey @goclawbot, check this"); got != "hey , check this" {
		t.Errorf("stripBotMention = %q", got)
	}
}
