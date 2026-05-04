package oa

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
)

// newAPIServer returns an httptest server that captures every request in
// requests[] and replies with the body for that index. The server uses the
// path as a discriminator: /v3.0/oa/message/cs returns the next item from
// `messageReplies`; /v2.0/oa/upload/image and /upload/file return uploadReply.
type apiServerOpts struct {
	messageReplies []string // consumed FIFO per /message/cs call
	uploadReply    string   // returned for any /upload/* call
}

type capturedRequest struct {
	path        string
	query       string
	contentType string
	accessToken string // from the `access_token` header (Zalo's auth convention)
	body        []byte
	multipart   *capturedMultipart
}

type capturedMultipart struct {
	fileFieldName string
	fileName      string
	fileBytes     []byte
	fields        map[string]string
}

func newAPIServer(t *testing.T, opts apiServerOpts) (*httptest.Server, *[]capturedRequest, *int32) {
	t.Helper()
	var captured []capturedRequest
	var msgIdx int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := capturedRequest{
			path:        r.URL.Path,
			query:       r.URL.RawQuery,
			contentType: r.Header.Get("Content-Type"),
			accessToken: r.Header.Get("access_token"),
		}

		if strings.HasPrefix(req.contentType, "multipart/") {
			if err := r.ParseMultipartForm(10 << 20); err != nil {
				t.Errorf("ParseMultipartForm: %v", err)
			}
			cm := &capturedMultipart{fields: map[string]string{}}
			for k, v := range r.MultipartForm.Value {
				if len(v) > 0 {
					cm.fields[k] = v[0]
				}
			}
			for fieldName, fhs := range r.MultipartForm.File {
				if len(fhs) == 0 {
					continue
				}
				fh := fhs[0]
				cm.fileFieldName = fieldName
				cm.fileName = fh.Filename
				f, _ := fh.Open()
				cm.fileBytes, _ = io.ReadAll(f)
				_ = f.Close()
			}
			req.multipart = cm
		} else {
			req.body, _ = io.ReadAll(r.Body)
		}
		captured = append(captured, req)

		// Route response.
		if strings.HasPrefix(r.URL.Path, "/v2.0/oa/upload/") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(opts.uploadReply))
			return
		}
		if r.URL.Path == "/v3.0/oa/message/cs" {
			i := atomic.AddInt32(&msgIdx, 1) - 1
			if int(i) >= len(opts.messageReplies) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"error":-1,"message":"no canned reply"}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(opts.messageReplies[i]))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv, &captured, &msgIdx
}

