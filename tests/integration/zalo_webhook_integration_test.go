//go:build integration

package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/bot"
	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/common"
	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/oa"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// signOAEvent reproduces the production X-ZEvent-Signature scheme:
//   hex(SHA256(appID + body + timestamp + secret))
// timestamp is taken verbatim as a decimal string (canonicalized to match
// what the server's verifier will derive from json.Number → Int64 →
// strconv.FormatInt — see oa/webhook_signature.go S4).
func signOAEvent(appID, body, timestamp, secret string) string {
	h := sha256.New()
	h.Write([]byte(appID))
	h.Write([]byte(body))
	h.Write([]byte(timestamp))
	h.Write([]byte(secret))
	return hex.EncodeToString(h.Sum(nil))
}

// buildSignedOAEvent returns the canonical body + matching signature for a
// "user_send_text" event with current ms-precision timestamp.
func buildSignedOAEvent(t *testing.T, appID, oaID, senderID, text, secret string) (body []byte, sig string) {
	t.Helper()
	tsMs := time.Now().UnixMilli()
	bodyMap := map[string]any{
		"event_name": "user_send_text",
		"app_id":     appID,
		"oa_id":      oaID,
		"timestamp":  tsMs,
		"sender":     map[string]any{"id": senderID},
		"recipient":  map[string]any{"id": oaID},
		"message":    map[string]any{"message_id": "mid-" + senderID + "-" + strconv.FormatInt(tsMs, 10), "text": text},
	}
	body, err := json.Marshal(bodyMap)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	sig = signOAEvent(appID, string(body), strconv.FormatInt(tsMs, 10), secret)
	return body, sig
}

// drainOneInbound waits up to budget for a single inbound message.
func drainOneInbound(t *testing.T, msgBus *bus.MessageBus, budget time.Duration) (bus.InboundMessage, bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	return msgBus.ConsumeInbound(ctx)
}

// ─── Cross-phase integration: shared router + two real channels ──────────

