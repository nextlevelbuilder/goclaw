package oa

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
)

// newCatchUpChannel returns a webhook-mode channel pointed at the given
// listrecentchat test server. Cursor is empty by default → catch-up will
// fire when invoked.
func newCatchUpChannel(t *testing.T, apiURL, oaID string) (*Channel, *bus.MessageBus, *atomic.Int32) {
	t.Helper()
	creds := &ChannelCreds{
		AppID:            "app-1",
		SecretKey:        "k",
		OAID:             oaID,
		AccessToken:      "AT",
		RefreshToken:     "RT",
		ExpiresAt:        time.Now().Add(time.Hour),
		WebhookSecretKey: "s",
	}
	cfg := config.ZaloOAConfig{
		Transport:        "webhook",
		CatchUpOnRestart: true,
	}
	mb := bus.New()
	c, err := New("catchup_test", cfg, creds, &fakeStore{}, mb, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.SetInstanceID(uuid.New())
	c.client.apiBase = apiURL
	return c, mb, nil
}

// catchupServer counts list calls and returns canned bodies.
type catchupServer struct {
	srv      *httptest.Server
	listN    atomic.Int32
	listBody string
	failWith int // status code; 0 → 200
}

func newCatchupServer(t *testing.T, body string) *catchupServer {
	t.Helper()
	s := &catchupServer{listBody: body}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2.0/oa/listrecentchat" {
			s.listN.Add(1)
			if s.failWith != 0 {
				w.WriteHeader(s.failWith)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(s.listBody))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

// Cursor recently-advanced (<30min) → no list call made.
func TestCatchUp_FreshCursorSkipsListCall(t *testing.T) {
	t.Parallel()
	srv := newCatchupServer(t, `{"error":0,"data":[]}`)
	c, _, _ := newCatchUpChannel(t, srv.srv.URL, "oa-1")

	// Seed cursor with a recent timestamp (now - 1min). LastSeenTimestamp()
	// will report this and gate the sweep.
	c.cursor.Advance("u1", time.Now().UnixMilli()-int64(time.Minute.Milliseconds()))

	c.runCatchUpSweep(context.Background())
	if got := srv.listN.Load(); got != 0 {
		t.Errorf("list calls = %d, want 0 (cursor is fresh)", got)
	}
}

// Cursor stale (>30min) → exactly one list call, messages dispatched.
func TestCatchUp_StaleCursorTriggersSingleListCall(t *testing.T) {
	t.Parallel()
	srv := newCatchupServer(t, `{"error":0,"data":[
		{"message_id":"m1","from_id":"u1","time":2000,"message":"hi","type":"text"}
	]}`)
	c, mb, _ := newCatchUpChannel(t, srv.srv.URL, "oa-1")
	// Cursor empty → LastSeenTimestamp == 0 → stale.
	c.runCatchUpSweep(context.Background())
	if got := srv.listN.Load(); got != 1 {
		t.Fatalf("list calls = %d, want 1", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	got, ok := mb.ConsumeInbound(ctx)
	if !ok {
		t.Fatal("no inbound dispatched from catch-up")
	}
	if got.Content != "hi" {
		t.Errorf("Content = %q", got.Content)
	}
}

// API error during catch-up is logged and swallowed — no panic, no dispatch.
func TestCatchUp_ListErrorTolerated(t *testing.T) {
	t.Parallel()
	srv := newCatchupServer(t, "")
	srv.failWith = http.StatusInternalServerError
	c, mb, _ := newCatchUpChannel(t, srv.srv.URL, "oa-1")

	// Must not panic.
	c.runCatchUpSweep(context.Background())

	if got := srv.listN.Load(); got < 1 {
		t.Errorf("list calls = %d, want >=1 (the failing call)", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, ok := mb.ConsumeInbound(ctx); ok {
		t.Error("error path should not have dispatched")
	}
}

// Self-echo (from_id == oa_id) is filtered just like polling.
func TestCatchUp_FiltersOAEcho(t *testing.T) {
	t.Parallel()
	srv := newCatchupServer(t, `{"error":0,"data":[
		{"message_id":"echo","from_id":"oa-1","time":1000,"message":"my own","type":"text"},
		{"message_id":"real","from_id":"u1","time":2000,"message":"user reply","type":"text"}
	]}`)
	c, mb, _ := newCatchUpChannel(t, srv.srv.URL, "oa-1")

	c.runCatchUpSweep(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	got, ok := mb.ConsumeInbound(ctx)
	if !ok {
		t.Fatal("expected one inbound dispatched")
	}
	if got.Content != "user reply" {
		t.Errorf("OA echo leaked through filter: %q", got.Content)
	}
	// No second message.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel2()
	if _, ok := mb.ConsumeInbound(ctx2); ok {
		t.Error("second inbound queued — echo not filtered")
	}
}