// newSendChannel wires a Channel against the test server. Refresh server
// rotates tokens — test code that needs to assert token use can read
// captured query strings.
func newSendChannel(t *testing.T, apiSrv, refreshSrv *httptest.Server, fs *fakeStore) *Channel {
	t.Helper()
	creds := &ChannelCreds{
		AppID:        "app",
		SecretKey:    "key",
		AccessToken:  "AT-current",
		RefreshToken: "RT-current",
		ExpiresAt:    time.Now().Add(time.Hour),
	}
	cfg := config.ZaloOAConfig{}
	msgBus := bus.New()
	c, err := New("send_test", cfg, creds, fs, msgBus, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.SetInstanceID(uuid.New())
	c.client.apiBase = apiSrv.URL
	c.client.oauthBase = refreshSrv.URL
	return c
}

func TestSendText_HappyPath(t *testing.T) {
	t.Parallel()
	api, captured, _ := newAPIServer(t, apiServerOpts{
		messageReplies: []string{`{"error":0,"data":{"message_id":"mid-1"}}`},
	})
	refresh, _ := newRefreshServer(t, "")
	c := newSendChannel(t, api, refresh, &fakeStore{})

	mid, err := c.SendText(context.Background(), "user-1", "hello", "")
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if mid != "mid-1" {
		t.Errorf("message_id = %q, want mid-1", mid)
	}
	if len(*captured) != 1 {
		t.Fatalf("captured %d requests, want 1", len(*captured))
	}
	r := (*captured)[0]
	if r.path != "/v3.0/oa/message/cs" {
		t.Errorf("path = %q", r.path)
	}
	if r.accessToken != "AT-current" {
		t.Errorf("access_token header = %q, want AT-current", r.accessToken)
	}
	if !strings.HasPrefix(r.contentType, "application/json") {
		t.Errorf("content-type = %q", r.contentType)
	}
	var body map[string]any
	if err := json.Unmarshal(r.body, &body); err != nil {
		t.Fatalf("body unmarshal: %v", err)
	}
	rec, _ := body["recipient"].(map[string]any)
	msg, _ := body["message"].(map[string]any)
	if rec["user_id"] != "user-1" {
		t.Errorf("recipient.user_id = %v", rec["user_id"])
	}
	if msg["text"] != "hello" {
		t.Errorf("message.text = %v", msg["text"])
	}
}

func TestSendText_ChunksLongMessages(t *testing.T) {
	t.Parallel()
	api, captured, _ := newAPIServer(t, apiServerOpts{
		messageReplies: []string{
			`{"error":0,"data":{"message_id":"mid-1"}}`,
			`{"error":0,"data":{"message_id":"mid-2"}}`,
			`{"error":0,"data":{"message_id":"mid-3"}}`,
			`{"error":0,"data":{"message_id":"mid-4"}}`,
			`{"error":0,"data":{"message_id":"mid-5"}}`,
		},
	})
	refresh, _ := newRefreshServer(t, "")
	c := newSendChannel(t, api, refresh, &fakeStore{})

	// Build a body well over the 2000-rune cap with paragraph breaks every
	// ~500 runes so the chunker has natural cut points.
	var bldr strings.Builder
	for range 10 {
		bldr.WriteString(strings.Repeat("a", 499))
		bldr.WriteString("\n\n")
	}
	long := bldr.String()
	mid, err := c.SendText(context.Background(), "user-1", long, "")
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if len(*captured) < 2 {
		t.Fatalf("captured %d requests, want ≥2 chunks", len(*captured))
	}
	wantLastMID := fmt.Sprintf("mid-%d", len(*captured))
	if mid != wantLastMID {
		t.Errorf("message_id = %q, want last chunk %q", mid, wantLastMID)
	}
	for i, r := range *captured {
		var body map[string]any
		_ = json.Unmarshal(r.body, &body)
		msg, _ := body["message"].(map[string]any)
		text, _ := msg["text"].(string)
		if n := len([]rune(text)); n > 2000 {
			t.Errorf("chunk %d has %d runes, exceeds 2000-cap", i+1, n)
		}
	}
}

// TestSendText_AuthErrorRetriesOnce: first reply is auth error → ForceRefresh
// fires → second reply is OK. Send returns mid from second reply. Refresh
// server hit exactly once.
func TestSendText_AuthErrorRetriesOnce(t *testing.T) {
	t.Parallel()
	api, captured, _ := newAPIServer(t, apiServerOpts{
		messageReplies: []string{
			`{"error":-216,"message":"access_token invalid"}`,
			`{"error":0,"data":{"message_id":"mid-after-refresh"}}`,
		},
	})
	refresh, refreshCount := newRefreshServer(t, "")
	c := newSendChannel(t, api, refresh, &fakeStore{})

	mid, err := c.SendText(context.Background(), "user-1", "hi", "")
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if mid != "mid-after-refresh" {
		t.Errorf("mid = %q, want mid-after-refresh", mid)
	}
	if n := atomic.LoadInt32(refreshCount); n != 1 {
		t.Errorf("refresh hits = %d, want 1", n)
	}
	if len(*captured) != 2 {
		t.Fatalf("captured %d requests, want 2", len(*captured))
	}
	tok1 := (*captured)[0].accessToken
	tok2 := (*captured)[1].accessToken
	if tok1 == tok2 {
		t.Errorf("retry used same token %q (refresh should have rotated it)", tok1)
	}
}

// TestSendText_AuthErrorTwice_FailsCleanly: both attempts return auth error.
// Send returns the APIError without an infinite loop. ForceRefresh fires once.
func TestSendText_AuthErrorTwice_FailsCleanly(t *testing.T) {
	t.Parallel()
	api, captured, _ := newAPIServer(t, apiServerOpts{
		messageReplies: []string{
			`{"error":-216,"message":"access_token invalid"}`,
			`{"error":-216,"message":"access_token invalid"}`,
		},
	})
	refresh, _ := newRefreshServer(t, "")
	c := newSendChannel(t, api, refresh, &fakeStore{})

	_, err := c.SendText(context.Background(), "user-1", "hi", "")
	if err == nil {
		t.Fatal("expected error after second auth failure")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Errorf("err = %T %v, want *APIError", err, err)
	}
	if len(*captured) != 2 {
		t.Errorf("captured %d requests, want 2 (no infinite loop)", len(*captured))
	}
}

func TestSendText_NonAuthErrorNoRetry(t *testing.T) {
	t.Parallel()
	api, captured, _ := newAPIServer(t, apiServerOpts{
		messageReplies: []string{`{"error":-3,"message":"recipient not in 48h consultation window"}`},
	})
	refresh, _ := newRefreshServer(t, "")
	c := newSendChannel(t, api, refresh, &fakeStore{})

	_, err := c.SendText(context.Background(), "user-1", "hi", "")
	if err == nil {
		t.Fatal("expected error")
	}
	if len(*captured) != 1 {
		t.Errorf("captured %d requests, want 1 (non-auth must not retry)", len(*captured))
	}
}

func TestSendImage_UploadsThenAttaches(t *testing.T) {
	t.Parallel()
	api, captured, _ := newAPIServer(t, apiServerOpts{
		uploadReply:    `{"error":0,"data":{"attachment_id":"img-tok-abc"}}`,
		messageReplies: []string{`{"error":0,"data":{"message_id":"mid-img"}}`},
	})
	refresh, _ := newRefreshServer(t, "")
	c := newSendChannel(t, api, refresh, &fakeStore{})

	imgBytes := []byte("\x89PNG\r\n\x1a\nfake-image")
	mid, err := c.SendImage(context.Background(), "user-1", imgBytes, "image/png")
	if err != nil {
		t.Fatalf("SendImage: %v", err)
	}
	if mid != "mid-img" {
		t.Errorf("mid = %q", mid)
	}
	if len(*captured) != 2 {
		t.Fatalf("captured %d, want 2 (upload + send)", len(*captured))
	}
	upload := (*captured)[0]
	if upload.path != "/v2.0/oa/upload/image" {
		t.Errorf("upload path = %q", upload.path)
	}
	if upload.multipart == nil {
		t.Fatalf("upload not multipart")
	}
	if upload.multipart.fileFieldName != "file" {
		t.Errorf("upload form field = %q, want 'file'", upload.multipart.fileFieldName)
	}
	if string(upload.multipart.fileBytes) != string(imgBytes) {
		t.Errorf("upload bytes mismatch")
	}
	send := (*captured)[1]
	var body map[string]any
	_ = json.Unmarshal(send.body, &body)
	msg, _ := body["message"].(map[string]any)
	att, _ := msg["attachment"].(map[string]any)
	payload, _ := att["payload"].(map[string]any)
	// Zalo's template/media shape: {"type":"template","payload":{
	//   "template_type":"media","elements":[{"media_type":"image","attachment_id":"..."}]}}
	if att["type"] != "template" {
		t.Errorf("attachment.type = %v, want template", att["type"])
	}
	if payload["template_type"] != "media" {
		t.Errorf("payload.template_type = %v, want media", payload["template_type"])
	}
	elements, _ := payload["elements"].([]any)
	if len(elements) != 1 {
		t.Fatalf("elements = %v, want 1 entry", elements)
	}
	elem := elements[0].(map[string]any)
	if elem["media_type"] != "image" {
		t.Errorf("elements[0].media_type = %v, want image", elem["media_type"])
	}
	if elem["attachment_id"] != "img-tok-abc" {
		t.Errorf("elements[0].attachment_id = %v", elem["attachment_id"])
	}
}

func TestSendFile_UploadsThenAttaches(t *testing.T) {
	t.Parallel()
	api, captured, _ := newAPIServer(t, apiServerOpts{
		uploadReply:    `{"error":0,"data":{"attachment_id":"file-tok-xyz"}}`,
		messageReplies: []string{`{"error":0,"data":{"message_id":"mid-file"}}`},
	})
	refresh, _ := newRefreshServer(t, "")
	c := newSendChannel(t, api, refresh, &fakeStore{})

	mid, err := c.SendFile(context.Background(), "user-1", []byte("doc bytes"), "report.pdf")
	if err != nil {
		t.Fatalf("SendFile: %v", err)
	}
	if mid != "mid-file" {
		t.Errorf("mid = %q", mid)
	}
	upload := (*captured)[0]
	if upload.path != "/v2.0/oa/upload/file" {
		t.Errorf("upload path = %q", upload.path)
	}
	if upload.multipart.fileName != "report.pdf" {
		t.Errorf("filename = %q", upload.multipart.fileName)
	}
	send := (*captured)[1]
	var body map[string]any
	_ = json.Unmarshal(send.body, &body)
	msg, _ := body["message"].(map[string]any)
	att, _ := msg["attachment"].(map[string]any)
	if att["type"] != "file" {
		t.Errorf("attachment.type = %v", att["type"])
	}
}

// Channel.Send dispatch by Media[].ContentType.
func TestChannelSend_DispatchByContentType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		media       []bus.MediaAttachment
		content     string
		wantUpload  string // "" if no upload expected
		wantMsgPath string
	}{
		{
			name:        "no media → text",
			content:     "hello",
			wantMsgPath: "/v3.0/oa/message/cs",
		},
		{
			name:        "image/png → upload/image",
			media:       []bus.MediaAttachment{{ContentType: "image/png"}},
			wantUpload:  "/v2.0/oa/upload/image",
			wantMsgPath: "/v3.0/oa/message/cs",
		},
		{
			name:        "image/jpeg → upload/image",
			media:       []bus.MediaAttachment{{ContentType: "image/jpeg"}},
			wantUpload:  "/v2.0/oa/upload/image",
			wantMsgPath: "/v3.0/oa/message/cs",
		},
		{
			name:        "application/pdf → upload/file",
			media:       []bus.MediaAttachment{{ContentType: "application/pdf"}},
			wantUpload:  "/v2.0/oa/upload/file",
			wantMsgPath: "/v3.0/oa/message/cs",
		},
		{
			name:        "empty content-type with .png URL → upload/image",
			media:       []bus.MediaAttachment{{ContentType: ""}}, // URL .png filled in by test
			wantUpload:  "/v2.0/oa/upload/image",
			wantMsgPath: "/v3.0/oa/message/cs",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			api, captured, _ := newAPIServer(t, apiServerOpts{
				uploadReply:    `{"error":0,"data":{"attachment_id":"tok"}}`,
				messageReplies: []string{`{"error":0,"data":{"message_id":"mid"}}`},
			})
			refresh, _ := newRefreshServer(t, "")
			c := newSendChannel(t, api, refresh, &fakeStore{})

			// Materialize the media URL on disk if needed.
			media := tc.media
			if len(media) > 0 {
				dir := t.TempDir()
				ext := ".bin"
				if strings.HasPrefix(media[0].ContentType, "image/jpeg") {
					ext = ".jpg"
				} else if strings.HasPrefix(media[0].ContentType, "image/png") || media[0].ContentType == "" {
					ext = ".png"
				} else if media[0].ContentType == "application/pdf" {
					ext = ".pdf"
				}
				p := filepath.Join(dir, "blob"+ext)
				_ = os.WriteFile(p, []byte("x"), 0o600)
				media[0].URL = p
			}

			err := c.Send(context.Background(), bus.OutboundMessage{
				ChatID:  "user-1",
				Content: tc.content,
				Media:   media,
			})
			if err != nil {
				t.Fatalf("Send: %v", err)
			}

			gotUpload := false
			gotMsg := false
			for _, r := range *captured {
				if r.path == tc.wantUpload && tc.wantUpload != "" {
					gotUpload = true
				}
				if r.path == tc.wantMsgPath {
					gotMsg = true
				}
			}
			if tc.wantUpload != "" && !gotUpload {
				t.Errorf("expected upload to %s, captured=%v", tc.wantUpload, pathsOf(*captured))
			}
			if !gotMsg {
				t.Errorf("expected msg to %s, captured=%v", tc.wantMsgPath, pathsOf(*captured))
			}
		})
	}
}

