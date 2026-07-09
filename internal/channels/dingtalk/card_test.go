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
	"sync/atomic"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// stubCardAPI serves the token endpoint and every card endpoint, recording calls.
type stubCardAPI struct {
	mu    sync.Mutex
	calls []capturedRequest

	// qpsRejectFirst makes the first streaming write fail with a QpsLimit 403.
	qpsRejectFirst atomic.Bool
	srv            *httptest.Server
}

func newStubCardAPI(t *testing.T) *stubCardAPI {
	t.Helper()
	s := &stubCardAPI{}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1.0/oauth2/accessToken" {
			fmt.Fprint(w, `{"accessToken":"t","expireIn":7200}`)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)

		s.mu.Lock()
		s.calls = append(s.calls, capturedRequest{Path: r.URL.Path, Body: body})
		s.mu.Unlock()

		if r.URL.Path == pathCardStreaming && s.qpsRejectFirst.CompareAndSwap(true, false) {
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"code":"QpsLimitError","message":"too fast","requestid":"r1"}`)
			return
		}
		fmt.Fprint(w, `{}`)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *stubCardAPI) snapshot() []capturedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]capturedRequest(nil), s.calls...)
}

func (s *stubCardAPI) pathsOf(path string) []capturedRequest {
	var out []capturedRequest
	for _, c := range s.snapshot() {
		if c.Path == path {
			out = append(out, c)
		}
	}
	return out
}

// cardChannel returns a running channel whose card writes hit api, with the rate
// limiter's spacing removed so tests do not sleep.
func cardChannel(t *testing.T, cfg Config, api *stubCardAPI) *Channel {
	t.Helper()
	ch, _ := newTestChannelCfg(t, cfg)
	ch.client.apiBase = api.srv.URL
	ch.cardLimiter.interval = 0
	ch.cardLimiter.backoffFor = time.Millisecond
	if err := ch.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = ch.Stop(context.Background()) })
	return ch
}

// A card left at INPUTING spins forever in the DingTalk UI. Stop is called on
// run.completed, run.failed AND run.cancelled, so every one of them terminates.
func TestCard_StopAlwaysReachesTerminalStatus(t *testing.T) {
	api := newStubCardAPI(t)
	ch := cardChannel(t, baseCfg(), api)
	ch.rememberChat("staff-1", chatMeta{UserID: "staff-1"})

	stream, err := ch.CreateStream(context.Background(), "staff-1", true)
	if err != nil {
		t.Fatalf("CreateStream: %v", err)
	}
	stream.Update(context.Background(), "partial")

	if err := stream.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if got := terminalStatus(api); got != flowStatusFinished {
		t.Fatalf("terminal flowStatus = %q, want %q", got, flowStatusFinished)
	}
}

// Stop is idempotent: the framework may call it on a stream it already stopped.
func TestCard_StopIsIdempotent(t *testing.T) {
	api := newStubCardAPI(t)
	ch := cardChannel(t, baseCfg(), api)
	ch.rememberChat("staff-1", chatMeta{UserID: "staff-1"})

	stream, err := ch.CreateStream(context.Background(), "staff-1", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	before := len(api.pathsOf(pathCardInstances))
	if err := stream.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if after := len(api.pathsOf(pathCardInstances)); after != before {
		t.Errorf("second Stop issued %d extra writes", after-before)
	}
}

// A gateway restart mid-run must not leave cards spinning. Channel.Stop aborts
// every open card, and calls it FAILED rather than claiming an answer arrived.
func TestCard_ChannelStopAbortsLiveCards(t *testing.T) {
	api := newStubCardAPI(t)
	ch, _ := newTestChannelCfg(t, baseCfg())
	ch.client.apiBase = api.srv.URL
	ch.cardLimiter.interval = 0
	if err := ch.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	ch.rememberChat("staff-1", chatMeta{UserID: "staff-1"})

	if _, err := ch.CreateStream(context.Background(), "staff-1", true); err != nil {
		t.Fatal(err)
	}

	if err := ch.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if got := terminalStatus(api); got != flowStatusFailed {
		t.Fatalf("abandoned card flowStatus = %q, want %q", got, flowStatusFailed)
	}
}

// terminalStatus returns the flowStatus of the last PUT to /card/instances.
func terminalStatus(api *stubCardAPI) string {
	calls := api.pathsOf(pathCardInstances)
	for i := len(calls) - 1; i >= 0; i-- {
		data, _ := calls[i].Body["cardData"].(map[string]any)
		param, _ := data["cardParamMap"].(map[string]any)
		if s, ok := param["flowStatus"].(string); ok && s != flowStatusInputing {
			return s
		}
	}
	return ""
}

func TestCard_CreateDeliversToRightSpace(t *testing.T) {
	tests := []struct {
		name        string
		meta        chatMeta
		wantSpaceID string
		wantModel   string
	}{
		{"dm", chatMeta{UserID: "staff-1"},
			"dtv1.card//IM_ROBOT.staff-1", "imRobotOpenDeliverModel"},
		{"group", chatMeta{IsGroup: true, ConversationID: "cid-1"},
			"dtv1.card//IM_GROUP.cid-1", "imGroupOpenDeliverModel"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			api := newStubCardAPI(t)
			ch := cardChannel(t, baseCfg(), api)
			ch.rememberChat("chat", tc.meta)

			if _, err := ch.CreateStream(context.Background(), "chat", true); err != nil {
				t.Fatalf("CreateStream: %v", err)
			}

			deliver := api.pathsOf(pathCardDeliver)
			if len(deliver) != 1 {
				t.Fatalf("deliver calls = %d", len(deliver))
			}
			if got := deliver[0].Body["openSpaceId"]; got != tc.wantSpaceID {
				t.Errorf("openSpaceId = %v, want %v", got, tc.wantSpaceID)
			}
			if _, ok := deliver[0].Body[tc.wantModel]; !ok {
				t.Errorf("missing %s in %+v", tc.wantModel, deliver[0].Body)
			}

			create := api.pathsOf(pathCardInstances)[0]
			if create.Body["cardTemplateId"] != cardTemplateID {
				t.Errorf("cardTemplateId = %v", create.Body["cardTemplateId"])
			}
			outTrackID, _ := create.Body["outTrackId"].(string)
			if !strings.HasPrefix(outTrackID, "card_") {
				t.Errorf("outTrackId = %q", outTrackID)
			}
		})
	}
}

// A group card cannot be delivered without a conversation id.
func TestCard_GroupWithoutConversationIDFails(t *testing.T) {
	api := newStubCardAPI(t)
	ch := cardChannel(t, baseCfg(), api)
	ch.rememberChat("chat", chatMeta{IsGroup: true})

	if _, err := ch.CreateStream(context.Background(), "chat", true); err == nil {
		t.Fatal("want error for a group chat with no conversation id")
	}
}

// DingTalk expects the whole text on every frame, not a delta, and a trailing
// newline on a non-final frame makes the card flicker.
func TestCard_StreamsFullTextAndTrimsTrailingNewline(t *testing.T) {
	api := newStubCardAPI(t)
	ch := cardChannel(t, baseCfg(), api)
	ch.rememberChat("staff-1", chatMeta{UserID: "staff-1"})

	stream, err := ch.CreateStream(context.Background(), "staff-1", true)
	if err != nil {
		t.Fatal(err)
	}
	stream.Update(context.Background(), "hello\n")

	frames := api.pathsOf(pathCardStreaming)
	if len(frames) != 1 {
		t.Fatalf("streaming frames = %d", len(frames))
	}
	if frames[0].Body["content"] != "hello" {
		t.Errorf("content = %q, want the trailing newline trimmed", frames[0].Body["content"])
	}
	if frames[0].Body["isFull"] != true {
		t.Errorf("isFull = %v, want true (DingTalk wants the whole text)", frames[0].Body["isFull"])
	}
	if frames[0].Body["isFinalize"] != false {
		t.Errorf("isFinalize = %v on a mid-stream frame", frames[0].Body["isFinalize"])
	}
	if frames[0].Body["key"] != cardContentKey {
		t.Errorf("key = %v", frames[0].Body["key"])
	}
}

// Every LLM chunk would otherwise become an HTTP request. Skipped repaints are
// not lost: Stop flushes the latest text.
func TestCard_ThrottlesUpdatesAndFlushesOnStop(t *testing.T) {
	api := newStubCardAPI(t)
	ch := cardChannel(t, baseCfg(), api)

	frozen := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	ch.now = func() time.Time { return frozen }
	ch.rememberChat("staff-1", chatMeta{UserID: "staff-1"})

	stream, err := ch.CreateStream(context.Background(), "staff-1", true)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	for i := range 10 {
		stream.Update(ctx, fmt.Sprintf("chunk %d", i))
	}

	// The clock never advances, so only the first Update escapes the throttle.
	if got := len(api.pathsOf(pathCardStreaming)); got != 1 {
		t.Errorf("10 rapid updates produced %d frames, want 1", got)
	}

	if err := stream.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	frames := api.pathsOf(pathCardStreaming)
	last := frames[len(frames)-1]
	if last.Body["content"] != "chunk 9" {
		t.Errorf("final frame content = %q, want the last throttled text", last.Body["content"])
	}
	if last.Body["isFinalize"] != true {
		t.Errorf("final frame isFinalize = %v", last.Body["isFinalize"])
	}
}

func TestCard_ThrottleReleasesAfterInterval(t *testing.T) {
	api := newStubCardAPI(t)
	ch := cardChannel(t, baseCfg(), api)

	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	ch.now = func() time.Time { return now }
	ch.rememberChat("staff-1", chatMeta{UserID: "staff-1"})

	stream, err := ch.CreateStream(context.Background(), "staff-1", true)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	stream.Update(ctx, "a")
	now = now.Add(cardUpdateThrottle - time.Millisecond)
	stream.Update(ctx, "b") // still throttled
	now = now.Add(2 * time.Millisecond)
	stream.Update(ctx, "c") // past the window

	if got := len(api.pathsOf(pathCardStreaming)); got != 2 {
		t.Errorf("streaming frames = %d, want 2", got)
	}
}

// A rate-limited frame is retried once, and the retry must carry a fresh guid —
// reusing it makes DingTalk treat the frame as a duplicate and drop it.
func TestCard_QPSLimitRetriesWithNewGuid(t *testing.T) {
	api := newStubCardAPI(t)
	api.qpsRejectFirst.Store(true)
	ch := cardChannel(t, baseCfg(), api)
	ch.rememberChat("staff-1", chatMeta{UserID: "staff-1"})

	stream, err := ch.CreateStream(context.Background(), "staff-1", true)
	if err != nil {
		t.Fatal(err)
	}
	stream.Update(context.Background(), "hello")

	frames := api.pathsOf(pathCardStreaming)
	if len(frames) != 2 {
		t.Fatalf("streaming frames = %d, want 2 (original + retry)", len(frames))
	}
	g1, _ := frames[0].Body["guid"].(string)
	g2, _ := frames[1].Body["guid"].(string)
	if g1 == "" || g2 == "" {
		t.Fatalf("missing guids: %q %q", g1, g2)
	}
	if g1 == g2 {
		t.Errorf("retry reused guid %q; DingTalk would drop it as a duplicate", g1)
	}
}

func TestIsQPSLimit(t *testing.T) {
	if !isQPSLimit(&apiError{Status: http.StatusForbidden, Code: "QpsLimitError"}) {
		t.Error("403 QpsLimitError not recognized")
	}
	if isQPSLimit(&apiError{Status: http.StatusForbidden, Code: "Forbidden.AccessDenied"}) {
		t.Error("a plain 403 must not be treated as a rate limit")
	}
	if isQPSLimit(&apiError{Status: http.StatusInternalServerError, Code: "QpsLimitError"}) {
		t.Error("a 500 must not be treated as a rate limit")
	}
	if isQPSLimit(fmt.Errorf("network down")) {
		t.Error("a non-API error must not be treated as a rate limit")
	}
}

func TestCard_RateLimiterBacksOff(t *testing.T) {
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }

	l := newCardRateLimiter()
	l.interval = 0
	l.backoff(clock)

	if !l.nextAllowed.Equal(now.Add(cardQPSBackoff)) {
		t.Errorf("nextAllowed = %v, want now+%v", l.nextAllowed, cardQPSBackoff)
	}
	// Backoff only ever pushes the gate later, never pulls it in.
	l.nextAllowed = now.Add(time.Hour)
	l.backoff(clock)
	if !l.nextAllowed.Equal(now.Add(time.Hour)) {
		t.Errorf("backoff pulled nextAllowed earlier: %v", l.nextAllowed)
	}
}

// The streamed text is raw; Send receives the same answer formatted. Repainting
// the card in place is what stops a duplicate message appearing beneath it.
func TestCard_SendRepaintsInsteadOfPostingAgain(t *testing.T) {
	api := newStubCardAPI(t)
	ch := cardChannel(t, baseCfg(), api)
	ch.rememberChat("staff-1", chatMeta{UserID: "staff-1"})

	ctx := context.Background()
	stream, err := ch.CreateStream(ctx, "staff-1", true)
	if err != nil {
		t.Fatal(err)
	}
	stream.Update(ctx, "raw")
	if err := stream.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	ch.FinalizeStream(ctx, "staff-1", stream)

	if err := ch.Send(ctx, bus.OutboundMessage{
		ChatID:   "staff-1",
		Content:  "**formatted**",
		Metadata: map[string]string{"dingtalk_chat_type": "direct"},
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	// No new message was posted.
	if got := api.pathsOf(pathRobotSendToUser); len(got) != 0 {
		t.Errorf("Send posted a duplicate message: %+v", got)
	}

	// The card carries the formatted answer.
	instances := api.pathsOf(pathCardInstances)
	last := instances[len(instances)-1]
	data, _ := last.Body["cardData"].(map[string]any)
	param, _ := data["cardParamMap"].(map[string]any)
	if param[cardContentKey] != "**formatted**" {
		t.Errorf("card content = %v, want the formatted answer", param[cardContentKey])
	}
	if last.Body["cardUpdateOptions"] == nil {
		t.Error("repaint must set cardUpdateOptions.updateCardDataByKey")
	}
}

// The card is claimed once. A second Send for the same chat posts normally.
func TestCard_HandoffIsSingleUse(t *testing.T) {
	api := newStubCardAPI(t)
	ch := cardChannel(t, baseCfg(), api)
	ch.rememberChat("staff-1", chatMeta{UserID: "staff-1"})

	ctx := context.Background()
	stream, _ := ch.CreateStream(ctx, "staff-1", true)
	_ = stream.Stop(ctx)
	ch.FinalizeStream(ctx, "staff-1", stream)

	msg := bus.OutboundMessage{
		ChatID:   "staff-1",
		Content:  "one",
		Metadata: map[string]string{"dingtalk_chat_type": "direct"},
	}
	if err := ch.Send(ctx, msg); err != nil {
		t.Fatal(err)
	}
	// Second Send has no card to claim; it must post. The stub returns no
	// processQueryKey for robot paths, so an error here proves it tried.
	err := ch.Send(ctx, msg)
	if err == nil || !strings.Contains(err.Error(), "processQueryKey") {
		t.Fatalf("second Send should have posted a real message, got %v", err)
	}
}

// A failed run does not call FinalizeStream, so the framework's error message
// arrives as its own message rather than overwriting the streamed text.
func TestCard_FailedRunLeavesCardAndPostsError(t *testing.T) {
	api := newStubCardAPI(t)
	ch := cardChannel(t, baseCfg(), api)
	ch.rememberChat("staff-1", chatMeta{UserID: "staff-1"})

	ctx := context.Background()
	stream, _ := ch.CreateStream(ctx, "staff-1", true)
	stream.Update(ctx, "partial thought")
	_ = stream.Stop(ctx) // run.failed path: Stop, no FinalizeStream

	if _, claimed := ch.takeCard("staff-1"); claimed {
		t.Error("a failed run must not hand its card to Send")
	}
}

func TestStreamEnabled_Matrix(t *testing.T) {
	no := false
	tests := []struct {
		name    string
		cfg     Config
		isGroup bool
		want    bool
	}{
		{"default dm", Config{}, false, true},
		{"default group", Config{GroupReplyMode: GroupReplyModeAICard}, true, true},
		{"streaming off", Config{Streaming: &no}, false, false},
		{"group text mode", Config{GroupReplyMode: GroupReplyModeText}, true, false},
		{"group markdown mode", Config{GroupReplyMode: GroupReplyModeMarkdown}, true, false},
		// The asymmetry: group_reply_mode never disables DM streaming.
		{"dm with group text mode", Config{GroupReplyMode: GroupReplyModeText}, false, true},
		{"dm with group markdown mode", Config{GroupReplyMode: GroupReplyModeMarkdown}, false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := &Channel{cfg: tc.cfg}
			if got := c.StreamEnabled(tc.isGroup); got != tc.want {
				t.Errorf("StreamEnabled(isGroup=%v) = %v, want %v", tc.isGroup, got, tc.want)
			}
		})
	}
}

// The stock template exposes one content key, so reasoning has no lane.
func TestReasoningStreamEnabled(t *testing.T) {
	c := &Channel{}
	if c.ReasoningStreamEnabled() {
		t.Error("ReasoningStreamEnabled must be false: the card template has one msgContent key")
	}
}

func TestOutTrackID_Unique(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		id := newOutTrackID()
		if seen[id] {
			t.Fatalf("duplicate outTrackId %q", id)
		}
		seen[id] = true
	}
}

// CreateStream on a chat with no inbound message (cron, delegate) treats the
// chatID as a user id, which is what a DM chatID already is.
func TestCreateStream_UnknownChatFallsBackToDM(t *testing.T) {
	api := newStubCardAPI(t)
	ch := cardChannel(t, baseCfg(), api)

	if _, err := ch.CreateStream(context.Background(), "staff-9", true); err != nil {
		t.Fatalf("CreateStream: %v", err)
	}
	deliver := api.pathsOf(pathCardDeliver)[0]
	if got := deliver.Body["openSpaceId"]; got != "dtv1.card//IM_ROBOT.staff-9" {
		t.Errorf("openSpaceId = %v", got)
	}
}
