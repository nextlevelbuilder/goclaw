package feishu

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// recordedRequest captures what the helper sent to the mocked Lark server
// so the test can assert which endpoint was hit.
type recordedRequest struct {
	method string
	path   string
	body   map[string]any
}

// newMockLarkServer returns an httptest server that records the first non-token
// request it receives. Subsequent requests still succeed with code=0 but are
// not recorded.
func newMockLarkServer(t *testing.T) (*httptest.Server, *recordedRequest) {
	t.Helper()
	rec := &recordedRequest{}
	var recorded bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == tokenEndpoint {
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","tenant_access_token":"tok","expire":7200}`))
			return
		}
		if !recorded {
			rec.method = r.Method
			rec.path = r.URL.Path
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &rec.body)
			recorded = true
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"msg":"","data":{"message_id":"om_out"}}`))
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

// TestSendText_InThread_RoutesToReplyEndpoint verifies that when replyTargetID
// is non-empty, sendText uses the reply endpoint with reply_in_thread=true.
func TestSendText_InThread_RoutesToReplyEndpoint(t *testing.T) {
	srv, rec := newMockLarkServer(t)

	ch := &Channel{client: NewLarkClient("app", "secret", srv.URL)}
	err := ch.sendText(context.Background(), "oc_chat_1", "chat_id", "hello in thread", "om_trigger_42")
	if err != nil {
		t.Fatalf("sendText returned error: %v", err)
	}

	wantPath := "/open-apis/im/v1/messages/om_trigger_42/reply"
	if rec.path != wantPath {
		t.Errorf("path: got %q, want %q", rec.path, wantPath)
	}
	if rec.method != http.MethodPost {
		t.Errorf("method: got %q, want POST", rec.method)
	}
	if rit, _ := rec.body["reply_in_thread"].(bool); !rit {
		t.Errorf("reply_in_thread: got %v, want true", rec.body["reply_in_thread"])
	}
	if mt, _ := rec.body["msg_type"].(string); mt != "post" {
		t.Errorf("msg_type: got %v, want %q", rec.body["msg_type"], "post")
	}
	// content is the Lark "post" structure encoded as a JSON string (double-encoded).
	content, ok := rec.body["content"].(string)
	if !ok || content == "" {
		t.Errorf("content: got %v, want non-empty string", rec.body["content"])
	}
	if !strings.Contains(content, "hello in thread") {
		t.Errorf("content missing text: %q", content)
	}
}

// TestSendText_NoThread_RoutesToNewMessageEndpoint verifies that when
// replyTargetID is empty, sendText falls through to the original SendMessage
// path (preserving existing UX for DMs and non-thread group messages).
func TestSendText_NoThread_RoutesToNewMessageEndpoint(t *testing.T) {
	srv, rec := newMockLarkServer(t)

	ch := &Channel{client: NewLarkClient("app", "secret", srv.URL)}
	err := ch.sendText(context.Background(), "oc_chat_1", "chat_id", "plain msg", "")
	if err != nil {
		t.Fatalf("sendText returned error: %v", err)
	}

	// New-message path — path is /open-apis/im/v1/messages (receive_id_type is a query param).
	wantPath := "/open-apis/im/v1/messages"
	if rec.path != wantPath {
		t.Errorf("path: got %q, want %q", rec.path, wantPath)
	}
	// No reply_in_thread field expected on new-message endpoint.
	if _, present := rec.body["reply_in_thread"]; present {
		t.Errorf("reply_in_thread should be absent on new-message endpoint, got %v", rec.body["reply_in_thread"])
	}
	if rid, _ := rec.body["receive_id"].(string); rid != "oc_chat_1" {
		t.Errorf("receive_id: got %v, want %q", rec.body["receive_id"], "oc_chat_1")
	}
}

// TestSendImage_InThread_RoutesToReplyEndpoint verifies image sends route to
// the reply endpoint when the message originated inside a thread.
func TestSendImage_InThread_RoutesToReplyEndpoint(t *testing.T) {
	srv, rec := newMockLarkServer(t)

	ch := &Channel{client: NewLarkClient("app", "secret", srv.URL)}
	err := ch.sendImage(context.Background(), "oc_chat_1", "chat_id", "img_key_123", "om_trigger_img")
	if err != nil {
		t.Fatalf("sendImage returned error: %v", err)
	}

	wantPath := "/open-apis/im/v1/messages/om_trigger_img/reply"
	if rec.path != wantPath {
		t.Errorf("path: got %q, want %q", rec.path, wantPath)
	}
	if mt, _ := rec.body["msg_type"].(string); mt != "image" {
		t.Errorf("msg_type: got %v, want %q", rec.body["msg_type"], "image")
	}
	if rit, _ := rec.body["reply_in_thread"].(bool); !rit {
		t.Errorf("reply_in_thread: got %v, want true", rec.body["reply_in_thread"])
	}
	if !strings.Contains(rec.body["content"].(string), "img_key_123") {
		t.Errorf("content missing image key: %v", rec.body["content"])
	}
}

// TestSendFile_InThread_RoutesToReplyEndpoint verifies file sends (documents +
// audio/video via "media" msg_type) route to reply endpoint inside threads.
func TestSendFile_InThread_RoutesToReplyEndpoint(t *testing.T) {
	srv, rec := newMockLarkServer(t)

	ch := &Channel{client: NewLarkClient("app", "secret", srv.URL)}
	err := ch.sendFile(context.Background(), "oc_chat_1", "chat_id", "file_key_abc", "file", "om_trigger_file")
	if err != nil {
		t.Fatalf("sendFile returned error: %v", err)
	}

	wantPath := "/open-apis/im/v1/messages/om_trigger_file/reply"
	if rec.path != wantPath {
		t.Errorf("path: got %q, want %q", rec.path, wantPath)
	}
	if mt, _ := rec.body["msg_type"].(string); mt != "file" {
		t.Errorf("msg_type: got %v, want %q", rec.body["msg_type"], "file")
	}
}

// TestSendMarkdownCard_InThread_RoutesToReplyEndpoint verifies card sends also
// respect thread routing.
func TestSendMarkdownCard_InThread_RoutesToReplyEndpoint(t *testing.T) {
	srv, rec := newMockLarkServer(t)

	ch := &Channel{client: NewLarkClient("app", "secret", srv.URL)}
	err := ch.sendMarkdownCard(context.Background(), "oc_chat_1", "chat_id", "**bold**", "om_trigger_99", nil)
	if err != nil {
		t.Fatalf("sendMarkdownCard returned error: %v", err)
	}

	wantPath := "/open-apis/im/v1/messages/om_trigger_99/reply"
	if rec.path != wantPath {
		t.Errorf("path: got %q, want %q", rec.path, wantPath)
	}
	if mt, _ := rec.body["msg_type"].(string); mt != "interactive" {
		t.Errorf("msg_type: got %v, want %q", rec.body["msg_type"], "interactive")
	}
	if rit, _ := rec.body["reply_in_thread"].(bool); !rit {
		t.Errorf("reply_in_thread: got %v, want true", rec.body["reply_in_thread"])
	}
}

// TestDeliverMessage_ReplyFailure_FallsBackToSendMessage verifies that when
// the reply endpoint returns a Lark error (e.g. thread root deleted), the
// helper falls through to the new-message endpoint so the user still receives
// the response even though thread placement is lost.
func TestDeliverMessage_ReplyFailure_FallsBackToSendMessage(t *testing.T) {
	var replyHits, sendHits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == tokenEndpoint {
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","tenant_access_token":"tok","expire":7200}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/reply") {
			replyHits++
			// 230002 = message not found per research report.
			_, _ = w.Write([]byte(`{"code":230002,"msg":"message not found","data":{}}`))
			return
		}
		sendHits++
		_, _ = w.Write([]byte(`{"code":0,"msg":"","data":{"message_id":"om_ok"}}`))
	}))
	defer srv.Close()

	ch := &Channel{client: NewLarkClient("app", "secret", srv.URL)}
	err := ch.deliverMessage(context.Background(), "oc_chat_1", "chat_id", "om_deleted_root", "text", `{"text":"hi"}`)
	if err != nil {
		t.Fatalf("deliverMessage should have succeeded via fallback, got: %v", err)
	}
	if replyHits != 1 {
		t.Errorf("reply hits: got %d, want 1", replyHits)
	}
	if sendHits != 1 {
		t.Errorf("send hits: got %d, want 1 (fallback)", sendHits)
	}
}

// TestParseMessageEvent_ThreadIDOnlyStampedForActualThreads guards H1: plain
// quote replies MUST NOT be stamped as thread replies. Only messages where
// Lark's event payload carries a non-empty `thread_id` should flow through
// the reply endpoint. This test asserts parseMessageEvent preserves the
// distinction between root_id (populated on any reply) and thread_id (only
// inside topic threads).
func TestParseMessageEvent_ThreadIDOnlyStampedForActualThreads(t *testing.T) {
	ch := &Channel{} // botOpenID empty — treat mentions as bot for test simplicity
	cases := []struct {
		name       string
		rootID     string
		threadID   string
		wantThread string
		wantRoot   string
	}{
		{"plain standalone message", "", "", "", ""},
		{"plain quote reply (root set, no thread)", "om_old_msg", "", "", "om_old_msg"},
		{"actual thread message", "om_thread_root", "om_thread_root", "om_thread_root", "om_thread_root"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ev := &MessageEvent{}
			ev.Event.Message.MessageID = "om_current"
			ev.Event.Message.ChatID = "oc_chat"
			ev.Event.Message.ChatType = "group"
			ev.Event.Message.MessageType = "text"
			ev.Event.Message.Content = `{"text":"hi"}`
			ev.Event.Message.RootID = tc.rootID
			ev.Event.Message.ThreadID = tc.threadID

			mc := ch.parseMessageEvent(ev)
			if mc == nil {
				t.Fatal("parseMessageEvent returned nil")
			}
			if mc.ThreadID != tc.wantThread {
				t.Errorf("ThreadID: got %q, want %q", mc.ThreadID, tc.wantThread)
			}
			if mc.RootID != tc.wantRoot {
				t.Errorf("RootID: got %q, want %q", mc.RootID, tc.wantRoot)
			}
		})
	}
}
