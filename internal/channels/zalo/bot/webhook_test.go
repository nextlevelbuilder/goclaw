package bot

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/common"
	"github.com/nextlevelbuilder/goclaw/internal/config"
)

func newWebhookTestChannel(t *testing.T, secret string) (*Channel, *bus.MessageBus) {
	t.Helper()
	mb := bus.New()
	ch, err := New(config.ZaloConfig{
		Token:         "tok",
		Transport:     "webhook",
		WebhookSecret: secret,
		DMPolicy:      "open",
	}, mb, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ch.botID = "bot-self"
	return ch, mb
}

func TestBotSignatureVerifier_RejectsEmptySecret(t *testing.T) {
	v := botSignatureVerifier{secret: ""}
	err := v.Verify(http.Header{"X-Bot-Api-Secret-Token": []string{"anything"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "secret unset") {
		t.Errorf("err = %v, want secret-unset rejection", err)
	}
}

func TestBotSignatureVerifier_RejectsMissingHeader(t *testing.T) {
	v := botSignatureVerifier{secret: "s3cret"}
	if err := v.Verify(http.Header{}, nil); err == nil {
		t.Error("missing header should be rejected")
	}
}

func TestBotSignatureVerifier_RejectsWrongSecret(t *testing.T) {
	v := botSignatureVerifier{secret: "right"}
	err := v.Verify(http.Header{"X-Bot-Api-Secret-Token": []string{"wrong"}}, nil)
	if !errors.Is(err, common.ErrSignatureMismatch) {
		t.Errorf("err = %v, want ErrSignatureMismatch", err)
	}
}

func TestBotSignatureVerifier_AcceptsMatchingSecret(t *testing.T) {
	v := botSignatureVerifier{secret: "s3cret"}
	if err := v.Verify(http.Header{"X-Bot-Api-Secret-Token": []string{"s3cret"}}, nil); err != nil {
		t.Errorf("err = %v, want nil", err)
	}
}

func TestBotMessageIDExtractor(t *testing.T) {
	e := botMessageIDExtractor{}
	got := e.ExtractMessageID(json.RawMessage(`{"ok":true,"result":{"event_name":"x","message":{"message_id":"m123"}}}`))
	if got != "m123" {
		t.Errorf("got %q, want m123", got)
	}
	if got := e.ExtractMessageID(json.RawMessage(`{"event_name":"x","message":{"message_id":"m123"}}`)); got != "m123" {
		t.Errorf("unwrapped payload (Zalo webhook shape) should also extract: got %q", got)
	}
	if e.ExtractMessageID(json.RawMessage(`{}`)) != "" {
		t.Error("missing message_id should yield empty string")
	}
	if e.ExtractMessageID(json.RawMessage(`not-json`)) != "" {
		t.Error("invalid JSON should yield empty string, not panic")
	}
}

func TestHandleWebhookEvent_DispatchesToBus(t *testing.T) {
	ch, mb := newWebhookTestChannel(t, "s3cret")
	payload := `{"ok":true,"result":{"event_name":"message.text.received","message":{"message_id":"m1","text":"hi","from":{"id":"alice"},"chat":{"id":"alice"}}}}`
	if err := ch.HandleWebhookEvent(context.Background(), json.RawMessage(payload)); err != nil {
		t.Fatalf("HandleWebhookEvent: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	got, ok := mb.ConsumeInbound(ctx)
	if !ok {
		t.Fatal("no inbound message published within deadline")
	}
	if got.Content != "hi" {
		t.Errorf("content = %q, want hi", got.Content)
	}
}

func TestHandleWebhookEvent_FiltersSelfEcho(t *testing.T) {
	ch, mb := newWebhookTestChannel(t, "s3cret")
	payload := `{"ok":true,"result":{"event_name":"message.text.received","message":{"message_id":"m1","text":"echo","from":{"id":"bot-self"},"chat":{"id":"someone"}}}}`
	if err := ch.HandleWebhookEvent(context.Background(), json.RawMessage(payload)); err != nil {
		t.Fatalf("HandleWebhookEvent: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, ok := mb.ConsumeInbound(ctx); ok {
		t.Error("self-echo should not have published an inbound message")
	}
}

// Fixture verbatim from https://bot.zapps.me/docs/apis/webhook.
func TestHandleWebhookEvent_DocsSamplePayload(t *testing.T) {
	ch, mb := newWebhookTestChannel(t, "s3cret")
	payload := `{
		"ok": true,
		"result": {
			"message": {
				"from": {"id": "6ede9afa66b88fe6d6a9", "display_name": "Ted", "is_bot": false},
				"chat": {"id": "6ede9afa66b88fe6d6a9", "chat_type": "PRIVATE"},
				"text": "Xin chào",
				"message_id": "2d758cb5e222177a4e35",
				"date": 1750316131602
			},
			"event_name": "message.text.received"
		}
	}`
	if err := ch.HandleWebhookEvent(context.Background(), json.RawMessage(payload)); err != nil {
		t.Fatalf("HandleWebhookEvent: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	got, ok := mb.ConsumeInbound(ctx)
	if !ok {
		t.Fatal("docs-sample payload did not publish an inbound message")
	}
	if got.Content != "Xin chào" {
		t.Errorf("content = %q, want Xin chào", got.Content)
	}
}

func TestHandleWebhookEvent_DropsWhenOkFalse(t *testing.T) {
	ch, mb := newWebhookTestChannel(t, "s3cret")
	payload := `{"ok":false,"description":"some error"}`
	if err := ch.HandleWebhookEvent(context.Background(), json.RawMessage(payload)); err != nil {
		t.Fatalf("HandleWebhookEvent: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, ok := mb.ConsumeInbound(ctx); ok {
		t.Error("ok=false envelope should not dispatch")
	}
}

func TestHandleWebhookEvent_BadJSONReturnsError(t *testing.T) {
	ch, _ := newWebhookTestChannel(t, "s3cret")
	if err := ch.HandleWebhookEvent(context.Background(), json.RawMessage(`not-json`)); err == nil {
		t.Error("invalid JSON should return error")
	}
}

func TestStart_WebhookWithoutSecret_EntersBootstrap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"result":{"id":"bot-xyz","display_name":"TestBot"}}`))
	}))
	defer srv.Close()
	swapAPIBase(t, srv.URL)

	mb := bus.New()
	ch, err := New(config.ZaloConfig{
		Token:       "tok",
		Transport:   "webhook",
		WebhookPath: "bot-bootstrap",
		// no WebhookSecret
	}, mb, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ch.webhookRouter = common.NewRouter()
	ch.instanceID = uuid.New()

	if err := ch.Start(context.Background()); err != nil {
		t.Fatalf("Start without webhook_secret should not return error in bootstrap; got %v", err)
	}
	if !ch.inBootstrap() {
		t.Error("channel should report inBootstrap()=true when secret missing")
	}
	if ch.ResolvedWebhookSlug() == "" {
		t.Error("slug should be registered even in bootstrap so setWebhook ping succeeds")
	}
}

func TestBootstrap_VerifierAcceptsAnything_HandlerDrops(t *testing.T) {
	ch, mb := newWebhookTestChannel(t, "")
	if !ch.inBootstrap() {
		t.Fatalf("setup expected inBootstrap()=true with empty secret")
	}

	if err := ch.SignatureVerifier().Verify(http.Header{}, nil); err != nil {
		t.Errorf("bootstrap verifier should accept missing header; got %v", err)
	}
	if err := ch.SignatureVerifier().Verify(http.Header{"X-Bot-Api-Secret-Token": []string{"anything"}}, nil); err != nil {
		t.Errorf("bootstrap verifier should accept arbitrary token; got %v", err)
	}

	payload := `{"ok":true,"result":{"event_name":"message.text.received","message":{"message_id":"m1","text":"hi","from":{"id":"alice"},"chat":{"id":"alice"}}}}`
	if err := ch.HandleWebhookEvent(context.Background(), json.RawMessage(payload)); err != nil {
		t.Fatalf("HandleWebhookEvent in bootstrap: %v", err)
	}
	if got := ch.BootstrapDroppedForTest(); got != 1 {
		t.Errorf("dropped count = %d, want 1", got)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, ok := mb.ConsumeInbound(ctx); ok {
		t.Error("bootstrap event should not be dispatched to the bus")
	}
}
