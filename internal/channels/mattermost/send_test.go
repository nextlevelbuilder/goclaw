package mattermost

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
)

func TestSend_EditsPlaceholderThenPostsRemainingChunks(t *testing.T) {
	fs := newFakeServer(t)
	ch, _ := newTestChannel(t, fs, nil)
	ch.placeholders.Store("c1:thread:r1", "ph1")

	long := strings.Repeat("a", maxMessageLen-10) + "\n\n" + strings.Repeat("b", 100)
	err := ch.Send(context.Background(), bus.OutboundMessage{
		ChatID:  "c1",
		Content: long,
		Metadata: map[string]string{
			"placeholder_key":   "c1:thread:r1",
			"message_thread_id": "r1",
		},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	patches := fs.callsTo("PUT", "/api/v4/posts/ph1/patch")
	if len(patches) != 1 || !strings.HasPrefix(patches[0].Body["message"].(string), "aaa") {
		t.Fatalf("first chunk must replace the placeholder, got %+v", patches)
	}
	posts := fs.callsTo("POST", "/api/v4/posts")
	if len(posts) != 1 || posts[0].Body["root_id"] != "r1" || !strings.HasPrefix(posts[0].Body["message"].(string), "bbb") {
		t.Fatalf("second chunk must be posted in the thread, got %+v", posts)
	}
	if _, ok := ch.placeholders.Load("c1:thread:r1"); ok {
		t.Fatal("placeholder must be consumed")
	}
}

func TestSend_InterimMessageToThreadLocalKey(t *testing.T) {
	fs := newFakeServer(t)
	ch, _ := newTestChannel(t, fs, nil)
	ch.startTyping("c1:thread:r1", "c1", "r1")

	// Block replies, quick-acks and progress carry the local key as ChatID
	// and no placeholder_key.
	for _, content := range []string{"Looking into it…", "Found the logs."} {
		err := ch.Send(context.Background(), bus.OutboundMessage{
			ChatID:   "c1:thread:r1",
			Content:  content,
			Metadata: map[string]string{"local_key": "c1:thread:r1"},
		})
		if err != nil {
			t.Fatalf("Send(%q): %v", content, err)
		}
	}

	posts := fs.callsTo("POST", "/api/v4/posts")
	if len(posts) != 2 {
		t.Fatalf("expected two posts, got %+v", posts)
	}
	for _, p := range posts {
		if p.Body["channel_id"] != "c1" || p.Body["root_id"] != "r1" {
			t.Fatalf("interim post must go to channel c1, thread r1, got %+v", p.Body)
		}
	}
	if _, ok := ch.typingCtrls.Load("c1:thread:r1"); !ok {
		t.Fatal("interim messages must not stop the typing indicator")
	}
}

func TestSend_EmptyContentDeletesPlaceholder(t *testing.T) {
	fs := newFakeServer(t)
	ch, _ := newTestChannel(t, fs, nil)
	ch.placeholders.Store("dm1", "ph1")

	if err := ch.Send(context.Background(), bus.OutboundMessage{ChatID: "dm1"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(fs.callsTo("DELETE", "/api/v4/posts/ph1")) != 1 {
		t.Fatal("NO_REPLY must delete the placeholder")
	}
	if len(fs.callsTo("POST", "/api/v4/posts")) != 0 {
		t.Fatal("NO_REPLY must not post anything")
	}
}

func TestSend_PlaceholderUpdateKeepsPlaceholder(t *testing.T) {
	fs := newFakeServer(t)
	ch, _ := newTestChannel(t, fs, nil)
	ch.placeholders.Store("dm1", "ph1")

	err := ch.Send(context.Background(), bus.OutboundMessage{
		ChatID:   "dm1",
		Content:  "Retrying...",
		Metadata: map[string]string{"placeholder_update": "true"},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(fs.callsTo("PUT", "/api/v4/posts/ph1/patch")) != 1 {
		t.Fatal("placeholder update must patch the placeholder")
	}
	if _, ok := ch.placeholders.Load("dm1"); !ok {
		t.Fatal("placeholder update must keep the placeholder for the final reply")
	}
}

func TestSend_UploadsMediaAsAttachmentPost(t *testing.T) {
	fs := newFakeServer(t)
	ch, _ := newTestChannel(t, fs, nil)
	ch.placeholders.Store("dm1", "ph1")

	path := filepath.Join(t.TempDir(), "report.csv")
	if err := os.WriteFile(path, []byte("a,b\n1,2\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := ch.Send(context.Background(), bus.OutboundMessage{
		ChatID: "dm1",
		Media:  []bus.MediaAttachment{{URL: path, Caption: "Q3 report"}},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	uploads := fs.callsTo("POST", "/api/v4/files")
	if len(uploads) != 1 || uploads[0].Body["filename"] != "report.csv" || uploads[0].Body["channel_id"] != "dm1" {
		t.Fatalf("unexpected uploads: %+v", uploads)
	}
	posts := fs.callsTo("POST", "/api/v4/posts")
	if len(posts) != 1 || posts[0].Body["message"] != "Q3 report" {
		t.Fatalf("expected one attachment post with caption, got %+v", posts)
	}
	if ids, _ := posts[0].Body["file_ids"].([]any); len(ids) != 1 {
		t.Fatalf("attachment post must carry the uploaded file, got %+v", posts[0].Body)
	}
	if len(fs.callsTo("DELETE", "/api/v4/posts/ph1")) != 1 {
		t.Fatal("media-only reply must remove the placeholder")
	}
}

func TestSend_RequiresRunningChannel(t *testing.T) {
	fs := newFakeServer(t)
	ch, _ := newTestChannel(t, fs, nil)
	ch.SetRunning(false)

	if err := ch.Send(context.Background(), bus.OutboundMessage{ChatID: "dm1", Content: "x"}); err == nil {
		t.Fatal("expected error when channel is not running")
	}
}

func TestStream_EditsPlaceholderAndHandsItBack(t *testing.T) {
	fs := newFakeServer(t)
	ch, _ := newTestChannel(t, fs, nil)
	ch.placeholders.Store("dm1", "ph1")

	stream, err := ch.CreateStream(context.Background(), "dm1", true)
	if err != nil {
		t.Fatalf("CreateStream: %v", err)
	}
	stream.Update(context.Background(), "partial answer")
	if len(fs.callsTo("PUT", "/api/v4/posts/ph1/patch")) != 1 {
		t.Fatal("stream update must edit the placeholder")
	}

	ch.placeholders.Delete("dm1")
	ch.FinalizeStream(context.Background(), "dm1", stream)
	if pID, ok := ch.placeholders.Load("dm1"); !ok || pID != "ph1" {
		t.Fatal("FinalizeStream must hand the placeholder back to Send")
	}
}

func TestTyping_StartsInThreadAndStopsOnFinalSend(t *testing.T) {
	fs := newFakeServer(t)
	ch, msgBus := newTestChannel(t, fs, nil)
	ctx := context.Background()

	ch.handlePostedEvent(ctx, postedEvent(t, mmPost{ID: "p1", UserID: "u1", ChannelID: "c1", Message: "@goclawbot hi"}, "O", nil))
	if _, ok := consumeInbound(t, msgBus, time.Second); !ok {
		t.Fatal("expected inbound message")
	}
	typingCalls := fs.callsTo("POST", "/api/v4/users/"+testBotID+"/typing")
	if len(typingCalls) != 1 || typingCalls[0].Body["channel_id"] != "c1" || typingCalls[0].Body["parent_id"] != "p1" {
		t.Fatalf("typing must be shown in the reply thread, got %+v", typingCalls)
	}

	meta := map[string]string{"placeholder_key": "c1:thread:p1", "message_thread_id": "p1"}
	retry := map[string]string{"placeholder_key": "c1:thread:p1", "placeholder_update": "true"}
	if err := ch.Send(ctx, bus.OutboundMessage{ChatID: "c1", Content: "Retrying...", Metadata: retry}); err != nil {
		t.Fatal(err)
	}
	if _, ok := ch.typingCtrls.Load("c1:thread:p1"); !ok {
		t.Fatal("placeholder update must keep the typing indicator")
	}
	if err := ch.Send(ctx, bus.OutboundMessage{ChatID: "c1", Content: "hello", Metadata: meta}); err != nil {
		t.Fatal(err)
	}
	if _, ok := ch.typingCtrls.Load("c1:thread:p1"); ok {
		t.Fatal("final send must stop the typing indicator")
	}
}

func TestReactions_FollowLevelAndClear(t *testing.T) {
	fs := newFakeServer(t)
	off, _ := newTestChannel(t, fs, nil)
	if err := off.OnReactionEvent(context.Background(), "dm1", "p1", "thinking"); err != nil {
		t.Fatal(err)
	}
	if len(fs.callsTo("POST", "/api/v4/reactions")) != 0 {
		t.Fatal("reaction_level off must not react")
	}

	ch, _ := newTestChannel(t, fs, func(c *config.MattermostConfig) { c.ReactionLevel = "full" })
	if err := ch.OnReactionEvent(context.Background(), "dm1", "p1", "thinking"); err != nil {
		t.Fatal(err)
	}
	adds := fs.callsTo("POST", "/api/v4/reactions")
	if len(adds) != 1 || adds[0].Body["emoji_name"] != "thinking_face" || adds[0].Body["user_id"] != testBotID {
		t.Fatalf("unexpected reaction: %+v", adds)
	}

	if err := ch.ClearReaction(context.Background(), "dm1", "p1"); err != nil {
		t.Fatal(err)
	}
	if len(fs.callsTo("DELETE", "/api/v4/users/"+testBotID+"/posts/p1/reactions/thinking_face")) != 1 {
		t.Fatal("ClearReaction must remove the status emoji")
	}
}
