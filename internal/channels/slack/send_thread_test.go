package slack

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	slackapi "github.com/slack-go/slack"
)

// slackCall records one chat.postMessage request as Slack would see it.
type slackCall struct {
	channel  string
	text     string
	threadTS string
}

// fakeSlackAPI is a minimal Slack Web API that answers chat.postMessage the way
// Slack does: a channel parameter that is not a channel ID (a composite
// "CHANNEL:thread:TS" local key, for instance) is rejected with
// channel_not_found.
type fakeSlackAPI struct {
	mu    sync.Mutex
	calls []slackCall
}

func (f *fakeSlackAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasSuffix(r.URL.Path, "chat.postMessage") {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	call := slackCall{
		channel:  r.FormValue("channel"),
		text:     r.FormValue("text"),
		threadTS: r.FormValue("thread_ts"),
	}
	f.mu.Lock()
	f.calls = append(f.calls, call)
	f.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	if strings.Contains(call.channel, ":") {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":    false,
			"error": "channel_not_found",
		})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":      true,
		"channel": call.channel,
		"ts":      "1700000000.000100",
	})
}

func (f *fakeSlackAPI) posted() []slackCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]slackCall(nil), f.calls...)
}

// newSendTestChannel builds a Channel wired to the fake Slack API, bypassing the
// Socket Mode start path that Send() does not need.
func newSendTestChannel(t *testing.T, api *fakeSlackAPI) *Channel {
	t.Helper()

	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)

	ch := &Channel{
		BaseChannel: channels.NewBaseChannel("slack-test", bus.New(), nil),
		api:         slackapi.New("xoxb-test-token", slackapi.OptionAPIURL(srv.URL+"/")),
	}
	ch.SetRunning(true)
	return ch
}

// A group session keyed by "CHANNEL:thread:TS" reaches Send() with that
// composite local key as ChatID when the reply does not go through the draft
// (streaming) transport, which parses the suffix with extractChannelID. The
// non-draft path must split it too: chat.postMessage needs the bare channel ID
// and the thread timestamp in its own field, otherwise Slack answers
// channel_not_found and the reply is lost.
func TestSend_CompositeThreadChatIDSplitsChannelAndThread(t *testing.T) {
	t.Parallel()

	api := &fakeSlackAPI{}
	ch := newSendTestChannel(t, api)

	err := ch.Send(context.Background(), bus.OutboundMessage{
		ChatID:  "C09CR4THNBA:thread:1784900153.376659",
		Content: "answer to the thread",
	})
	if err != nil {
		t.Fatalf("Send with a composite thread chat ID failed: %v", err)
	}

	calls := api.posted()
	if len(calls) != 1 {
		t.Fatalf("expected 1 chat.postMessage call, got %d", len(calls))
	}
	if calls[0].channel != "C09CR4THNBA" {
		t.Errorf("channel = %q, want %q (the thread suffix must not reach the API)", calls[0].channel, "C09CR4THNBA")
	}
	if calls[0].threadTS != "1784900153.376659" {
		t.Errorf("thread_ts = %q, want %q", calls[0].threadTS, "1784900153.376659")
	}
	if calls[0].text != "answer to the thread" {
		t.Errorf("text = %q, want the message content", calls[0].text)
	}
}

// A bare chat ID stays a channel with no thread, and an explicit
// message_thread_id still wins over anything the chat ID carries.
func TestSend_PlainChatIDUnchanged(t *testing.T) {
	t.Parallel()

	api := &fakeSlackAPI{}
	ch := newSendTestChannel(t, api)

	if err := ch.Send(context.Background(), bus.OutboundMessage{
		ChatID:  "C09CR4THNBA",
		Content: "plain reply",
	}); err != nil {
		t.Fatalf("Send with a plain chat ID failed: %v", err)
	}

	calls := api.posted()
	if len(calls) != 1 {
		t.Fatalf("expected 1 chat.postMessage call, got %d", len(calls))
	}
	if calls[0].channel != "C09CR4THNBA" {
		t.Errorf("channel = %q, want %q", calls[0].channel, "C09CR4THNBA")
	}
	if calls[0].threadTS != "" {
		t.Errorf("thread_ts = %q, want empty", calls[0].threadTS)
	}
}

func TestSend_MetadataThreadIDWins(t *testing.T) {
	t.Parallel()

	api := &fakeSlackAPI{}
	ch := newSendTestChannel(t, api)

	if err := ch.Send(context.Background(), bus.OutboundMessage{
		ChatID:   "C09CR4THNBA",
		Content:  "threaded reply",
		Metadata: map[string]string{"message_thread_id": "1784900153.376659"},
	}); err != nil {
		t.Fatalf("Send with metadata thread ID failed: %v", err)
	}

	calls := api.posted()
	if len(calls) != 1 {
		t.Fatalf("expected 1 chat.postMessage call, got %d", len(calls))
	}
	if calls[0].channel != "C09CR4THNBA" {
		t.Errorf("channel = %q, want %q", calls[0].channel, "C09CR4THNBA")
	}
	if calls[0].threadTS != "1784900153.376659" {
		t.Errorf("thread_ts = %q, want %q", calls[0].threadTS, "1784900153.376659")
	}
}
