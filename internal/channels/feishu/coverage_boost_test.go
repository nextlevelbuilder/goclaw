package feishu

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// --- sendText error path ---

func TestSendText_Error(t *testing.T) {
	srv := newSimpleMockServer(t, `{"code":10001,"msg":"error","data":{}}`)
	ch := &Channel{client: NewLarkClient("app", "secret", srv.URL)}

	err := ch.sendText(context.Background(), "oc_chat_1", "chat_id", "hello", "")
	if err == nil {
		t.Fatal("expected error for non-zero API code")
	}
}

// --- sendMarkdownCard error path ---

func TestSendMarkdownCard_Error(t *testing.T) {
	srv := newSimpleMockServer(t, `{"code":10001,"msg":"error","data":{}}`)
	ch := &Channel{client: NewLarkClient("app", "secret", srv.URL)}

	err := ch.sendMarkdownCard(context.Background(), "oc_chat_1", "chat_id", "text", "", nil)
	if err == nil {
		t.Fatal("expected error for non-zero API code")
	}
}

// --- sendChunkedText error propagation ---

func TestSendChunkedText_Error(t *testing.T) {
	srv := newSimpleMockServer(t, `{"code":10001,"msg":"error","data":{}}`)
	ch := &Channel{client: NewLarkClient("app", "secret", srv.URL)}

	err := ch.sendChunkedText(context.Background(), "oc_chat_1", "chat_id", "hello error test", 4000, "")
	if err == nil {
		t.Fatal("expected error propagated from sendText")
	}
}

// --- sendImage error path ---

func TestSendImage_Error(t *testing.T) {
	srv := newSimpleMockServer(t, `{"code":10001,"msg":"error","data":{}}`)
	ch := &Channel{client: NewLarkClient("app", "secret", srv.URL)}

	err := ch.sendImage(context.Background(), "oc_chat_1", "chat_id", "img_key_abc", "")
	if err == nil {
		t.Fatal("expected error for send image failure")
	}
}

// --- sendFile error path ---

func TestSendFile_Error(t *testing.T) {
	srv := newSimpleMockServer(t, `{"code":10001,"msg":"error","data":{}}`)
	ch := &Channel{client: NewLarkClient("app", "secret", srv.URL)}

	err := ch.sendFile(context.Background(), "oc_chat_1", "chat_id", "file_key_abc", "file", "")
	if err == nil {
		t.Fatal("expected error for send file failure")
	}
}

// --- OnReactionEvent active state (thinking/tool) ---

