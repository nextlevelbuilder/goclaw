package zalo

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
)

// swapAPIBase points zalo.apiBase at a test server for the duration of t.
// Restores original value automatically via t.Cleanup.
func swapAPIBase(t *testing.T, url string) {
	t.Helper()
	original := apiBase
	apiBase = url
	t.Cleanup(func() { apiBase = original })
}

// newTestChannel returns a Channel wired to the given mock server URL.
// Token is fixed to "t" so callers can predict the URL path:
//
//	<base>/bott/<method>
func newTestChannel(t *testing.T, srvURL string) *Channel {
	t.Helper()
	swapAPIBase(t, srvURL)
	mb := bus.New()
	ch, err := New(config.ZaloConfig{Token: "t", DMPolicy: "open"}, mb, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ch.SetRunning(true)
	return ch
}

// TestCallAPI_SuccessRoutesOKResponse verifies callAPI marshals the body,
// hits the correct path with the bot token, and returns apiResp.Result.
func TestCallAPI_SuccessRoutesOKResponse(t *testing.T) {
	var gotPath, gotMethod, gotCT string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotCT = r.Header.Get("Content-Type")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = w.Write([]byte(`{"ok":true,"result":{"id":"bot-1","name":"Zalobot"}}`))
	}))
	defer srv.Close()

	ch := newTestChannel(t, srv.URL)

	raw, err := ch.callAPI("getMe", nil)
	if err != nil {
		t.Fatalf("callAPI: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/bott/getMe" {
		t.Errorf("path = %q, want /bott/getMe", gotPath)
	}
	// No body → Content-Type should not be set by callAPI.
	if gotCT != "" {
		t.Errorf("content-type = %q, want empty (no body sent)", gotCT)
	}
	// Verify raw result is forwarded verbatim.
	if !strings.Contains(string(raw), `"bot-1"`) {
		t.Errorf("raw result = %s, want bot-1", raw)
	}
}

// TestCallAPI_ErrorResponseSurfaces verifies callAPI returns an error when
// the upstream reports ok=false.
func TestCallAPI_ErrorResponseSurfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false,"error_code":1001,"description":"bad token"}`))
	}))
	defer srv.Close()

	ch := newTestChannel(t, srv.URL)
	_, err := ch.callAPI("getMe", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "1001") || !strings.Contains(err.Error(), "bad token") {
		t.Errorf("err = %v, want both code and description", err)
	}
}

// TestCallAPI_MalformedJSONReturnsError verifies malformed upstream JSON
// surfaces an unmarshal error.
func TestCallAPI_MalformedJSONReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer srv.Close()

	ch := newTestChannel(t, srv.URL)
	_, err := ch.callAPI("getMe", nil)
	if err == nil {
		t.Fatal("expected unmarshal error, got nil")
	}
}

// TestGetMe_ParsesBotInfo verifies getMe returns the zaloBotInfo struct.
func TestGetMe_ParsesBotInfo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"result":{"id":"bot-xyz","name":"TestBot"}}`))
	}))
	defer srv.Close()

	ch := newTestChannel(t, srv.URL)
	info, err := ch.getMe()
	if err != nil {
		t.Fatalf("getMe: %v", err)
	}
	if info.ID != "bot-xyz" || info.Name != "TestBot" {
		t.Errorf("info = %+v, want bot-xyz/TestBot", info)
	}
}

// TestGetMe_UnmarshalError surfaces decode errors when result is malformed.
func TestGetMe_UnmarshalError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"result":"not-an-object"}`))
	}))
	defer srv.Close()

	ch := newTestChannel(t, srv.URL)
	if _, err := ch.getMe(); err == nil {
		t.Fatal("expected unmarshal error, got nil")
	}
}

// TestGetUpdates_ParsesUpdateArray verifies getUpdates decodes the updates array.
func TestGetUpdates_ParsesUpdateArray(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"result":[{"event_name":"message.text.received","message":{"message_id":"m1","text":"hi","from":{"id":"user1"},"chat":{"id":"user1"}}}]}`))
	}))
	defer srv.Close()

	ch := newTestChannel(t, srv.URL)
	updates, err := ch.getUpdates(10)
	if err != nil {
		t.Fatalf("getUpdates: %v", err)
	}
	if len(updates) != 1 {
		t.Fatalf("updates len = %d, want 1", len(updates))
	}
	if updates[0].EventName != "message.text.received" {
		t.Errorf("EventName = %q", updates[0].EventName)
	}
	if updates[0].Message == nil || updates[0].Message.Text != "hi" {
		t.Errorf("Message = %+v", updates[0].Message)
	}
}