// TestZaloWebhookRouter_MultiInstanceRouting registers ONE OA channel and
// ONE Bot channel against a shared common.Router. Each channel uses a
// distinct secret + tenant. Test asserts:
//   1. POST signed for OA instance lands on OA channel (bus inbound has OA metadata)
//   2. POST signed for Bot instance lands on Bot channel
//   3. POSTing OA's payload to Bot's instance ID (cross-route attempt) is rejected by the Bot's signature verifier — no inbound published
func TestZaloWebhookRouter_MultiInstanceRouting(t *testing.T) {
	router := common.NewRouter()
	mux := http.NewServeMux()
	mux.Handle(common.WebhookPathPrefix, router)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	msgBus := bus.New()

	// ── OA channel ──
	oaTenantID := uuid.New()
	oaInstID := uuid.New()
	oaSecret := "oa-secret-int"
	oaCreds := &oa.ChannelCreds{
		AppID: "oa-app", SecretKey: "oa-sk", OAID: "oa-mt",
		AccessToken: "AT", RefreshToken: "RT", ExpiresAt: time.Now().Add(time.Hour),
		WebhookSecretKey: oaSecret,
	}
	oaCfg := config.ZaloOAConfig{
		Transport:                  "webhook",
		WebhookSignatureMode:       "strict",
		WebhookReplayWindowSeconds: 300,
	}
	oaCh, err := oa.New("oa-int", oaCfg, oaCreds, &oaIntegrationStubStore{}, msgBus, nil)
	if err != nil {
		t.Fatalf("oa.New: %v", err)
	}
	oaCh.SetInstanceID(oaInstID)
	oaCh.SetTenantID(oaTenantID)
	const oaSlug = "oa-int"
	if err := router.RegisterInstance(oaInstID, oaCh, oaTenantID, oaSlug); err != nil {
		t.Fatalf("RegisterInstance OA: %v", err)
	}
	t.Cleanup(func() { router.UnregisterInstance(oaInstID) })

	// ── Bot channel ──
	botTenantID := uuid.New()
	botInstID := uuid.New()
	botSecret := "bot-secret-int"
	botCfg := config.ZaloConfig{
		Enabled: true, Token: "bot-token",
		Transport: "webhook", WebhookSecret: botSecret,
		DMPolicy: "open", // bypass pairing-by-default for the integration test
	}
	botCh, err := bot.New(botCfg, msgBus, nil)
	if err != nil {
		t.Fatalf("bot.New: %v", err)
	}
	botCh.SetInstanceID(botInstID)
	botCh.SetTenantID(botTenantID)
	// Bot self-echo filter compares against c.botID populated by getMe at
	// Start(). We bypass Start() in this test, so botID stays "" — no echo
	// filter trips for our test sender IDs.
	const botSlug = "bot-int"
	if err := router.RegisterInstance(botInstID, botCh, botTenantID, botSlug); err != nil {
		t.Fatalf("RegisterInstance Bot: %v", err)
	}
	t.Cleanup(func() { router.UnregisterInstance(botInstID) })

	// 1. OA delivery
	body, sig := buildSignedOAEvent(t, "oa-app", "oa-mt", "user-1", "hello-from-oa", oaSecret)
	resp, err := postWebhook(t, srv.URL, oaSlug, http.Header{
		"X-Zevent-Signature": []string{sig},
		"Content-Type":       []string{"application/json"},
	}, body)
	if err != nil {
		t.Fatalf("OA POST: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("OA POST status = %d, want 200", resp.StatusCode)
	}
	msg, ok := drainOneInbound(t, msgBus, 1*time.Second)
	if !ok {
		t.Fatal("expected OA inbound, got none")
	}
	if msg.Content != "hello-from-oa" {
		t.Errorf("OA Content = %q, want hello-from-oa", msg.Content)
	}
	if msg.Metadata["platform"] != string(common.PlatformZaloOA) {
		t.Errorf("OA platform metadata = %q, want %q", msg.Metadata["platform"], common.PlatformZaloOA)
	}
	if msg.TenantID != oaTenantID {
		t.Errorf("OA TenantID = %s, want %s", msg.TenantID, oaTenantID)
	}

	// 2. Bot delivery (uses X-Bot-Api-Secret-Token header, no body sig)
	botBody := []byte(`{"event_name":"message.text.received","message":{"message_id":"bot-mid-1","from":{"id":"user-bot","display_name":"Bot User"},"chat":{"id":"user-bot"},"text":"hello-from-bot"}}`)
	resp, err = postWebhook(t, srv.URL, botSlug, http.Header{
		"X-Bot-Api-Secret-Token": []string{botSecret},
		"Content-Type":           []string{"application/json"},
	}, botBody)
	if err != nil {
		t.Fatalf("Bot POST: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Bot POST status = %d, want 200", resp.StatusCode)
	}
	msg, ok = drainOneInbound(t, msgBus, 1*time.Second)
	if !ok {
		t.Fatal("expected Bot inbound, got none")
	}
	if msg.Content != "hello-from-bot" {
		t.Errorf("Bot Content = %q, want hello-from-bot", msg.Content)
	}

	// 3. Cross-route attempt: send OA payload to Bot instance slug. Bot's
	// verifier requires X-Bot-Api-Secret-Token, which OA payloads don't
	// carry — should reject with 401 and not publish.
	body2, sig2 := buildSignedOAEvent(t, "oa-app", "oa-mt", "user-attacker", "should-not-route", oaSecret)
	resp, err = postWebhook(t, srv.URL, botSlug, http.Header{
		"X-Zevent-Signature": []string{sig2},
		"Content-Type":       []string{"application/json"},
	}, body2)
	if err != nil {
		t.Fatalf("cross-route POST: %v", err)
	}
	if resp.StatusCode == http.StatusOK {
		t.Errorf("cross-route POST returned 200 — Bot's verifier should reject OA payload (status=%d)", resp.StatusCode)
	}
	if _, ok := drainOneInbound(t, msgBus, 200*time.Millisecond); ok {
		t.Error("cross-route attempt produced inbound — verifier did not block")
	}
}

// TestZaloWebhookRouter_SignatureMismatch_NoInbound asserts that a wrong
// signature returns 401 and never reaches HandleWebhookEvent.
func TestZaloWebhookRouter_SignatureMismatch_NoInbound(t *testing.T) {
	router := common.NewRouter()
	mux := http.NewServeMux()
	mux.Handle(common.WebhookPathPrefix, router)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	msgBus := bus.New()
	tenantID := uuid.New()
	instID := uuid.New()
	creds := &oa.ChannelCreds{
		AppID: "oa-app", SecretKey: "oa-sk", OAID: "oa-mt",
		AccessToken: "AT", RefreshToken: "RT", ExpiresAt: time.Now().Add(time.Hour),
		WebhookSecretKey: "right-secret",
	}
	cfg := config.ZaloOAConfig{
		Transport:           "webhook",
		WebhookSignatureMode: "strict", WebhookReplayWindowSeconds: 300,
	}
	ch, err := oa.New("oa-mismatch", cfg, creds, &oaIntegrationStubStore{}, msgBus, nil)
	if err != nil {
		t.Fatalf("oa.New: %v", err)
	}
	ch.SetInstanceID(instID)
	ch.SetTenantID(tenantID)
	const slug = "oa-mismatch"
	if err := router.RegisterInstance(instID, ch, tenantID, slug); err != nil {
		t.Fatalf("RegisterInstance: %v", err)
	}
	t.Cleanup(func() { router.UnregisterInstance(instID) })

	// Sign with the WRONG secret.
	body, sig := buildSignedOAEvent(t, "oa-app", "oa-mt", "user-x", "no-route", "wrong-secret")
	resp, err := postWebhook(t, srv.URL, slug, http.Header{
		"X-Zevent-Signature": []string{sig},
		"Content-Type":       []string{"application/json"},
	}, body)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
	if _, ok := drainOneInbound(t, msgBus, 200*time.Millisecond); ok {
		t.Error("inbound published despite signature mismatch")
	}
}

// TestZaloWebhookRouter_UnknownSlug_404 confirms an unregistered slug
// returns 404 cleanly.
func TestZaloWebhookRouter_UnknownSlug_404(t *testing.T) {
	router := common.NewRouter()
	mux := http.NewServeMux()
	mux.Handle(common.WebhookPathPrefix, router)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	resp, err := postWebhook(t, srv.URL, "ghost-slug", http.Header{
		"Content-Type": []string{"application/json"},
	}, []byte(`{}`))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

// Note: WS RPC handler branches (UUID parse, store.Get, cross-tenant,
// wrong channel type, success) are covered by unit tests in
// internal/gateway/methods/zalo_webhook_test.go. Replicating that here
// would require a full gateway.Server harness for permission gating with
// no additional coverage value.

// ─── helpers ─────────────────────────────────────────────────────────────

// oaIntegrationStubStore stubs ChannelInstanceStore enough for oa.New;
// integration tests that need real PG use ciStore directly.
type oaIntegrationStubStore struct {
	store.ChannelInstanceStore
}

func (oaIntegrationStubStore) Get(_ context.Context, _ uuid.UUID) (*store.ChannelInstanceData, error) {
	return nil, nil
}

func (oaIntegrationStubStore) MergeConfig(_ context.Context, _ uuid.UUID, _ map[string]any) error {
	return nil
}

func (oaIntegrationStubStore) Update(_ context.Context, _ uuid.UUID, _ map[string]any) error {
	return nil
}

func postWebhook(t *testing.T, baseURL string, slug string, headers http.Header, body []byte) (*http.Response, error) {
	t.Helper()
	u := fmt.Sprintf("%s%s%s", baseURL, common.WebhookPathPrefix, slug)
	req, err := http.NewRequest(http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for k, vv := range headers {
		for _, v := range vv {
			req.Header.Add(k, v)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp, nil
}

