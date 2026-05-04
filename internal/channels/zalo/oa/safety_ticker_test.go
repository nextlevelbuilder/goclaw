package oa

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
)

// TestStartStop_TickerShutsDownPromptly proves the safety-ticker goroutine
// exits within a bounded time when Stop() is called. Failure mode being
// guarded: a leaked goroutine keeps polling forever after channel removal.
func TestStartStop_TickerShutsDownPromptly(t *testing.T) {
	t.Parallel()

	cfg := config.ZaloOAConfig{
		SafetyTickerMinutes: 1, // value irrelevant — we Stop before any tick fires
	}
	creds := &ChannelCreds{
		AppID:        "app",
		SecretKey:    "key",
		AccessToken:  "AT",
		RefreshToken: "RT",
		ExpiresAt:    time.Now().Add(time.Hour),
	}
	msgBus := bus.New()

	c, err := New("test_inst", cfg, creds, &fakeStore{}, msgBus, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.SetInstanceID(uuid.New())

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
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return within 2s — ticker goroutine leaked")
	}
}

// TestSafetyTicker_RefreshesWhenWithinThreshold verifies the ticker calls
// Access() (which triggers refresh) when the token sits inside the safety
// threshold. We don't measure timing precisely — just that within a few
// short ticks the upstream gets called.
func TestSafetyTicker_RefreshesWhenWithinThreshold(t *testing.T) {
	t.Parallel()

	srv, count := newRefreshServer(t, "")
	fs := &fakeStore{}

	cfg := config.ZaloOAConfig{
		// 1-second ticker so the test runs quickly. Forced via newWithInterval helper.
	}
	creds := &ChannelCreds{
		AppID:        "app",
		SecretKey:    "key",
		AccessToken:  "AT-old",
		RefreshToken: "RT-old",
		ExpiresAt:    time.Now().Add(30 * time.Second), // well inside the safety threshold
	}
	msgBus := bus.New()

	c, err := New("test_inst", cfg, creds, fs, msgBus, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.SetInstanceID(uuid.New())
	// Override the upstream OAuth host for the test.
	c.tokens.client.oauthBase = srv.URL
	// Override the ticker interval so the test doesn't wait the production default.
	c.safetyTickerInterval = 100 * time.Millisecond

	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = c.Stop(context.Background()) }()

	// Wait up to 2s for the ticker to fire and trigger one refresh.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(count) >= 1 && fs.UpdateCount() >= 1 {
			return // pass
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("ticker did not refresh within 2s: refresh=%d, updates=%d", atomic.LoadInt32(count), fs.UpdateCount())
}

// newChannelForReauthTest builds a Channel with the supplied refresh-token
// expiry so we can drive evaluateReauthWarning() without spinning the ticker.
func newChannelForReauthTest(t *testing.T, refreshExp time.Time) *Channel {
	t.Helper()
	creds := &ChannelCreds{
		AppID:                 "app",
		SecretKey:             "key",
		AccessToken:           "AT",
		RefreshToken:          "RT",
		ExpiresAt:             time.Now().Add(time.Hour),
		RefreshTokenExpiresAt: refreshExp,
	}
	c, err := New("test_inst", config.ZaloOAConfig{}, creds, &fakeStore{}, bus.New(), nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.SetInstanceID(uuid.New())
	return c
}

// In-window + Healthy → Degraded(Auth, retryable) with the i18n summary.
func TestEvaluateReauthWarning_HealthyToDegraded(t *testing.T) {
	t.Parallel()
	c := newChannelForReauthTest(t, time.Now().Add(10*24*time.Hour))
	c.MarkHealthy("connected")

	c.evaluateReauthWarning()

	snap := c.HealthSnapshot()
	if snap.State != channels.ChannelHealthStateDegraded {
		t.Fatalf("state = %q, want degraded", snap.State)
	}
	if snap.FailureKind != channels.ChannelFailureKindAuth {
		t.Errorf("failure_kind = %q, want auth", snap.FailureKind)
	}
	if !snap.Retryable {
		t.Errorf("retryable = false, want true")
	}
	if !strings.Contains(snap.Summary, "Re-consent") {
		t.Errorf("summary = %q, want contains \"Re-consent\"", snap.Summary)
	}
}

// Outside the window → Healthy stays Healthy.
func TestEvaluateReauthWarning_OutsideWindowStaysHealthy(t *testing.T) {
	t.Parallel()
	c := newChannelForReauthTest(t, time.Now().Add(30*24*time.Hour))
	c.MarkHealthy("connected")

	c.evaluateReauthWarning()

	if got := c.HealthSnapshot().State; got != channels.ChannelHealthStateHealthy {
		t.Errorf("state = %q, want healthy", got)
	}
}

// Legacy channel (zero RefreshTokenExpiresAt) → no transition, no false alarm.
func TestEvaluateReauthWarning_ZeroExpiryNoOp(t *testing.T) {
	t.Parallel()
	c := newChannelForReauthTest(t, time.Time{})
	c.MarkHealthy("connected")

	c.evaluateReauthWarning()

	if got := c.HealthSnapshot().State; got != channels.ChannelHealthStateHealthy {
		t.Errorf("state = %q, want healthy (legacy channel must stay silent)", got)
	}
}

// Re-consent path: warning was set, fresh refresh extends expiry → Healthy.
func TestEvaluateReauthWarning_ClearsAfterReconsent(t *testing.T) {
	t.Parallel()
	c := newChannelForReauthTest(t, time.Now().Add(10*24*time.Hour))
	c.MarkHealthy("connected")
	c.evaluateReauthWarning() // warning ON
	if got := c.HealthSnapshot().State; got != channels.ChannelHealthStateDegraded {
		t.Fatalf("setup: state = %q, want degraded", got)
	}

	// Operator re-consents — Phase 1 stamps a fresh expiry.
	snap := *c.creds()
	snap.RefreshTokenExpiresAt = time.Now().Add(60 * 24 * time.Hour)
	c.tokens.creds.Store(&snap)
	c.evaluateReauthWarning()

	if got := c.HealthSnapshot().State; got != channels.ChannelHealthStateHealthy {
		t.Errorf("state = %q, want healthy after re-consent", got)
	}
}

// Failed state must NOT be downgraded to Degraded(warn) — Failed wins.
func TestEvaluateReauthWarning_FailedStateLeftAlone(t *testing.T) {
	t.Parallel()
	c := newChannelForReauthTest(t, time.Now().Add(10*24*time.Hour))
	c.MarkFailed("re-auth required", "...", channels.ChannelFailureKindAuth, false)

	c.evaluateReauthWarning()

	snap := c.HealthSnapshot()
	if snap.State != channels.ChannelHealthStateFailed {
		t.Errorf("state = %q, want failed (must not downgrade)", snap.State)
	}
	if snap.Retryable {
		t.Errorf("retryable = true, want false (must not flip the failed flag)")
	}
}