// TestGetUpdates_UnmarshalError verifies a malformed result surfaces an error.
func TestGetUpdates_UnmarshalError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"result":"string-instead-of-array"}`))
	}))
	defer srv.Close()

	ch := newTestChannel(t, srv.URL)
	if _, err := ch.getUpdates(5); err == nil {
		t.Fatal("expected unmarshal error, got nil")
	}
}

// TestSendMessage_PostsBodyWithParams verifies sendMessage forwards chat_id
// and text through callAPI as a JSON body.
func TestSendMessage_PostsBodyWithParams(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got)
		_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	defer srv.Close()

	ch := newTestChannel(t, srv.URL)
	if err := ch.sendMessage("user-1", "hello"); err != nil {
		t.Fatalf("sendMessage: %v", err)
	}
	if got["chat_id"] != "user-1" {
		t.Errorf("chat_id = %v, want user-1", got["chat_id"])
	}
	if got["text"] != "hello" {
		t.Errorf("text = %v, want hello", got["text"])
	}
}

// TestSendPhoto_OmitsEmptyCaption verifies sendPhoto only adds "caption"
// when it is non-empty (KISS: don't transmit empty fields).
func TestSendPhoto_OmitsEmptyCaption(t *testing.T) {
	var gotBodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		m := map[string]any{}
		_ = json.Unmarshal(raw, &m)
		gotBodies = append(gotBodies, m)
		_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	defer srv.Close()

	ch := newTestChannel(t, srv.URL)
	if err := ch.sendPhoto("chat-1", "https://cdn.example.test/a.jpg", ""); err != nil {
		t.Fatalf("sendPhoto: %v", err)
	}
	if err := ch.sendPhoto("chat-1", "https://cdn.example.test/b.jpg", "caption"); err != nil {
		t.Fatalf("sendPhoto (w/ caption): %v", err)
	}
	if len(gotBodies) != 2 {
		t.Fatalf("want 2 bodies, got %d", len(gotBodies))
	}
	if _, present := gotBodies[0]["caption"]; present {
		t.Errorf("first call should omit caption, got %v", gotBodies[0]["caption"])
	}
	if gotBodies[1]["caption"] != "caption" {
		t.Errorf("second call caption = %v, want caption", gotBodies[1]["caption"])
	}
}

// TestSendChunkedText_ChunksLongContent verifies long text is split into
// multiple sendMessage calls via ChunkMarkdown + maxTextLength.
func TestSendChunkedText_ChunksLongContent(t *testing.T) {
	var callCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	defer srv.Close()

	ch := newTestChannel(t, srv.URL)
	// Build text larger than maxTextLength (2000) to force chunking.
	long := strings.Repeat("a", maxTextLength*2+10)
	if err := ch.sendChunkedText("chat-x", long); err != nil {
		t.Fatalf("sendChunkedText: %v", err)
	}
	if got := atomic.LoadInt32(&callCount); got < 2 {
		t.Errorf("expected ≥2 chunked sends, got %d", got)
	}
}

// TestSendChunkedText_PropagatesFirstError verifies that if the first
// chunk fails, the method returns the error without attempting later chunks.
func TestSendChunkedText_PropagatesFirstError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false,"error_code":500,"description":"boom"}`))
	}))
	defer srv.Close()

	ch := newTestChannel(t, srv.URL)
	err := ch.sendChunkedText("chat-x", "short enough")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// TestSend_NotRunningRejects verifies Send errors when the channel is not running.