func TestOnReactionEvent_Thinking_AddsReaction(t *testing.T) {
	srv := newSimpleMockServer(t, `{"code":0,"msg":"ok","data":{"reaction_id":"rx_001"}}`)
	ch := &Channel{client: NewLarkClient("app", "secret", srv.URL)}
	ch.cfg.ReactionLevel = "full"

	err := ch.OnReactionEvent(context.Background(), "oc_chat_1", "om_msg_1", "thinking")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestOnReactionEvent_Thinking_AlreadyHasReaction(t *testing.T) {
	srv := newSimpleMockServer(t, `{"code":0,"msg":"ok","data":{"reaction_id":"rx_001"}}`)
	ch := &Channel{client: NewLarkClient("app", "secret", srv.URL)}
	ch.cfg.ReactionLevel = "full"

	// First call stores the reaction
	_ = ch.OnReactionEvent(context.Background(), "oc_chat_11", "om_msg_1", "thinking")

	// Second call with same chatID: already loaded, should skip
	err := ch.OnReactionEvent(context.Background(), "oc_chat_11", "om_msg_2", "thinking")
	if err != nil {
		t.Errorf("unexpected error on second thinking call: %v", err)
	}
}

// --- removeTypingReaction with stored reaction ---

func TestRemoveTypingReaction_WithStoredReaction(t *testing.T) {
	srv := newSimpleMockServer(t, `{"code":0,"msg":"ok","data":{}}`)
	ch := &Channel{client: NewLarkClient("app", "secret", srv.URL)}

	ch.reactions.Store("oc_chat_2", &reactionState{
		messageID:  "om_msg_2",
		reactionID: "rx_stored_1",
	})

	err := ch.removeTypingReaction(context.Background(), "oc_chat_2")
	if err != nil {
		t.Errorf("removeTypingReaction: %v", err)
	}
}

func TestRemoveTypingReaction_EmptyReactionID(t *testing.T) {
	ch := &Channel{}
	// Store state with empty reactionID — should be no-op after LoadAndDelete
	ch.reactions.Store("oc_chat_3", &reactionState{
		messageID:  "om_msg_3",
		reactionID: "",
	})

	err := ch.removeTypingReaction(context.Background(), "oc_chat_3")
	if err != nil {
		t.Errorf("unexpected error for empty reactionID: %v", err)
	}
}

// --- ClearReaction with stored reaction ---

func TestClearReaction_WithStoredReaction(t *testing.T) {
	srv := newSimpleMockServer(t, `{"code":0,"msg":"ok","data":{}}`)
	ch := &Channel{client: NewLarkClient("app", "secret", srv.URL)}

	ch.reactions.Store("oc_chat_4", &reactionState{
		messageID:  "om_msg_4",
		reactionID: "rx_to_clear",
	})

	err := ch.ClearReaction(context.Background(), "oc_chat_4", "")
	if err != nil {
		t.Errorf("ClearReaction: %v", err)
	}
}

// --- WebhookHandler returns handler in webhook mode with port=0 ---

func TestWebhookHandler_WebhookNoPort_ReturnsHandler(t *testing.T) {
	ch := &Channel{}
	ch.cfg.ConnectionMode = "webhook"
	ch.cfg.WebhookPort = 0
	ch.cfg.VerificationToken = "testtoken"

	path, handler := ch.WebhookHandler()
	if path == "" {
		t.Error("expected non-empty path for webhook mode with no port")
	}
	if handler == nil {
		t.Error("expected non-nil handler for webhook mode with no port")
	}
}

func TestWebhookHandler_CustomPath_ReturnsHandler(t *testing.T) {
	ch := &Channel{}
	ch.cfg.ConnectionMode = "webhook"
	ch.cfg.WebhookPort = 0
	ch.cfg.WebhookPath = "/custom/feishu"

	path, handler := ch.WebhookHandler()
	if path != "/custom/feishu" {
		t.Errorf("expected /custom/feishu, got %q", path)
	}
	if handler == nil {
		t.Error("expected non-nil handler")
	}
}

// --- sendMediaAttachment ---

// newDualResponseServer returns a server that gives resp1 on first non-token request,
// resp2 on subsequent requests.
func newDualResponseServer(t *testing.T, resp1, resp2 string) *httptest.Server {
	t.Helper()
	call := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == tokenEndpoint {
			w.Write([]byte(`{"code":0,"msg":"ok","tenant_access_token":"tok","expire":7200}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if call == 0 {
			w.Write([]byte(resp1))
		} else {
			w.Write([]byte(resp2))
		}
		call++
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSendMediaAttachment_Image(t *testing.T) {
	srv := newDualResponseServer(t,
		`{"code":0,"msg":"ok","data":{"image_key":"img_test_key"}}`,
		`{"code":0,"msg":"ok","data":{"message_id":"om_img_x"}}`,
	)
	ch := &Channel{client: NewLarkClient("app", "secret", srv.URL)}

	f, err := os.CreateTemp(t.TempDir(), "test_img_*.png")
	if err != nil {
		t.Fatalf("create temp: %v", err)
	}
	f.Write([]byte{0x89, 0x50, 0x4E, 0x47})
	f.Close()

	att := bus.MediaAttachment{URL: f.Name(), ContentType: "image/png"}
	if err := ch.sendMediaAttachment(context.Background(), "oc_chat", "chat_id", att, ""); err != nil {
		t.Errorf("sendMediaAttachment image: %v", err)
	}
}

func TestSendMediaAttachment_EmptyURL(t *testing.T) {
	ch := &Channel{}
	att := bus.MediaAttachment{URL: "", ContentType: "image/png"}
	if err := ch.sendMediaAttachment(context.Background(), "oc_chat", "chat_id", att, ""); err != nil {
		t.Errorf("expected no error for empty URL: %v", err)
	}
}

func TestSendMediaAttachment_DefaultFile(t *testing.T) {
	srv := newDualResponseServer(t,
		`{"code":0,"msg":"ok","data":{"file_key":"file_test_key"}}`,
		`{"code":0,"msg":"ok","data":{"message_id":"om_file_x"}}`,
	)
	ch := &Channel{client: NewLarkClient("app", "secret", srv.URL)}

	f, err := os.CreateTemp(t.TempDir(), "test_doc_*.pdf")
	if err != nil {
		t.Fatalf("create temp: %v", err)
	}
	f.Write([]byte("pdf data"))
	f.Close()

	att := bus.MediaAttachment{URL: f.Name(), ContentType: "application/pdf"}
	if err := ch.sendMediaAttachment(context.Background(), "oc_chat", "chat_id", att, ""); err != nil {
		t.Errorf("sendMediaAttachment file: %v", err)
	}
}

func TestSendMediaAttachment_Video(t *testing.T) {
	srv := newDualResponseServer(t,
		`{"code":0,"msg":"ok","data":{"file_key":"video_test_key"}}`,
		`{"code":0,"msg":"ok","data":{"message_id":"om_video_x"}}`,
	)
	ch := &Channel{client: NewLarkClient("app", "secret", srv.URL)}

	f, err := os.CreateTemp(t.TempDir(), "test_vid_*.mp4")
	if err != nil {
		t.Fatalf("create temp: %v", err)
	}
	f.Write([]byte("video data"))
	f.Close()

	att := bus.MediaAttachment{URL: f.Name(), ContentType: "video/mp4"}
	if err := ch.sendMediaAttachment(context.Background(), "oc_chat", "chat_id", att, ""); err != nil {
		t.Errorf("sendMediaAttachment video: %v", err)
	}
}

func TestSendMediaAttachment_FileNotFound(t *testing.T) {
	ch := &Channel{}
	att := bus.MediaAttachment{URL: "/nonexistent/path/file.pdf", ContentType: "application/pdf"}
	if err := ch.sendMediaAttachment(context.Background(), "oc_chat", "chat_id", att, ""); err == nil {
		t.Fatal("expected error for missing file")
	}
}
