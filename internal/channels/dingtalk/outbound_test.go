package dingtalk

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// capturedRequest records one request the stub server saw.
type capturedRequest struct {
	Path string
	Body map[string]any
}

// stubDingTalk serves both the token endpoint and the proactive robot endpoints,
// recording every non-token call.
type stubDingTalk struct {
	mu       sync.Mutex
	requests []capturedRequest
	srv      *httptest.Server
}

func newStubDingTalk(t *testing.T) *stubDingTalk {
	t.Helper()
	s := &stubDingTalk{}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1.0/oauth2/accessToken" {
			fmt.Fprint(w, `{"accessToken":"tok","expireIn":7200}`)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)

		s.mu.Lock()
		s.requests = append(s.requests, capturedRequest{Path: r.URL.Path, Body: body})
		s.mu.Unlock()

		fmt.Fprint(w, `{"processQueryKey":"pqk-1"}`)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *stubDingTalk) calls() []capturedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]capturedRequest(nil), s.requests...)
}

// stubWebhook serves a session webhook and records the bodies it receives.
type stubWebhook struct {
	mu       sync.Mutex
	bodies   []map[string]any
	status   int
	errcode  int
	srv      *httptest.Server
	hitCount int
}

func newStubWebhook(t *testing.T) *stubWebhook {
	t.Helper()
	s := &stubWebhook{status: http.StatusOK}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)

		s.mu.Lock()
		s.bodies = append(s.bodies, body)
		s.hitCount++
		status, errcode := s.status, s.errcode
		s.mu.Unlock()

		w.WriteHeader(status)
		if errcode != 0 {
			fmt.Fprintf(w, `{"errcode":%d,"errmsg":"nope"}`, errcode)
			return
		}
		fmt.Fprint(w, `{"errcode":0}`)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *stubWebhook) hits() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hitCount
}

func (s *stubWebhook) received() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]map[string]any(nil), s.bodies...)
}

// sendingChannel returns a running channel whose API client points at api.
func sendingChannel(t *testing.T, cfg Config, api *stubDingTalk) *Channel {
	t.Helper()
	ch, _ := newTestChannelCfg(t, cfg)
	ch.client.apiBase = api.srv.URL
	if err := ch.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = ch.Stop(context.Background()) })
	return ch
}

func baseCfg() Config {
	return Config{ClientID: "robot-1", ClientSecret: "s", DMPolicy: "open", GroupPolicy: "open"}
}