func pathsOf(rs []capturedRequest) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.path
	}
	return out
}

func TestChannelSend_MediaTooLarge(t *testing.T) {
	t.Parallel()
	api, _, _ := newAPIServer(t, apiServerOpts{
		uploadReply: `{"error":0,"data":{"attachment_id":"tok"}}`,
	})
	refresh, _ := newRefreshServer(t, "")
	c := newSendChannel(t, api, refresh, &fakeStore{})

	// PDF >5MB — routes to SendFile path and must be rejected for exceeding
	// Zalo's /v2.0/oa/upload/file cap. (Image path auto-compresses, so the
	// size-limit test shifted to the file path where compression isn't
	// applicable.)
	dir := t.TempDir()
	p := filepath.Join(dir, "big.pdf")
	if err := os.WriteFile(p, make([]byte, 6<<20), 0o600); err != nil { // 6MB > 5MB Zalo cap
		t.Fatalf("write: %v", err)
	}

	err := c.Send(context.Background(), bus.OutboundMessage{
		ChatID: "u",
		Media:  []bus.MediaAttachment{{URL: p, ContentType: "application/pdf"}},
	})
	if err == nil {
		t.Fatal("expected size-limit error")
	}
	if !strings.Contains(err.Error(), "too large") && !strings.Contains(err.Error(), "exceeds") && !strings.Contains(err.Error(), "5MB") {
		t.Errorf("err message = %v, want 'too large'/'exceeds'/'5MB'", err)
	}
}

