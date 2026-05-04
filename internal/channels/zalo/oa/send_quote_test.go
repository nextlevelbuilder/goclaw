package oa

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
)

func TestBuildTextBody_NoQuote(t *testing.T) {
	t.Parallel()
	body := buildTextBody("u1", "hi", "")
	msg, _ := body["message"].(map[string]any)
	if msg == nil {
		t.Fatalf("message missing in body: %v", body)
	}
	if _, ok := msg["quote_message_id"]; ok {
		t.Fatalf("quote_message_id must be absent when empty, got body=%v", body)
	}
	if msg["text"] != "hi" {
		t.Errorf("message.text = %v, want hi", msg["text"])
	}
}

func TestBuildTextBody_WithQuote(t *testing.T) {
	t.Parallel()
	body := buildTextBody("u1", "hi", "qid42")
	msg, _ := body["message"].(map[string]any)
	if msg["quote_message_id"] != "qid42" {
		t.Fatalf("message.quote_message_id = %v, want qid42", msg["quote_message_id"])
	}
}

// hasQuote reads JSON request body and returns the quote_message_id (or "").
func extractQuoteID(t *testing.T, raw []byte) string {
	t.Helper()
	var b map[string]any
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatalf("unmarshal: %v\nraw=%s", err, raw)
	}
	msg, _ := b["message"].(map[string]any)
	q, _ := msg["quote_message_id"].(string)
	return q
}

func TestSendText_QuoteOnFirstChunkOnly(t *testing.T) {
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

	var bldr strings.Builder
	for range 10 {
		bldr.WriteString(strings.Repeat("a", 499))
		bldr.WriteString("\n\n")
	}
	long := bldr.String()
	_, err := c.SendText(context.Background(), "u1", long, "qid-first")
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if len(*captured) < 2 {
		t.Fatalf("captured %d, want >=2", len(*captured))
	}
	if got := extractQuoteID(t, (*captured)[0].body); got != "qid-first" {
		t.Errorf("chunk 1 quote = %q, want qid-first", got)
	}
	for i := 1; i < len(*captured); i++ {
		if got := extractQuoteID(t, (*captured)[i].body); got != "" {
			t.Errorf("chunk %d quote = %q, must be empty (continuation chunks unquoted)", i+1, got)
		}
	}
}

func TestSendText_NoQuoteWhenIDEmpty(t *testing.T) {
	t.Parallel()
	api, captured, _ := newAPIServer(t, apiServerOpts{
		messageReplies: []string{`{"error":0,"data":{"message_id":"m"}}`},
	})
	refresh, _ := newRefreshServer(t, "")
	c := newSendChannel(t, api, refresh, &fakeStore{})

	if _, err := c.SendText(context.Background(), "u1", "hi", ""); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if got := extractQuoteID(t, (*captured)[0].body); got != "" {
		t.Errorf("quote present without metadata: %q", got)
	}
}

// TestSendText_QuoteDroppedOnPayloadError: server rejects quoted body with
// -201 (FamilyPayload) → channel retries once without quote, succeeds.
func TestSendText_QuoteDroppedOnPayloadError(t *testing.T) {
	t.Parallel()
	var seenQuoted, seenUnquoted int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v3.0/oa/message/cs" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var b map[string]any
		_ = json.Unmarshal(raw, &b)
		msg, _ := b["message"].(map[string]any)
		w.Header().Set("Content-Type", "application/json")
		if _, ok := msg["quote_message_id"]; ok {
			atomic.AddInt32(&seenQuoted, 1)
			_, _ = w.Write([]byte(`{"error":-201,"message":"params invalid"}`))
			return
		}
		atomic.AddInt32(&seenUnquoted, 1)
		_, _ = w.Write([]byte(`{"error":0,"data":{"message_id":"m-no-quote"}}`))
	}))
	t.Cleanup(srv.Close)

	refresh, _ := newRefreshServer(t, "")
	c := newSendChannel(t, srv, refresh, &fakeStore{})
	mid, err := c.SendText(context.Background(), "u1", "hi", "qid-old")
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if mid != "m-no-quote" {
		t.Errorf("mid = %q, want m-no-quote", mid)
	}
	if g := atomic.LoadInt32(&seenQuoted); g != 1 {
		t.Errorf("seenQuoted = %d, want 1", g)
	}
	if g := atomic.LoadInt32(&seenUnquoted); g != 1 {
		t.Errorf("seenUnquoted = %d, want 1", g)
	}
}

// TestSendText_RateErrorPropagatesNoQuoteRetry: rate error (12010) is NOT a
// payload-family code; quote-fallback must not trigger.
func TestSendText_RateErrorPropagatesNoQuoteRetry(t *testing.T) {
	t.Parallel()
	var count int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&count, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":12010,"message":"per-user daily quota"}`))
	}))
	t.Cleanup(srv.Close)
	refresh, _ := newRefreshServer(t, "")
	c := newSendChannel(t, srv, refresh, &fakeStore{})
	_, err := c.SendText(context.Background(), "u1", "hi", "qid")
	if err == nil {
		t.Fatal("expected rate error")
	}
	if g := atomic.LoadInt32(&count); g != 1 {
		t.Errorf("hit count = %d, want 1 (no fallback retry)", g)
	}
}