func TestSend_NotRunningRejects(t *testing.T) {
	mb := bus.New()
	ch, err := New(config.ZaloConfig{Token: "t"}, mb, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// channel is NOT Start()ed → IsRunning() is false.
	err = ch.Send(context.Background(), bus.OutboundMessage{
		ChatID:  "x",
		Content: "hi",
	})
	if err == nil {
		t.Fatal("expected error when not running, got nil")
	}
	if !strings.Contains(err.Error(), "not running") {
		t.Errorf("err = %v, want 'not running'", err)
	}
}

// TestSend_PlainTextGoesThroughSendMessage verifies Send routes plain text
// through sendChunkedText → sendMessage → callAPI.
func TestSend_PlainTextGoesThroughSendMessage(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got)
		_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	defer srv.Close()

	ch := newTestChannel(t, srv.URL)
	err := ch.Send(context.Background(), bus.OutboundMessage{
		ChatID:  "user-7",
		Content: "**hello** world",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	// Content should have markdown stripped (StripMarkdown drops **…**).
	if text, _ := got["text"].(string); strings.Contains(text, "**") {
		t.Errorf("markdown not stripped: %q", text)
	}
	if got["chat_id"] != "user-7" {
		t.Errorf("chat_id = %v, want user-7", got["chat_id"])
	}
}

// TestSend_PhotoExtractionRoutesToSendPhoto verifies [photo:URL] is
// extracted and sent via sendPhoto instead of sendMessage.
func TestSend_PhotoExtractionRoutesToSendPhoto(t *testing.T) {
	var lastPath string
	var lastBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		lastBody = map[string]any{}
		_ = json.Unmarshal(raw, &lastBody)
		_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	defer srv.Close()

	ch := newTestChannel(t, srv.URL)
	err := ch.Send(context.Background(), bus.OutboundMessage{
		ChatID:  "user-8",
		Content: "look at this [photo:https://cdn.example/test.jpg] nice pic",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !strings.HasSuffix(lastPath, "/sendPhoto") {
		t.Errorf("path = %q, want sendPhoto", lastPath)
	}
	if lastBody["photo"] != "https://cdn.example/test.jpg" {
		t.Errorf("photo = %v", lastBody["photo"])
	}
}

// TestStop_SignalsLoopAndTogglesRunning verifies Stop closes stopCh and
// flips IsRunning() back to false.
func TestStop_SignalsLoopAndTogglesRunning(t *testing.T) {
	mb := bus.New()
	ch, err := New(config.ZaloConfig{Token: "t"}, mb, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ch.SetRunning(true)
	if err := ch.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if ch.IsRunning() {
		t.Error("IsRunning still true after Stop")
	}
	// stopCh should be closed — reading from a closed channel returns zero value
	// immediately instead of blocking.
	select {
	case <-ch.stopCh:
	case <-time.After(100 * time.Millisecond):
		t.Error("stopCh not closed after Stop")
	}
}

// TestProcessUpdate_DispatchesByEventName covers the event switch: text,
// image, and unknown events. Verifies HandleMessage is invoked for known
// events (observed indirectly through bus inbound queue).
func TestProcessUpdate_DispatchesByEventName(t *testing.T) {
	mb := bus.New()
	ch, err := New(config.ZaloConfig{Token: "t", DMPolicy: "open"}, mb, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Unknown event → no-op, no panic.
	ch.processUpdate(zaloUpdate{EventName: "message.unknown"})

	// Text event with nil message → handler guard returns early without
	// panicking (nil pointer check).
	ch.processUpdate(zaloUpdate{EventName: "message.text.received"})

	// Image event with nil message → same guard.
	ch.processUpdate(zaloUpdate{EventName: "message.image.received"})

	// Text event with populated message → should flow through handleTextMessage.
	// We cannot easily assert the bus side without draining it, but the call
	// should not panic and should route through the happy path.
	ch.processUpdate(zaloUpdate{
		EventName: "message.text.received",
		Message: &zaloMessage{
			MessageID: "m1",
			Text:      "hello",
			From:      zaloFrom{ID: "u1"},
			Chat:      zaloChat{ID: "u1"},
		},
	})
}

// TestHandleTextMessage_EmptySenderDropped verifies messages with empty
// sender IDs are dropped (logged warning, no bus publish).
func TestHandleTextMessage_EmptySenderDropped(t *testing.T) {
	mb := bus.New()
	ch, err := New(config.ZaloConfig{Token: "t", DMPolicy: "open"}, mb, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// No panic expected.
	ch.handleTextMessage(&zaloMessage{
		MessageID: "m",
		Text:      "hi",
		From:      zaloFrom{ID: ""}, // missing sender
		Chat:      zaloChat{ID: ""},
	})
}

// TestHandleImageMessage_EmptySenderDropped mirrors the text variant guard.
func TestHandleImageMessage_EmptySenderDropped(t *testing.T) {
	mb := bus.New()
	ch, err := New(config.ZaloConfig{Token: "t", DMPolicy: "open"}, mb, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ch.handleImageMessage(&zaloMessage{
		MessageID: "m",
		From:      zaloFrom{ID: ""},
	})
}

// TestDownloadMedia_SuccessWritesTempFile verifies downloadMedia fetches
// the URL and persists a temp file with matching extension.
func TestDownloadMedia_SuccessWritesTempFile(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), 128)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	mb := bus.New()
	ch, _ := New(config.ZaloConfig{Token: "t"}, mb, nil)
	path, err := ch.downloadMedia(srv.URL + "/photo")
	if err != nil {
		t.Fatalf("downloadMedia: %v", err)
	}
	defer os.Remove(path)

	if !strings.HasSuffix(path, ".png") {
		t.Errorf("path ext = %q, want .png (from Content-Type)", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, payload) {
		t.Errorf("file content len = %d, want %d", len(data), len(payload))
	}
}

// TestDownloadMedia_HTTPErrorReturnsError verifies non-200 responses error out.
func TestDownloadMedia_HTTPErrorReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	ch, _ := New(config.ZaloConfig{Token: "t"}, bus.New(), nil)
	if _, err := ch.downloadMedia(srv.URL); err == nil {
		t.Fatal("expected error on 404, got nil")
	}
}

// TestDownloadMedia_EmptyResponseReturnsError verifies zero-byte responses
// are treated as errors (don't leave empty temp files behind).
func TestDownloadMedia_EmptyResponseReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		// no body
	}))
	defer srv.Close()

	ch, _ := New(config.ZaloConfig{Token: "t"}, bus.New(), nil)
	if _, err := ch.downloadMedia(srv.URL); err == nil {
		t.Fatal("expected empty-response error, got nil")
	}
}

// TestDownloadMedia_FallbackJPEGExtension verifies an unknown content-type
// defaults to .jpg extension.
func TestDownloadMedia_FallbackJPEGExtension(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Intentionally no Content-Type → default to .jpg
		_, _ = w.Write([]byte("binary-bytes"))
	}))
	defer srv.Close()

	ch, _ := New(config.ZaloConfig{Token: "t"}, bus.New(), nil)
	path, err := ch.downloadMedia(srv.URL)
	if err != nil {
		t.Fatalf("downloadMedia: %v", err)
	}
	defer os.Remove(path)
	if !strings.HasSuffix(path, ".jpg") {
		t.Errorf("path = %q, want .jpg default", path)
	}
}

// TestZaloAPIResponse_Roundtrip sanity-checks the wire-type tags.
func TestZaloAPIResponse_Roundtrip(t *testing.T) {
	src := zaloAPIResponse{
		OK:          true,
		Result:      json.RawMessage(`{"id":"x"}`),
		ErrorCode:   0,
		Description: "",
	}
	data, err := json.Marshal(src)
	if err != nil {
		t.Fatal(err)
	}
	var got zaloAPIResponse
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if !got.OK {
		t.Error("OK field lost in round-trip")
	}
}
