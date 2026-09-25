package oa

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/common"
	"github.com/nextlevelbuilder/goclaw/internal/config"
)

func TestCallbackURLUsesPublicGatewayBase(t *testing.T) {
	got, err := CallbackURL("https://gateway.example/")
	if err != nil {
		t.Fatalf("CallbackURL: %v", err)
	}
	want := "https://gateway.example/oauth/zalo/callback"
	if got != want {
		t.Fatalf("CallbackURL = %q, want %q", got, want)
	}
}

func TestCallbackURLRequiresPublicBase(t *testing.T) {
	if _, err := CallbackURL(""); err == nil {
		t.Fatal("expected error for empty public base")
	}
}

func TestWebhookURLUsesConfiguredSlug(t *testing.T) {
	got, err := WebhookURL("https://gateway.example/", "registered-slug", "ignored-name")
	if err != nil {
		t.Fatalf("WebhookURL: %v", err)
	}
	want := "https://gateway.example/channels/zalo/webhook/registered-slug"
	if got != want {
		t.Fatalf("WebhookURL = %q, want %q", got, want)
	}
}

func TestWebhookURLDerivesSlugFromInstanceName(t *testing.T) {
	got, err := WebhookURL("https://gateway.example", "", "Zalo OA Sales")
	if err != nil {
		t.Fatalf("WebhookURL: %v", err)
	}
	want := "https://gateway.example/channels/zalo/webhook/zalo-oa-sales"
	if got != want {
		t.Fatalf("WebhookURL = %q, want %q", got, want)
	}
}

func TestStartWebhookRegistersRouteBeforeConsent(t *testing.T) {
	ch, err := New("Zalo OA Preconsent", config.ZaloOAConfig{Transport: "webhook"},
		&ChannelCreds{AppID: "app", SecretKey: "secret"},
		&noopCIStore{}, bus.New(), nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ch.instanceID = uuid.New()
	ch.webhookRouter = common.NewRouter()
	if err := ch.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = ch.Stop(context.Background()) })

	if got, want := ch.ResolvedWebhookSlug(), "zalo-oa-preconsent"; got != want {
		t.Fatalf("resolved webhook slug = %q, want %q", got, want)
	}
	req := httptest.NewRequest(http.MethodPost,
		common.WebhookPathPrefix+ch.ResolvedWebhookSlug(),
		bytes.NewBufferString(`{"event_name":"oa_webhook_verify"}`))
	rr := httptest.NewRecorder()
	ch.webhookRouter.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("pre-consent webhook status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
}

func TestHandleWebhookEventDropsVerifiedEventsBeforeConsent(t *testing.T) {
	ch, err := New("preconsent", config.ZaloOAConfig{
		Transport:            "webhook",
		WebhookSignatureMode: string(SignatureModeStrict),
	}, &ChannelCreds{
		AppID:            "app",
		SecretKey:        "secret",
		WebhookSecretKey: "webhook-secret",
	}, &noopCIStore{}, bus.New(), nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	raw := []byte(`{"event_name":"user_send_text","sender":{"id":"u1"},"message":{"message_id":"m1","text":"hi"}}`)
	if err := ch.HandleWebhookEvent(context.Background(), raw); err != nil {
		t.Fatalf("pre-consent drop must not error: %v", err)
	}
	if got := ch.BootstrapDroppedForTest(); got != 1 {
		t.Fatalf("pre-consent dropped count = %d, want 1", got)
	}
}

func TestWebhookURLRequiresPublicBase(t *testing.T) {
	if _, err := WebhookURL("", "slug", "name"); err == nil {
		t.Fatal("WebhookURL must reject an empty public base URL")
	}
}

func TestNewWebhookDefaultsToStrictSignatureVerification(t *testing.T) {
	ch, err := New("test-oa", config.ZaloOAConfig{Transport: "webhook"},
		&ChannelCreds{AppID: "app", SecretKey: "secret"},
		&noopCIStore{}, bus.New(), nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := normalizeMode(ch.cfg.WebhookSignatureMode); got != SignatureModeStrict {
		t.Fatalf("signature mode = %q, want %q", got, SignatureModeStrict)
	}
	if !ch.inBootstrap() {
		t.Fatal("webhook channel without a webhook secret must enter bootstrap")
	}
}

func TestNewWebhookPreservesExplicitDisabledSignatureMode(t *testing.T) {
	ch, err := New("test-oa", config.ZaloOAConfig{
		Transport:            "webhook",
		WebhookSignatureMode: string(SignatureModeDisabled),
	}, &ChannelCreds{AppID: "app", SecretKey: "secret"},
		&noopCIStore{}, bus.New(), nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := normalizeMode(ch.cfg.WebhookSignatureMode); got != SignatureModeDisabled {
		t.Fatalf("signature mode = %q, want explicit %q", got, SignatureModeDisabled)
	}
	if ch.inBootstrap() {
		t.Fatal("explicit disabled signature mode must not enter bootstrap")
	}
}

// TestNormalizeModeFailsClosedOnGarbageValue guards the public webhook
// endpoint's fail-closed posture: a corrupted or stale persisted value
// (e.g. a typo, or an enum member removed in a future release) must not
// silently fall back to accepting unsigned payloads the way an unset ("")
// value intentionally does for non-enforcing transports.
func TestNormalizeModeFailsClosedOnGarbageValue(t *testing.T) {
	for _, garbage := range []string{"strcit", "STRICT", "Disabled", "none", " "} {
		if got := normalizeMode(garbage); got != SignatureModeStrict {
			t.Errorf("normalizeMode(%q) = %q, want %q", garbage, got, SignatureModeStrict)
		}
	}
	if got := normalizeMode(""); got != SignatureModeDisabled {
		t.Errorf(`normalizeMode("") = %q, want %q`, got, SignatureModeDisabled)
	}
}