func TestSend_UsesLiveSessionWebhook(t *testing.T) {
	api := newStubDingTalk(t)
	hook := newStubWebhook(t)
	ch := sendingChannel(t, baseCfg(), api)

	err := ch.Send(context.Background(), bus.OutboundMessage{
		ChatID:  "staff-1",
		Content: "hello",
		Metadata: map[string]string{
			"dingtalk_chat_type":          "direct",
			"dingtalk_session_webhook":    hook.srv.URL,
			"dingtalk_webhook_expires_at": fmt.Sprint(time.Now().Add(time.Hour).UnixMilli()),
		},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if hook.hits() != 1 {
		t.Errorf("webhook hits = %d, want 1", hook.hits())
	}
	if len(api.calls()) != 0 {
		t.Errorf("proactive API was called despite a live webhook: %+v", api.calls())
	}
}

// The webhook is per-message and short-lived. A long agent run outlives it, and
// the reply must still land.
func TestSend_FallsBackWhenWebhookExpired(t *testing.T) {
	api := newStubDingTalk(t)
	hook := newStubWebhook(t)
	ch := sendingChannel(t, baseCfg(), api)

	expired := time.Now().Add(-time.Minute)

	err := ch.Send(context.Background(), bus.OutboundMessage{
		ChatID:  "staff-1",
		Content: "hello",
		Metadata: map[string]string{
			"dingtalk_chat_type":          "direct",
			"dingtalk_session_webhook":    hook.srv.URL,
			"dingtalk_webhook_expires_at": fmt.Sprint(expired.UnixMilli()),
		},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if hook.hits() != 0 {
		t.Errorf("expired webhook was used %d times", hook.hits())
	}
	calls := api.calls()
	if len(calls) != 1 || calls[0].Path != pathRobotSendToUser {
		t.Fatalf("want one call to %s, got %+v", pathRobotSendToUser, calls)
	}
}

// A webhook can be revoked before its stamped expiry. Falling back turns a
// dropped reply into a delivered one.
func TestSend_FallsBackWhenWebhookRejects(t *testing.T) {
	api := newStubDingTalk(t)
	hook := newStubWebhook(t)
	hook.errcode = 300001
	ch := sendingChannel(t, baseCfg(), api)

	err := ch.Send(context.Background(), bus.OutboundMessage{
		ChatID:   "staff-1",
		Content:  "hello",
		Metadata: map[string]string{"dingtalk_chat_type": "direct", "dingtalk_session_webhook": hook.srv.URL},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if hook.hits() != 1 {
		t.Errorf("webhook hits = %d, want 1 (tried once)", hook.hits())
	}
	if len(api.calls()) != 1 {
		t.Fatalf("want proactive fallback, got %+v", api.calls())
	}
}

// Cron- and delegate-initiated messages have no webhook at all.
func TestSend_NoWebhookGoesProactive(t *testing.T) {
	api := newStubDingTalk(t)
	ch := sendingChannel(t, baseCfg(), api)

	err := ch.Send(context.Background(), bus.OutboundMessage{
		ChatID:   "staff-1",
		Content:  "scheduled report",
		Metadata: map[string]string{"dingtalk_chat_type": "direct"},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	calls := api.calls()
	if len(calls) != 1 || calls[0].Path != pathRobotSendToUser {
		t.Fatalf("calls = %+v", calls)
	}
	if got := calls[0].Body["robotCode"]; got != "robot-1" {
		t.Errorf("robotCode = %v, want the clientId", got)
	}
	users, _ := calls[0].Body["userIds"].([]any)
	if len(users) != 1 || users[0] != "staff-1" {
		t.Errorf("userIds = %v", calls[0].Body["userIds"])
	}
}

// group_session_scope=group_sender suffixes the ChatID, so the bare conversation
// id must come from metadata or the proactive call addresses a nonexistent group.
func TestSend_GroupUsesConversationIDNotChatID(t *testing.T) {
	api := newStubDingTalk(t)
	cfg := baseCfg()
	cfg.GroupSessionScope = GroupSessionScopeGroupSender
	ch := sendingChannel(t, cfg, api)

	err := ch.Send(context.Background(), bus.OutboundMessage{
		ChatID:  "cid-group:staff-1",
		Content: "hi",
		Metadata: map[string]string{
			"dingtalk_chat_type":       "group",
			"dingtalk_conversation_id": "cid-group",
		},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	calls := api.calls()
	if len(calls) != 1 || calls[0].Path != pathRobotSendToGroup {
		t.Fatalf("calls = %+v", calls)
	}
	if got := calls[0].Body["openConversationId"]; got != "cid-group" {
		t.Errorf("openConversationId = %v, want cid-group (not the suffixed chat id)", got)
	}
}

// msgParam is a JSON-encoded string, not a nested object. Sending an object
// yields an opaque DingTalk rejection.
func TestSend_MsgParamIsJSONString(t *testing.T) {
	api := newStubDingTalk(t)
	ch := sendingChannel(t, baseCfg(), api)

	if err := ch.Send(context.Background(), bus.OutboundMessage{
		ChatID:   "staff-1",
		Content:  "# Title\nbody",
		Metadata: map[string]string{"dingtalk_chat_type": "direct"},
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	body := api.calls()[0].Body
	if body["msgKey"] != "sampleMarkdown" {
		t.Errorf("msgKey = %v, want sampleMarkdown", body["msgKey"])
	}
	param, ok := body["msgParam"].(string)
	if !ok {
		t.Fatalf("msgParam is %T, want a JSON-encoded string", body["msgParam"])
	}
	var decoded map[string]string
	if err := json.Unmarshal([]byte(param), &decoded); err != nil {
		t.Fatalf("msgParam is not valid JSON: %v", err)
	}
	if decoded["text"] != "# Title\nbody" {
		t.Errorf("msgParam.text = %q", decoded["text"])
	}
	if decoded["title"] != "Title" {
		t.Errorf("msgParam.title = %q, want the heading with markers stripped", decoded["title"])
	}
}

// group_reply_mode only governs groups. Reading it as a global card/markdown
// switch is the easy mistake.
func TestReplyMsgType_GroupModeIsAsymmetric(t *testing.T) {
	tests := []struct {
		mode      string
		isGroup   bool
		wantMsgTy string
	}{
		{GroupReplyModeText, true, msgTypeText},
		{GroupReplyModeText, false, msgTypeMarkdown},
		{GroupReplyModeMarkdown, true, msgTypeMarkdown},
		{GroupReplyModeMarkdown, false, msgTypeMarkdown},
		{GroupReplyModeAICard, true, msgTypeMarkdown},
		{GroupReplyModeAICard, false, msgTypeMarkdown},
	}
	for _, tc := range tests {
		c := &Channel{cfg: Config{GroupReplyMode: tc.mode}}
		if got := c.replyMsgType(tc.isGroup); got != tc.wantMsgTy {
			t.Errorf("mode=%s isGroup=%v: msgType = %q, want %q", tc.mode, tc.isGroup, got, tc.wantMsgTy)
		}
	}
}

func TestSend_GroupTextModeSendsPlainText(t *testing.T) {
	api := newStubDingTalk(t)
	hook := newStubWebhook(t)
	cfg := baseCfg()
	cfg.GroupReplyMode = GroupReplyModeText
	ch := sendingChannel(t, cfg, api)

	if err := ch.Send(context.Background(), bus.OutboundMessage{
		ChatID:  "cid-group",
		Content: "**bold**",
		Metadata: map[string]string{
			"dingtalk_chat_type":       "group",
			"dingtalk_conversation_id": "cid-group",
			"dingtalk_session_webhook": hook.srv.URL,
		},
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	got := hook.received()
	if len(got) != 1 {
		t.Fatalf("webhook bodies = %+v", got)
	}
	if got[0]["msgtype"] != msgTypeText {
		t.Errorf("msgtype = %v, want text", got[0]["msgtype"])
	}
}

func TestSend_ChunksLongText(t *testing.T) {
	api := newStubDingTalk(t)
	hook := newStubWebhook(t)
	cfg := baseCfg()
	cfg.TextChunkLimit = 100
	ch := sendingChannel(t, cfg, api)

	long := strings.Repeat("word ", 100) // 500 bytes
	if err := ch.Send(context.Background(), bus.OutboundMessage{
		ChatID:   "staff-1",
		Content:  long,
		Metadata: map[string]string{"dingtalk_chat_type": "direct", "dingtalk_session_webhook": hook.srv.URL},
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if hook.hits() < 5 {
		t.Errorf("500 bytes at a 100-byte limit produced %d chunks, want >= 5", hook.hits())
	}
	for _, body := range hook.received() {
		md, _ := body["markdown"].(map[string]any)
		text, _ := md["text"].(string)
		if len(text) > cfg.TextChunkLimit {
			t.Errorf("chunk of %d bytes exceeds the %d limit", len(text), cfg.TextChunkLimit)
		}
	}
}

// A 200 with no processQueryKey means DingTalk queued nothing. Reporting success
// would claim a delivered message that never arrives.
func TestSendProactive_MissingProcessQueryKeyIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1.0/oauth2/accessToken" {
			fmt.Fprint(w, `{"accessToken":"t","expireIn":7200}`)
			return
		}
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()

	ch, _ := newTestChannelCfg(t, baseCfg())
	ch.client.apiBase = srv.URL
	if err := ch.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ch.Stop(context.Background()) })

	err := ch.Send(context.Background(), bus.OutboundMessage{
		ChatID:   "staff-1",
		Content:  "hi",
		Metadata: map[string]string{"dingtalk_chat_type": "direct"},
	})
	if err == nil || !strings.Contains(err.Error(), "processQueryKey") {
		t.Fatalf("want a processQueryKey error, got %v", err)
	}
}

func TestSend_Guards(t *testing.T) {
	api := newStubDingTalk(t)

	stopped, _ := newTestChannelCfg(t, baseCfg())
	if err := stopped.Send(context.Background(), bus.OutboundMessage{ChatID: "x", Content: "y"}); err == nil {
		t.Error("Send on a stopped channel must error")
	}

	ch := sendingChannel(t, baseCfg(), api)
	if err := ch.Send(context.Background(), bus.OutboundMessage{Content: "y"}); err == nil {
		t.Error("Send without a chat id must error")
	}
	// An empty body is not an error; it is nothing to do.
	if err := ch.Send(context.Background(), bus.OutboundMessage{ChatID: "x", Content: "   "}); err != nil {
		t.Errorf("Send with empty content: %v", err)
	}
	if len(api.calls()) != 0 {
		t.Errorf("empty content produced a request: %+v", api.calls())
	}
}

func TestWebhookUsable(t *testing.T) {
	frozen := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	c := &Channel{now: func() time.Time { return frozen }}

	tests := []struct {
		name  string
		stamp string
		want  bool
	}{
		{"no stamp means unknown, use it", "", true},
		{"unparseable stamp, use it", "not-a-number", true},
		{"future expiry", fmt.Sprint(frozen.Add(time.Minute).UnixMilli()), true},
		{"past expiry", fmt.Sprint(frozen.Add(-time.Minute).UnixMilli()), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := bus.OutboundMessage{Metadata: map[string]string{"dingtalk_webhook_expires_at": tc.stamp}}
			if got := c.webhookUsable(msg); got != tc.want {
				t.Errorf("webhookUsable = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMarkdownTitle(t *testing.T) {
	tests := []struct{ in, want string }{
		{"# Heading\nbody", "Heading"},
		{"plain body", "plain body"},
		{"\n\n  ## Second try  \n", "Second try"},
		{"> quoted", "quoted"},
		{"", "Message"},
		{"\n \n", "Message"},
		{strings.Repeat("x", 40), strings.Repeat("x", markdownTitleMaxLen) + "…"},
		// Truncation counts runes, not bytes: a CJK title must not be cut mid-character.
		{strings.Repeat("钉", 40), strings.Repeat("钉", markdownTitleMaxLen) + "…"},
	}
	for _, tc := range tests {
		if got := markdownTitle(tc.in); got != tc.want {
			t.Errorf("markdownTitle(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