func TestChannelSend_StripsMarkdown(t *testing.T) {
	t.Parallel()
	api, captured, _ := newAPIServer(t, apiServerOpts{
		messageReplies: []string{`{"error":0,"data":{"message_id":"mid-md"}}`},
	})
	refresh, _ := newRefreshServer(t, "")
	c := newSendChannel(t, api, refresh, &fakeStore{})

	err := c.Send(context.Background(), bus.OutboundMessage{
		ChatID:  "u",
		Content: "**Bold** and __very emphatic__\n\n---\n\n# Header\n- bullet\n`code`",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(*captured) != 1 {
		t.Fatalf("captured %d, want 1", len(*captured))
	}
	var body map[string]any
	_ = json.Unmarshal((*captured)[0].body, &body)
	msg, _ := body["message"].(map[string]any)
	text, _ := msg["text"].(string)
	for _, banned := range []string{"**", "__", "---", "# Header", "`code`"} {
		if strings.Contains(text, banned) {
			t.Errorf("markdown not stripped: %q still contains %q", text, banned)
		}
	}
	for _, want := range []string{"Bold", "very emphatic", "Header", "bullet", "code"} {
		if !strings.Contains(text, want) {
			t.Errorf("content lost during strip: missing %q in %q", want, text)
		}
	}
}

func TestChannelSend_UnsupportedMIMEFallsBackToText(t *testing.T) {
	t.Parallel()
	api, captured, _ := newAPIServer(t, apiServerOpts{
		messageReplies: []string{`{"error":0,"data":{"message_id":"mid-fallback"}}`},
	})
	refresh, _ := newRefreshServer(t, "")
	c := newSendChannel(t, api, refresh, &fakeStore{})

	dir := t.TempDir()
	p := filepath.Join(dir, "report.xlsx")
	if err := os.WriteFile(p, []byte("PK\x03\x04xlsx"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	err := c.Send(context.Background(), bus.OutboundMessage{
		ChatID:  "u",
		Content: "Here is the summary.",
		Media: []bus.MediaAttachment{{
			URL:         p,
			ContentType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		}},
	})
	if err != nil {
		t.Fatalf("xlsx attachment should fall back to text, got err: %v", err)
	}
	if len(*captured) != 1 {
		t.Fatalf("captured %d requests, want exactly 1 (trailing text only)", len(*captured))
	}
	var body map[string]any
	_ = json.Unmarshal((*captured)[0].body, &body)
	msg, _ := body["message"].(map[string]any)
	text, _ := msg["text"].(string)
	if !strings.Contains(text, "Here is the summary.") {
		t.Errorf("trailing content dropped: %q", text)
	}
	if !strings.Contains(text, "report.xlsx") || !strings.Contains(text, "cannot be delivered") {
		t.Errorf("fallback note missing filename/explanation: %q", text)
	}
}

func TestChannelSend_EmptyChatID(t *testing.T) {
	t.Parallel()
	api, _, _ := newAPIServer(t, apiServerOpts{})
	refresh, _ := newRefreshServer(t, "")
	c := newSendChannel(t, api, refresh, &fakeStore{})

	err := c.Send(context.Background(), bus.OutboundMessage{Content: "hello"})
	if err == nil {
		t.Fatal("expected error for empty ChatID")
	}
}

// Compile-time guard: the response decoder must extract message_id from the
// nested "data" envelope, not from the top level.
func TestMessageResponse_ParseShape(t *testing.T) {
	t.Parallel()
	body := []byte(`{"error":0,"data":{"message_id":"M","recipient_id":"U"}}`)
	mid, err := parseMessageResponse(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if mid != "M" {
		t.Errorf("mid = %q, want M", mid)
	}
}

var _ = multipart.NewWriter // silence unused import in some test builds

// TestChannelSend_CaptionAndContentMerged: when both Caption + Content are
// set on a media message, both must ride in the trailing text msg.
func TestChannelSend_CaptionAndContentMerged(t *testing.T) {
	t.Parallel()
	api, captured, _ := newAPIServer(t, apiServerOpts{
		uploadReply:    `{"error":0,"data":{"attachment_id":"T"}}`,
		messageReplies: []string{`{"error":0,"data":{"message_id":"mid-img"}}`, `{"error":0,"data":{"message_id":"mid-txt"}}`},
	})
	refresh, _ := newRefreshServer(t, "")
	c := newSendChannel(t, api, refresh, &fakeStore{})

	dir := t.TempDir()
	p := filepath.Join(dir, "x.png")
	_ = os.WriteFile(p, []byte("x"), 0o600)

	err := c.Send(context.Background(), bus.OutboundMessage{
		ChatID:  "u",
		Content: "the body",
		Media:   []bus.MediaAttachment{{URL: p, ContentType: "image/png", Caption: "the caption"}},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	// Find the text-message request (last /v3.0/oa/message/cs after upload + first message/cs).
	var textBody string
	for _, r := range *captured {
		if r.path == "/v3.0/oa/message/cs" {
			var b map[string]any
			_ = json.Unmarshal(r.body, &b)
			if msg, ok := b["message"].(map[string]any); ok {
				if t, ok := msg["text"].(string); ok {
					textBody = t // last one wins (the trailing text)
				}
			}
		}
	}
	if !strings.Contains(textBody, "the caption") || !strings.Contains(textBody, "the body") {
		t.Errorf("trailing text = %q, want both 'the caption' and 'the body'", textBody)
	}
}

// TestChannelSend_PartialSendOnTrailingTextFailure: attachment succeeds,
// trailing text fails → returns ErrPartialSend.
func TestChannelSend_PartialSendOnTrailingTextFailure(t *testing.T) {
	t.Parallel()
	api, _, _ := newAPIServer(t, apiServerOpts{
		uploadReply:    `{"error":0,"data":{"attachment_id":"T"}}`,
		messageReplies: []string{`{"error":0,"data":{"message_id":"mid-img"}}`, `{"error":-99,"message":"blocked"}`},
	})
	refresh, _ := newRefreshServer(t, "")
	c := newSendChannel(t, api, refresh, &fakeStore{})

	dir := t.TempDir()
	p := filepath.Join(dir, "x.png")
	_ = os.WriteFile(p, []byte("x"), 0o600)

	err := c.Send(context.Background(), bus.OutboundMessage{
		ChatID:  "u",
		Content: "follow-up text",
		Media:   []bus.MediaAttachment{{URL: p, ContentType: "image/png"}},
	})
	if err == nil {
		t.Fatal("expected ErrPartialSend")
	}
	if !errors.Is(err, ErrPartialSend) {
		t.Errorf("err = %v, want ErrPartialSend", err)
	}
}
