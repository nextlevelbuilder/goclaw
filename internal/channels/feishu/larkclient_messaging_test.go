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

// TestReplyMessage_InThread verifies the client hits the correct reply endpoint
// with reply_in_thread=true and a double-encoded JSON content string.
func TestReplyMessage_InThread(t *testing.T) {
	const rootMsgID = "om_root_1234567890"
	const wantContent = `{"text":"hello from thread"}`

	var gotMethod, gotPath, gotAuth, gotContentType string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == tokenEndpoint {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","tenant_access_token":"fake-token","expire":7200}`))
			return
		}
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"msg":"","data":{"message_id":"om_reply_abc"}}`))
	}))
	defer srv.Close()

	client := NewLarkClient("fake-app", "fake-secret", srv.URL)
	resp, err := client.ReplyMessage(context.Background(), rootMsgID, "text", wantContent, true)
	if err != nil {
		t.Fatalf("ReplyMessage returned error: %v", err)
	}
	if resp == nil || resp.MessageID != "om_reply_abc" {
		t.Fatalf("unexpected response: %+v", resp)
	}

	// Path must be the reply endpoint, not the new-message endpoint.
	wantPath := "/open-apis/im/v1/messages/" + rootMsgID + "/reply"
	if gotPath != wantPath {
		t.Errorf("path: got %q, want %q", gotPath, wantPath)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method: got %q, want POST", gotMethod)
	}
	if !strings.HasPrefix(gotAuth, "Bearer fake-token") {
		t.Errorf("authorization header missing or wrong: %q", gotAuth)
	}
	if !strings.HasPrefix(gotContentType, "application/json") {
		t.Errorf("content-type: got %q", gotContentType)
	}

	// Body assertions: content must be a JSON STRING (double-encoded), not an object.
	if gotContent, _ := gotBody["content"].(string); gotContent != wantContent {
		t.Errorf("content field: got %v (type %T), want string %q", gotBody["content"], gotBody["content"], wantContent)
	}
	if msgType, _ := gotBody["msg_type"].(string); msgType != "text" {
		t.Errorf("msg_type: got %v, want %q", gotBody["msg_type"], "text")
	}
	if rit, ok := gotBody["reply_in_thread"].(bool); !ok || !rit {
		t.Errorf("reply_in_thread: got %v, want true", gotBody["reply_in_thread"])
	}
}

// TestReplyMessage_APIError verifies that a non-zero Lark code surfaces as an error.
func TestReplyMessage_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == tokenEndpoint {
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","tenant_access_token":"t","expire":7200}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":230002,"msg":"message not found","data":{}}`))
	}))
	defer srv.Close()

	client := NewLarkClient("a", "s", srv.URL)
	if _, err := client.ReplyMessage(context.Background(), "om_missing", "text", `{"text":"x"}`, true); err == nil {
		t.Fatal("expected error on non-zero code, got nil")
	}
}
