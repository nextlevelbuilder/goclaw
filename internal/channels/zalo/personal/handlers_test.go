package personal

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/personal/protocol"
	"github.com/nextlevelbuilder/goclaw/internal/config"
)

func TestExtractContentAndMediaWithQuote_TextQuote(t *testing.T) {
	current := textContent("@Tue Linh Pm - Cppai Task nay cung giao cho Hao luon.")
	quote := &protocol.TQuote{
		FromD: "Duc Nguyen CPP",
		Msg:   "Tao task giao cho Hao.",
	}

	got, media := extractContentAndMediaWithQuote(current, quote)
	if len(media) != 0 {
		t.Fatalf("media = %v, want none", media)
	}
	if !strings.Contains(got, "[Replying to Duc Nguyen CPP]\nTao task giao cho Hao.\n[/Replying]") {
		t.Fatalf("missing quote context: %q", got)
	}
	if !strings.Contains(got, "@Tue Linh Pm - Cppai Task nay cung giao cho Hao luon.") {
		t.Fatalf("missing current reply text: %q", got)
	}
}

func TestFormatQuoteContext_AttachmentQuote(t *testing.T) {
	var attach protocol.QuoteAttachment
	if err := attach.UnmarshalJSON([]byte(`"{\"title\":\"brief.png\",\"href\":\"https://f20-zpc.zdn.vn/jpg/abc123\"}"`)); err != nil {
		t.Fatal(err)
	}
	quote := &protocol.TQuote{
		FromD:  "Duc Nguyen CPP",
		Attach: attach,
	}

	got := formatQuoteContext(quote)
	if !strings.Contains(got, "[Replying to Duc Nguyen CPP]") {
		t.Fatalf("missing quote sender: %q", got)
	}
	if !strings.Contains(got, "[Quoted image: brief.png]") {
		t.Fatalf("missing quote attachment text: %q", got)
	}
}

func TestExtractContentAndMediaWithQuote_NestedStyleReply(t *testing.T) {
	current := textContent("@Tue Linh Pm - Cppai Task nay cung giao cho Hao luon.")
	quote := &protocol.TQuote{
		FromD: "Tue Linh Pm - Cppai",
		Msg:   "@Duong Anh Hao tao task follow Zalo reply context.",
	}

	got, media := extractContentAndMediaWithQuote(current, quote)
	if len(media) != 0 {
		t.Fatalf("media = %v, want none", media)
	}
	for _, want := range []string{
		"@Duong Anh Hao tao task follow Zalo reply context.",
		"@Tue Linh Pm - Cppai Task nay cung giao cho Hao luon.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
}

func TestZaloReplyContext_BuildsMultiLevelChainFromCache(t *testing.T) {
	ch := newTestChannel(t, config.ZaloPersonalConfig{DMPolicy: "open", GroupPolicy: "open"})
	threadID := "group-1"

	ch.rememberReplyContext(threadID, "Alice", protocol.TMessage{
		MsgID:    "msg-a",
		CliMsgID: "cli-a",
		UIDFrom:  "u-a",
	}, "original")
	ch.rememberReplyContext(threadID, "Bob", protocol.TMessage{
		MsgID:    "msg-b",
		CliMsgID: "cli-b",
		UIDFrom:  "u-b",
		Quote: &protocol.TQuote{
			OwnerID:     "u-a",
			CliMsgID:    "cli-a",
			GlobalMsgID: "msg-a",
		},
	}, "reply 1")
	ch.rememberReplyContext(threadID, "Carol", protocol.TMessage{
		MsgID:    "msg-c",
		CliMsgID: "cli-c",
		UIDFrom:  "u-c",
		Quote: &protocol.TQuote{
			OwnerID:     "u-b",
			CliMsgID:    "cli-b",
			GlobalMsgID: "msg-b",
		},
	}, "reply 2")

	got := ch.buildReplyContext(threadID, &protocol.TQuote{
		OwnerID:     "u-c",
		CliMsgID:    "cli-c",
		GlobalMsgID: "msg-c",
		FromD:       "Carol",
		Msg:         "fallback quote text",
	})

	for _, want := range []string{"original", "reply 1", "reply 2"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
	assertContainsInOrder(t, got, "original", "reply 1", "reply 2")
	if strings.Contains(got, "fallback quote text") {
		t.Fatalf("used direct fallback despite cache hit: %q", got)
	}
}

func TestHandleDM_RejectedMessageDoesNotDownloadAttachment(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte("image bytes"))
	}))
	defer server.Close()

	msgBus := bus.New()
	ch, err := New(config.ZaloPersonalConfig{
		AllowFrom: config.FlexibleStringSlice{"allowed-user"},
		DMPolicy:  "allowlist",
	}, msgBus, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ch.handleDM(protocol.NewUserMessage("bot", protocol.TMessage{
		MsgID:   "denied-attachment",
		UIDFrom: "denied-user",
		IDTo:    "bot",
		Content: attachmentContent(t, server.URL+"/secret.png"),
	}))

	if got := hits.Load(); got != 0 {
		t.Fatalf("attachment endpoint hit %d times, want 0", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if got, ok := msgBus.ConsumeInbound(ctx); ok {
		t.Fatalf("unexpected inbound for rejected message: %+v", got)
	}
}

func textContent(text string) protocol.Content {
	return protocol.Content{String: &text}
}

func attachmentContent(t *testing.T, href string) protocol.Content {
	t.Helper()
	raw := `{"title":"secret.png","href":` + strconvQuote(href) + `}`
	var content protocol.Content
	if err := json.Unmarshal([]byte(raw), &content); err != nil {
		t.Fatal(err)
	}
	return content
}

func newTestChannel(t *testing.T, cfg config.ZaloPersonalConfig) *Channel {
	t.Helper()
	ch, err := New(cfg, bus.New(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return ch
}

func assertContainsInOrder(t *testing.T, text string, values ...string) {
	t.Helper()
	last := -1
	for _, value := range values {
		idx := strings.Index(text, value)
		if idx < 0 {
			t.Fatalf("missing %q in %q", value, text)
		}
		if idx <= last {
			t.Fatalf("%q appears out of order in %q", value, text)
		}
		last = idx
	}
}

func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
