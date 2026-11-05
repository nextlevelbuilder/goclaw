package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	slackapi "github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
)

// inboundWait bounds how long a test waits for the handler to publish inbound.
const inboundWait = 2 * time.Second

// updateCall records one chat.update request as Slack would see it.
type updateCall struct {
	channel string
	ts      string
	text    string
}

// threadedSlackAPI is a minimal Slack Web API for the concurrent-mention case:
// every chat.postMessage gets its own message timestamp, and chat.update reports
// which message the channel handler tried to edit.
type threadedSlackAPI struct {
	mu      sync.Mutex
	posts   []slackCall
	postTS  []string
	updates []updateCall
	nextTS  int
}

func (f *threadedSlackAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	w.Header().Set("Content-Type", "application/json")

	f.mu.Lock()
	defer f.mu.Unlock()

	switch {
	case strings.HasSuffix(r.URL.Path, "chat.postMessage"):
		f.nextTS++
		ts := fmt.Sprintf("1784900300.%06d", f.nextTS)
		f.posts = append(f.posts, slackCall{
			channel:  r.FormValue("channel"),
			text:     r.FormValue("text"),
			threadTS: r.FormValue("thread_ts"),
		})
		f.postTS = append(f.postTS, ts)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":      true,
			"channel": r.FormValue("channel"),
			"ts":      ts,
		})
	case strings.HasSuffix(r.URL.Path, "chat.update"):
		f.updates = append(f.updates, updateCall{
			channel: r.FormValue("channel"),
			ts:      r.FormValue("ts"),
			text:    r.FormValue("text"),
		})
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "ts": r.FormValue("ts")})
	default:
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}
}

func (f *threadedSlackAPI) snapshot() (posts []slackCall, postTS []string, updates []updateCall) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]slackCall(nil), f.posts...),
		append([]string(nil), f.postTS...),
		append([]updateCall(nil), f.updates...)
}

// newMentionTestChannel wires a Channel to the fake Slack API without the Socket
// Mode start path, keeping the mention gate on so the handlers run end to end.
func newMentionTestChannel(t *testing.T, api http.Handler) (*Channel, *bus.MessageBus) {
	t.Helper()

	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)

	msgBus := bus.New()
	t.Cleanup(msgBus.Close)

	base := channels.NewBaseChannel("slack-test", msgBus, nil)
	base.SetRequireMention(true)
	base.SetGroupHistory(channels.NewPendingHistory())
	base.SetHistoryLimit(channels.DefaultGroupHistoryLimit)

	ch := &Channel{
		BaseChannel: base,
		api:         slackapi.New("xoxb-test-token", slackapi.OptionAPIURL(srv.URL+"/")),
		config:      config.SlackConfig{GroupPolicy: "open"},
		botUserID:   "UBOT",
		userCache:   make(map[string]cachedUser),
	}
	ch.SetRunning(true)
	return ch, msgBus
}

// mentionInbound publishes one top-level @mention in a channel and returns the
// inbound message the gateway would have received for it.
func mentionInbound(t *testing.T, ch *Channel, msgBus *bus.MessageBus, channelID, user, ts, text string) bus.InboundMessage {
	t.Helper()

	ch.handleMessage(&slackevents.MessageEvent{
		Channel:     channelID,
		ChannelType: "channel",
		User:        user,
		Text:        text,
		TimeStamp:   ts,
	})

	ctx, cancel := context.WithTimeout(context.Background(), inboundWait)
	defer cancel()
	in, ok := msgBus.ConsumeInbound(ctx)
	if !ok {
		t.Fatalf("no inbound message published for mention %q", text)
	}
	return in
}

// Two @mentions sent to the same channel start two different threads, so each
// gets its own "Thinking..." placeholder. Group sessions are keyed per channel,
// so keying the placeholder by the session key alone makes the second mention
// overwrite the first: both answers are then edited into the newest thread and
// one placeholder is orphaned (issue #1476).
func TestConcurrentTopLevelMentionsKeepTheirOwnPlaceholder(t *testing.T) {
	t.Parallel()

	api := &threadedSlackAPI{}
	ch, msgBus := newMentionTestChannel(t, api)

	first := mentionInbound(t, ch, msgBus, "C09CR4THNBA", "U1", "1784900100.000001", "<@UBOT> first question")
	second := mentionInbound(t, ch, msgBus, "C09CR4THNBA", "U2", "1784900200.000002", "<@UBOT> second question")

	posts, postTS, _ := api.snapshot()
	if len(posts) != 2 {
		t.Fatalf("expected 2 placeholders, got %d", len(posts))
	}
	if posts[0].threadTS != "1784900100.000001" || posts[1].threadTS != "1784900200.000002" {
		t.Fatalf("placeholders went to threads %q and %q, want each mention's own thread",
			posts[0].threadTS, posts[1].threadTS)
	}

	// Answer the second mention first: that is when the overwritten entry is
	// read back and the first answer lands in the wrong thread.
	for _, send := range []struct {
		in      bus.InboundMessage
		content string
	}{
		{second, "answer two"},
		{first, "answer one"},
	} {
		if err := ch.Send(context.Background(), bus.OutboundMessage{
			ChatID:   send.in.ChatID,
			Content:  send.content,
			Metadata: send.in.Metadata,
		}); err != nil {
			t.Fatalf("Send(%q) failed: %v", send.content, err)
		}
	}

	_, _, updates := api.snapshot()
	if len(updates) != 2 {
		t.Fatalf("expected 2 chat.update calls, got %d", len(updates))
	}

	edited := map[string]string{}
	for _, u := range updates {
		edited[u.ts] = u.text
	}
	if got := edited[postTS[0]]; got != "answer one" {
		t.Errorf("placeholder in thread %q was edited with %q, want %q",
			posts[0].threadTS, got, "answer one")
	}
	if got := edited[postTS[1]]; got != "answer two" {
		t.Errorf("placeholder in thread %q was edited with %q, want %q",
			posts[1].threadTS, got, "answer two")
	}
}

func TestThreadPlaceholderKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		localKey string
		threadTS string
		want     string
	}{
		{"top-level mention keys by its reply thread", "C09CR4THNBA", "1784900100.000001", "C09CR4THNBA:thread:1784900100.000001"},
		{"channel message without a reply thread", "C09CR4THNBA", "", "C09CR4THNBA"},
		{"thread reply is already thread-scoped", "C09CR4THNBA:thread:1784900100.000001", "1784900100.000001", "C09CR4THNBA:thread:1784900100.000001"},
		{"dm without a thread", "D09CR4THNBA", "", "D09CR4THNBA"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := threadPlaceholderKey(tt.localKey, tt.threadTS); got != tt.want {
				t.Errorf("threadPlaceholderKey(%q, %q) = %q, want %q", tt.localKey, tt.threadTS, got, tt.want)
			}
		})
	}
}

// With one mention in flight the streaming transport still finds its placeholder
// under the channel-level local key, so the "Thinking..." placeholder keeps being
// reused as the preview target.
func TestSingleMentionKeepsStreamingPreview(t *testing.T) {
	t.Parallel()

	api := &threadedSlackAPI{}
	ch, msgBus := newMentionTestChannel(t, api)

	in := mentionInbound(t, ch, msgBus, "C09CR4THNBA", "U1", "1784900100.000001", "<@UBOT> question")
	_, postTS, _ := api.snapshot()
	if len(postTS) != 1 {
		t.Fatalf("expected 1 placeholder, got %d", len(postTS))
	}

	stream, err := ch.CreateStream(context.Background(), in.ChatID, false)
	if err != nil {
		t.Fatalf("CreateStream failed: %v", err)
	}
	ss, ok := stream.(*slackStream)
	if !ok {
		t.Fatalf("unexpected stream type %T", stream)
	}
	if ss.msgTS != postTS[0] {
		t.Errorf("stream placeholder = %q, want %q", ss.msgTS, postTS[0])
	}

	ch.FinalizeStream(context.Background(), in.ChatID, stream)
	if err := ch.Send(context.Background(), bus.OutboundMessage{
		ChatID:   in.ChatID,
		Content:  "the answer",
		Metadata: in.Metadata,
	}); err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	_, _, updates := api.snapshot()
	if len(updates) != 1 {
		t.Fatalf("expected 1 chat.update call, got %d", len(updates))
	}
	if updates[0].ts != postTS[0] || updates[0].text != "the answer" {
		t.Errorf("chat.update targeted %q with %q, want %q with %q",
			updates[0].ts, updates[0].text, postTS[0], "the answer")
	}
}

// While two mentions are in flight the channel-level lookup is ambiguous, so the
// stream must not pick either placeholder: skipping the preview is preferable to
// editing text into the other mention's thread.
func TestConcurrentMentionsSkipAmbiguousStreamPreview(t *testing.T) {
	t.Parallel()

	api := &threadedSlackAPI{}
	ch, msgBus := newMentionTestChannel(t, api)

	mentionInbound(t, ch, msgBus, "C09CR4THNBA", "U1", "1784900100.000001", "<@UBOT> first question")
	mentionInbound(t, ch, msgBus, "C09CR4THNBA", "U2", "1784900200.000002", "<@UBOT> second question")

	stream, err := ch.CreateStream(context.Background(), "C09CR4THNBA", false)
	if err != nil {
		t.Fatalf("CreateStream failed: %v", err)
	}
	ss, ok := stream.(*slackStream)
	if !ok {
		t.Fatalf("unexpected stream type %T", stream)
	}
	if ss.msgTS != "" {
		t.Errorf("stream picked placeholder %q while two mentions are in flight, want no preview target", ss.msgTS)
	}
}
