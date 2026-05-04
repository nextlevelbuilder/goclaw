package oa

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
)

// pollServer simulates the GET /v2.0/oa/listrecentchat endpoint. Tests
// configure the canned body; the server captures call count for
// assertions. listrecentchat returns MESSAGES directly (verified against
// live Zalo API via the developer API explorer, 2026-04-20) so there's
// no separate /conversation endpoint to mock.
type pollServerOpts struct {
	listResp string // body for /listrecentchat
	status   int    // override status code (0 = 200)
}

type pollServer struct {
	srv   *httptest.Server
	listN atomic.Int32
}

func newPollServer(t *testing.T, opts pollServerOpts) *pollServer {
	t.Helper()
	ps := &pollServer{}
	ps.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status := opts.status
		if status == 0 {
			status = http.StatusOK
		}
		switch r.URL.Path {
		case "/v2.0/oa/listrecentchat":
			ps.listN.Add(1)
			w.WriteHeader(status)
			if opts.listResp != "" {
				_, _ = w.Write([]byte(opts.listResp))
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(ps.srv.Close)
	return ps
}

// newPollChannel wires a Channel for poll tests. Use t.Cleanup to Stop()
// any started loops.
func newPollChannel(t *testing.T, ps *pollServer, oaID string) (*Channel, *bus.MessageBus) {
	t.Helper()
	creds := &ChannelCreds{
		AppID:        "app",
		SecretKey:    "key",
		OAID:         oaID,
		AccessToken:  "AT",
		RefreshToken: "RT",
		ExpiresAt:    time.Now().Add(time.Hour),
	}
	cfg := config.ZaloOAConfig{
		Transport:           "polling",
		PollIntervalSeconds: 1,
	}
	msgBus := bus.New()
	c, err := New("poll_test", cfg, creds, &fakeStore{}, msgBus, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.SetInstanceID(uuid.New())
	c.client.apiBase = ps.srv.URL
	return c, msgBus
}

func TestPollOnce_FetchesThreadsAndPublishesInbound(t *testing.T) {
	t.Parallel()
	ps := newPollServer(t, pollServerOpts{
		// listrecentchat returns MESSAGES directly (not thread summaries).
		// Zalo's actual field is `message`, not `text`.
		listResp: `{"error":0,"message":"Success","data":[
			{"message_id":"m1","from_id":"u1","to_id":"oa-1","time":1000,"message":"hi","type":"text","from_display_name":"Alice"}
		]}`,
	})
	c, msgBus := newPollChannel(t, ps, "oa-1")

	if err := c.pollOnce(context.Background()); err != nil {
		t.Fatalf("pollOnce: %v", err)
	}
	// Drain bus.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	msg, ok := msgBus.ConsumeInbound(ctx)
	if !ok {
		t.Fatal("expected inbound message published")
	}
	if msg.SenderID != "u1" {
		t.Errorf("SenderID = %q", msg.SenderID)
	}
	if msg.ChatID != "u1" {
		t.Errorf("ChatID = %q (Zalo OA is DM-only)", msg.ChatID)
	}
	if msg.Content != "hi" {
		t.Errorf("Content = %q", msg.Content)
	}
	if msg.PeerKind != "direct" {
		t.Errorf("PeerKind = %q, want direct", msg.PeerKind)
	}
	if msg.Metadata["message_id"] != "m1" {
		t.Errorf("metadata.message_id = %q", msg.Metadata["message_id"])
	}
}

// FilterOAMessages: messages with from_id == oa_id are echoes of our own
// outbound — must NOT be re-published as inbound.
func TestPollOnce_FiltersOAEchoMessages(t *testing.T) {
	t.Parallel()
	ps := newPollServer(t, pollServerOpts{
		listResp: `{"error":0,"data":[
			{"message_id":"oa-echo","from_id":"oa-1","to_id":"u1","time":900,"message":"my own outbound","type":"text"},
			{"message_id":"real","from_id":"u1","to_id":"oa-1","time":1000,"message":"user reply","type":"text"}
		]}`,
	})
	c, msgBus := newPollChannel(t, ps, "oa-1")

	if err := c.pollOnce(context.Background()); err != nil {
		t.Fatalf("pollOnce: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	msg, ok := msgBus.ConsumeInbound(ctx)
	if !ok {
		t.Fatal("expected one inbound message")
	}
	if msg.Content != "user reply" {
		t.Errorf("got OA echo through filter: %q", msg.Content)
	}
	// No second message should be queued.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel2()
	if _, ok := msgBus.ConsumeInbound(ctx2); ok {
		t.Error("a second inbound was queued — OA echo not filtered")
	}
}

// CursorAdvances: a second pollOnce on the same conversation must NOT
// re-emit the already-seen message.
func TestPollOnce_CursorPreventsDuplicate(t *testing.T) {
	t.Parallel()
	ps := newPollServer(t, pollServerOpts{
		listResp: `{"error":0,"data":[
			{"message_id":"m1","from_id":"u1","time":1000,"message":"hi","type":"text"}
		]}`,
	})
	c, msgBus := newPollChannel(t, ps, "oa-1")

	for i := range 3 {
		if err := c.pollOnce(context.Background()); err != nil {
			t.Fatalf("pollOnce #%d: %v", i, err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	count := 0
	for {
		ctx2, cancel2 := context.WithTimeout(ctx, 50*time.Millisecond)
		_, ok := msgBus.ConsumeInbound(ctx2)
		cancel2()
		if !ok {
			break
		}
		count++
		if count > 5 {
			break
		}
	}
	if count != 1 {
		t.Errorf("inbound count = %d, want 1 (cursor must dedupe)", count)
	}
}

// HaltOnReauth: when health is Failed/Auth, pollOnce skips the API entirely.
func TestPollOnce_HaltsWhenAuthFailed(t *testing.T) {
	t.Parallel()
	ps := newPollServer(t, pollServerOpts{
		listResp: `{"error":0,"data":[{"message_id":"m1","from_id":"u1","time":1000,"message":"hi","type":"text"}]}`,
	})
	c, _ := newPollChannel(t, ps, "oa-1")
	c.MarkFailed("re-auth required", "test-only", channels.ChannelFailureKindAuth, false)

	if err := c.pollOnce(context.Background()); err != nil {
		t.Fatalf("pollOnce: %v", err)
	}
	if got := ps.listN.Load(); got != 0 {
		t.Errorf("listrecentchat hits = %d while auth-failed; want 0", got)
	}
}

// RateLimit: HTTP 429 → ErrRateLimit returned (caller switches into backoff).
func TestPollOnce_RateLimitDetected(t *testing.T) {
	t.Parallel()
	ps := newPollServer(t, pollServerOpts{
		status:   http.StatusTooManyRequests,
		listResp: `{"error":429,"message":"rate limited"}`,
	})
	c, _ := newPollChannel(t, ps, "oa-1")

	err := c.pollOnce(context.Background())
	if err == nil {
		t.Fatal("expected rate-limit error")
	}
	if !errors.Is(err, ErrRateLimit) {
		t.Errorf("err = %v, want ErrRateLimit", err)
	}
}

// FlushCursor: SQL-level merge writes only the poll_cursor key, leaving
// operator-set sibling keys untouched. Simulated by seeding the fakeStore's
// in-memory config with operator keys before flushing.
func TestFlushCursor_PreservesOperatorConfigKeys(t *testing.T) {
	t.Parallel()
	fs := &fakeStore{}
	fs.lastConfig = map[string]any{
		"poll_interval_seconds": 15,
		"dm_policy":             "open",
	}
	c, _ := newPollChannel(t, newPollServer(t, pollServerOpts{}), "oa-1")
	c.ciStore = fs
	c.SetInstanceID(uuid.New())
	c.cursor.Advance("u1", 100)
	c.cursor.Advance("u2", 200)

	if err := c.flushCursor(context.Background()); err != nil {
		t.Fatalf("flushCursor: %v", err)
	}
	if fs.MergeCount() != 1 {
		t.Errorf("MergeCount = %d, want 1", fs.MergeCount())
	}

	got := parseCursorFromConfig(fs.ConfigBlob())
	if got["u1"] != 100 || got["u2"] != 200 {
		t.Errorf("persisted cursor = %v", got)
	}
	// Operator keys must survive the merge.
	if v, _ := fs.lastConfig["dm_policy"].(string); v != "open" {
		t.Errorf("dm_policy lost after merge: %v", fs.lastConfig)
	}
}

// AllowlistEnforcement: pollOnce → dispatchInbound → BaseChannel.HandleMessage
// must drop messages from senders not on cfg.AllowFrom when the allowlist is
// non-empty. Empty allowlist = allow-all (verified separately by phase-04 audit).
func TestPollOnce_AllowlistBlocksNonAllowedSender(t *testing.T) {
	t.Parallel()
	ps := newPollServer(t, pollServerOpts{
		listResp: `{"error":0,"data":[
			{"message_id":"m-ok","from_id":"allowed","time":1000,"message":"hi from allowed","type":"text"},
			{"message_id":"m-block","from_id":"blocked","time":2000,"message":"hi from blocked","type":"text"}
		]}`,
	})
	// Set allowlist to only "allowed". newPollChannel uses cfg.AllowFrom=nil
	// (allow all), so we construct manually here.
	creds := &ChannelCreds{
		AppID: "app", SecretKey: "key", OAID: "oa-1",
		AccessToken: "AT", RefreshToken: "RT", ExpiresAt: time.Now().Add(time.Hour),
	}
	cfg := config.ZaloOAConfig{
		AllowFrom: config.FlexibleStringSlice{"allowed"},
	}
	msgBus := bus.New()
	c, err := New("allowlist_test", cfg, creds, &fakeStore{}, msgBus, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.SetInstanceID(uuid.New())
	c.client.apiBase = ps.srv.URL

	if err := c.pollOnce(context.Background()); err != nil {
		t.Fatalf("pollOnce: %v", err)
	}
	// Drain bus.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	msg, ok := msgBus.ConsumeInbound(ctx)
	if !ok {
		t.Fatal("expected one inbound from allowed sender")
	}
	if msg.SenderID != "allowed" || msg.Content != "hi from allowed" {
		t.Errorf("unexpected msg: sender=%q content=%q", msg.SenderID, msg.Content)
	}
	// Confirm no second message (the blocked one) arrives.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel2()
	if extra, ok := msgBus.ConsumeInbound(ctx2); ok {
		t.Errorf("blocked sender slipped through allowlist: sender=%q content=%q", extra.SenderID, extra.Content)
	}
}

// dispatchInbound must drop messages with empty Text even when type=="text"
// (e.g., a sticker mis-tagged as text wouldn't have body content). Otherwise
// HandleMessage receives empty content and downstream agents see noise.
func TestDispatchInbound_EmptyTextDropped(t *testing.T) {
	t.Parallel()
	ps := newPollServer(t, pollServerOpts{
		listResp: `{"error":0,"data":[
			{"message_id":"empty","from_id":"u1","time":1000,"message":"","type":"text"}
		]}`,
	})
	c, msgBus := newPollChannel(t, ps, "oa-1")

	if err := c.pollOnce(context.Background()); err != nil {
		t.Fatalf("pollOnce: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, ok := msgBus.ConsumeInbound(ctx); ok {
		t.Error("empty-text message should not be published as inbound")
	}
}

// Start/Stop with poll loop: the goroutine must shut down within bounded time.
func TestStartStop_PollGoroutineExitsPromptly(t *testing.T) {
	t.Parallel()
	ps := newPollServer(t, pollServerOpts{
		listResp: `{"error":0,"data":[]}`,
	})
	c, _ := newPollChannel(t, ps, "oa-1")
	c.pollInterval = 50 * time.Millisecond

	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	done := make(chan struct{})
	go func() {
		_ = c.Stop(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Stop did not return within 3s — poll goroutine leaked")
	}
}
