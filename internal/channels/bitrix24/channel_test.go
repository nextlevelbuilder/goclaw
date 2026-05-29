package bitrix24

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// newStartedChannel builds a Channel whose Start() side-effects (portal,
// client, botID) are pre-populated without hitting Bitrix24. Tests that care
// about Send/DispatchEvent semantics instead of the Start control flow use
// this to skip the OAuth + imbot.register dance.
func newStartedChannel(t *testing.T, fs *fakeBitrixStore, tenant uuid.UUID, portalName string, botID int, state store.BitrixPortalState) *Channel {
	t.Helper()

	creds, _ := json.Marshal(store.BitrixPortalCredentials{ClientID: "cid", ClientSecret: "secret"})
	stateBytes, _ := json.Marshal(state)
	fs.seed(tenant, portalName, "portal.bitrix24.com", creds, stateBytes)

	resetWebhookRouterForTest()
	fn := FactoryWithPortalStore(fs, "")
	cfg := json.RawMessage(`{"portal":"` + portalName + `","bot_code":"c","bot_name":"n"}`)
	ch, err := fn("b1", nil, cfg, bus.New(), nil)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	bc := ch.(*Channel)
	bc.SetTenantID(tenant)

	// Load the portal directly via the router so it's registered + bound to the
	// singleton (matches what Start would do).
	p, err := bc.router.ResolveOrLoadPortal(context.Background(), tenant, portalName)
	if err != nil {
		t.Fatalf("resolve portal: %v", err)
	}
	bc.router.RegisterPortal(p)

	bc.startMu.Lock()
	bc.portal = p
	bc.client = p.Client()
	bc.botID = botID
	bc.startMu.Unlock()

	bc.SetRunning(true)
	return bc
}

func TestChannel_Type_IsBitrix24(t *testing.T) {
	fs := newFakeStore()
	resetWebhookRouterForTest()
	defer resetWebhookRouterForTest()
	fn := FactoryWithPortalStore(fs, "")

	ch, err := fn("b1", nil,
		json.RawMessage(`{"portal":"p","bot_code":"c","bot_name":"n"}`),
		&bus.MessageBus{}, nil)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	if ch.Type() != channels.TypeBitrix24 {
		t.Errorf("Type = %q; want %q", ch.Type(), channels.TypeBitrix24)
	}
}

func TestChannel_Accessors_BeforeStart(t *testing.T) {
	fs := newFakeStore()
	resetWebhookRouterForTest()
	defer resetWebhookRouterForTest()
	fn := FactoryWithPortalStore(fs, "")

	ch, err := fn("b1", nil,
		json.RawMessage(`{"portal":"p","bot_code":"mybot","bot_name":"n"}`),
		&bus.MessageBus{}, nil)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	bc := ch.(*Channel)

	// BotID / Portal / Client must be zero-valued before Start().
	if got := bc.BotID(); got != 0 {
		t.Errorf("BotID before Start = %d; want 0", got)
	}
	if bc.Portal() != nil {
		t.Error("Portal() should be nil before Start")
	}
	if bc.Client() != nil {
		t.Error("Client() should be nil before Start")
	}
	if bc.PortalName() != "p" {
		t.Errorf("PortalName = %q; want %q", bc.PortalName(), "p")
	}
}

func TestChannel_Start_PortalNotInstalled_FailsAuth(t *testing.T) {
	fs := newFakeStore()
	tid := store.GenNewID()
	// Seed portal with credentials but NO refresh token → Installed() == false.
	creds, _ := json.Marshal(store.BitrixPortalCredentials{ClientID: "cid", ClientSecret: "secret"})
	stateBytes, _ := json.Marshal(store.BitrixPortalState{})
	fs.seed(tid, "p", "portal.bitrix24.com", creds, stateBytes)

	resetWebhookRouterForTest()
	defer resetWebhookRouterForTest()
	fn := FactoryWithPortalStore(fs, "")
	ch, err := fn("b1", nil,
		json.RawMessage(`{"portal":"p","bot_code":"c","bot_name":"n"}`),
		bus.New(), nil)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	ch.(*Channel).SetTenantID(tid)

	err = ch.Start(context.Background())
	if err == nil {
		t.Fatal("Start must fail when portal is not installed")
	}

	// Health should flag the failure with Auth kind + retryable=false (admin
	// must visit /bitrix24/install).
	h := ch.(*Channel).HealthSnapshot()
	if h.State != channels.ChannelHealthStateFailed {
		t.Errorf("health state = %q; want Failed", h.State)
	}
	if h.FailureKind != channels.ChannelFailureKindAuth {
		t.Errorf("failure kind = %q; want Auth", h.FailureKind)
	}
}