// TestSendText_PayloadErrorWithoutQuote_NoRetry: a -201 with no quote set
// must NOT trigger fallback (no quote to drop).
func TestSendText_PayloadErrorWithoutQuote_NoRetry(t *testing.T) {
	t.Parallel()
	var count int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&count, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":-201,"message":"params invalid"}`))
	}))
	t.Cleanup(srv.Close)
	refresh, _ := newRefreshServer(t, "")
	c := newSendChannel(t, srv, refresh, &fakeStore{})
	_, err := c.SendText(context.Background(), "u1", "hi", "")
	if err == nil {
		t.Fatal("expected payload error to propagate when no quote set")
	}
	if g := atomic.LoadInt32(&count); g != 1 {
		t.Errorf("hit count = %d, want 1 (no retry without quote to drop)", g)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Errorf("err type = %T, want *APIError", err)
	}
}

// TestChannelSend_MetadataReplyToBecomesQuote: full Send path threads
// metadata["reply_to_message_id"] → message.quote_message_id.
func TestChannelSend_MetadataReplyToBecomesQuote(t *testing.T) {
	t.Parallel()
	api, captured, _ := newAPIServer(t, apiServerOpts{
		messageReplies: []string{`{"error":0,"data":{"message_id":"m"}}`},
	})
	refresh, _ := newRefreshServer(t, "")
	c := newSendChannel(t, api, refresh, &fakeStore{})

	err := c.Send(context.Background(), bus.OutboundMessage{
		ChatID:   "u1",
		Content:  "hello",
		Metadata: map[string]string{"reply_to_message_id": "qid-meta"},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(*captured) != 1 {
		t.Fatalf("captured %d, want 1", len(*captured))
	}
	if got := extractQuoteID(t, (*captured)[0].body); got != "qid-meta" {
		t.Errorf("quote_message_id = %q, want qid-meta", got)
	}
}

// TestChannelSend_TrailingTextAfterAttachmentDoesNotQuote: when both image
// and text ride together, the trailing text must NOT carry the quote.
func TestChannelSend_TrailingTextAfterAttachmentDoesNotQuote(t *testing.T) {
	t.Parallel()
	api, captured, _ := newAPIServer(t, apiServerOpts{
		uploadReply: `{"error":0,"data":{"attachment_id":"T"}}`,
		messageReplies: []string{
			`{"error":0,"data":{"message_id":"mid-img"}}`,
			`{"error":0,"data":{"message_id":"mid-txt"}}`,
		},
	})
	refresh, _ := newRefreshServer(t, "")
	c := newSendChannel(t, api, refresh, &fakeStore{})

	dir := t.TempDir()
	p := filepath.Join(dir, "x.png")
	if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	err := c.Send(context.Background(), bus.OutboundMessage{
		ChatID:   "u1",
		Content:  "trailing note",
		Media:    []bus.MediaAttachment{{URL: p, ContentType: "image/png"}},
		Metadata: map[string]string{"reply_to_message_id": "qid-meta"},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	// Find the text-only request (last /v3.0/oa/message/cs that has text, no attachment)
	var trailingBody []byte
	for _, r := range *captured {
		if r.path != "/v3.0/oa/message/cs" {
			continue
		}
		var b map[string]any
		_ = json.Unmarshal(r.body, &b)
		msg, _ := b["message"].(map[string]any)
		if _, isText := msg["text"]; isText {
			trailingBody = r.body
		}
	}
	if trailingBody == nil {
		t.Fatal("no trailing text request captured")
	}
	if got := extractQuoteID(t, trailingBody); got != "" {
		t.Errorf("trailing text quote = %q, must be empty", got)
	}
}

// TestSendText_AuthRetryThenPayloadFallback: -216 (auth) on first call
// triggers ForceRefresh+retry; second call hits -201 (payload) → quote
// dropped → third call succeeds. Total 3 message requests.
func TestSendText_AuthRetryThenPayloadFallback(t *testing.T) {
	t.Parallel()
	api, captured, _ := newAPIServer(t, apiServerOpts{
		messageReplies: []string{
			`{"error":-216,"message":"access_token invalid"}`,
			`{"error":-201,"message":"params invalid"}`,
			`{"error":0,"data":{"message_id":"mid-final"}}`,
		},
	})
	refresh, _ := newRefreshServer(t, "")
	c := newSendChannel(t, api, refresh, &fakeStore{})

	mid, err := c.SendText(context.Background(), "u1", "hi", "qid")
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if mid != "mid-final" {
		t.Errorf("mid = %q, want mid-final", mid)
	}
	if len(*captured) != 3 {
		t.Errorf("captured %d, want 3 (auth retry + payload fallback)", len(*captured))
	}
	// Assert quote present on first 2, absent on 3rd.
	if got := extractQuoteID(t, (*captured)[0].body); got != "qid" {
		t.Errorf("call 1 quote = %q, want qid", got)
	}
	if got := extractQuoteID(t, (*captured)[1].body); got != "qid" {
		t.Errorf("call 2 quote = %q, want qid (auth retry preserves quote)", got)
	}
	if got := extractQuoteID(t, (*captured)[2].body); got != "" {
		t.Errorf("call 3 quote = %q, want empty (payload fallback drops it)", got)
	}
}

func TestQuoteInboundOnDM_HonorsConfig(t *testing.T) {
	t.Parallel()
	off := false
	on := true
	cases := []struct {
		name string
		ptr  *bool
		want bool
	}{
		{"unset_defaults_off", nil, false},
		{"explicit_true", &on, true},
		{"explicit_false", &off, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := &Channel{cfg: config.ZaloOAConfig{QuoteUserMessage: tc.ptr}}
			if got := c.QuoteInboundOnDM(); got != tc.want {
				t.Errorf("QuoteInboundOnDM = %v, want %v", got, tc.want)
			}
		})
	}
}