func TestChannel_Start_PortalNotFound_FailsConfig(t *testing.T) {
	fs := newFakeStore()
	tid := store.GenNewID()
	// Deliberately do NOT seed a portal row — store returns "not found".

	resetWebhookRouterForTest()
	defer resetWebhookRouterForTest()
	fn := FactoryWithPortalStore(fs, "")
	ch, err := fn("b1", nil,
		json.RawMessage(`{"portal":"ghost","bot_code":"c","bot_name":"n"}`),
		bus.New(), nil)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	ch.(*Channel).SetTenantID(tid)

	err = ch.Start(context.Background())
	if err == nil {
		t.Fatal("Start must fail when portal row is missing")
	}
	h := ch.(*Channel).HealthSnapshot()
	if h.FailureKind != channels.ChannelFailureKindConfig {
		t.Errorf("failure kind = %q; want Config", h.FailureKind)
	}
}

func TestChannel_Stop_Idempotent(t *testing.T) {
	fs := newFakeStore()
	tid := store.GenNewID()
	ch := newStartedChannel(t, fs, tid, "p", 42, store.BitrixPortalState{
		RefreshToken: "RT",
		AccessToken:  "AT",
		ExpiresAt:    time.Now().Add(time.Hour),
	})
	defer resetWebhookRouterForTest()

	// Triple Stop must not panic or deadlock; once the botID is cleared, the
	// second+ call is a no-op.
	if err := ch.Stop(context.Background()); err != nil {
		t.Fatalf("first Stop: %v", err)
	}
	if err := ch.Stop(context.Background()); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
	if err := ch.Stop(context.Background()); err != nil {
		t.Fatalf("third Stop: %v", err)
	}

	if ch.IsRunning() {
		t.Error("IsRunning should be false after Stop")
	}
	if got := ch.BotID(); got != 0 {
		t.Errorf("BotID after Stop = %d; want 0", got)
	}
}

func TestChannel_Stop_UnregistersBotFromRouter(t *testing.T) {
	fs := newFakeStore()
	tid := store.GenNewID()
	ch := newStartedChannel(t, fs, tid, "p", 777, store.BitrixPortalState{
		RefreshToken: "RT", AccessToken: "AT", ExpiresAt: time.Now().Add(time.Hour),
	})
	defer resetWebhookRouterForTest()

	// Manually register so we can observe the unregister side-effect.
	ch.router.RegisterBot(777, ch)

	if err := ch.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// After Stop the router must not return a dispatcher for this bot id.
	ch.router.mu.RLock()
	_, exists := ch.router.byBotID[777]
	ch.router.mu.RUnlock()
	if exists {
		t.Error("router still has dispatcher for stopped bot")
	}
}

func TestClassifyStartupErr_AuthCodes(t *testing.T) {
	cases := []struct {
		code string
		want channels.ChannelFailureKind
	}{
		{"expired_token", channels.ChannelFailureKindAuth},
		{"invalid_token", channels.ChannelFailureKindAuth},
		{"NO_AUTH_FOUND", channels.ChannelFailureKindAuth},
		{"PORTAL_DELETED", channels.ChannelFailureKindAuth},
		{"QUERY_LIMIT_EXCEEDED", channels.ChannelFailureKindConfig},
		{"ERROR_ARGUMENT", channels.ChannelFailureKindConfig},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			err := &APIError{Code: tc.code, Method: "imbot.register"}
			if got := classifyStartupErr(err); got != tc.want {
				t.Errorf("classifyStartupErr(%q) = %q; want %q", tc.code, got, tc.want)
			}
		})
	}

	// Nil → Unknown.
	if got := classifyStartupErr(nil); got != channels.ChannelFailureKindUnknown {
		t.Errorf("classifyStartupErr(nil) = %q; want Unknown", got)
	}
	// Plain error → Config.
	if got := classifyStartupErr(errors.New("boom")); got != channels.ChannelFailureKindConfig {
		t.Errorf("classifyStartupErr(plain) = %q; want Config", got)
	}
}

// Confirms Start refuses to run when SetTenantID was never called — guards
// against the gateway wiring bug where InstanceLoader forgets to propagate
// the tenant_id onto the channel.
func TestChannel_Start_RequiresTenantID(t *testing.T) {
	fs := newFakeStore()
	resetWebhookRouterForTest()
	defer resetWebhookRouterForTest()
	fn := FactoryWithPortalStore(fs, "")
	ch, err := fn("b1", nil,
		json.RawMessage(`{"portal":"p","bot_code":"c","bot_name":"n"}`),
		bus.New(), nil)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	// Explicitly leave TenantID zero.

	err = ch.Start(context.Background())
	if err == nil {
		t.Fatal("Start must reject missing tenant")
	}
	h := ch.(*Channel).HealthSnapshot()
	if h.FailureKind != channels.ChannelFailureKindConfig {
		t.Errorf("failure kind = %q; want Config", h.FailureKind)
	}
}
