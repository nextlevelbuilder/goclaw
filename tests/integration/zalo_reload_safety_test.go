//go:build integration

package integration

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/common"
	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/oa"
	"github.com/nextlevelbuilder/goclaw/internal/config"
)

// TestZaloWebhook_MountRouteIdempotentAcrossReload exercises the load-bearing
// invariant of the WebhookChannel collapse: once a path is mounted via
// MountRoute(), no subsequent caller (different channel instance, post-Reload
// re-registration, etc.) ever gets a non-empty path again. http.ServeMux
// panics on duplicate registration, so this is the safety net the entire
// design depends on.
//
// Setup mirrors the Reload path: register an OA instance, mount the route
// once, then unregister + re-register (simulating instance_loader.Reload's
// Stop→Start cycle). The route handler must still dispatch and the second
// MountRoute call must return ("", nil).
func TestZaloWebhook_MountRouteIdempotentAcrossReload(t *testing.T) {
	router := common.NewRouter()
	mux := http.NewServeMux()
	mux.Handle(common.WebhookPathPrefix, router)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	msgBus := bus.New()

	// First MountRoute — must claim the path.
	path1, h1 := router.MountRoute()
	if path1 != common.WebhookPathPrefix || h1 != router {
		t.Fatalf("first MountRoute = (%q, %v), want (%q, router)", path1, h1, common.WebhookPathPrefix)
	}

	// Register an OA instance, send a signed event, drain inbound — proves
	// dispatch works through the freshly-mounted route.
	tenantID := uuid.New()
	instID := uuid.New()
	secret := "reload-secret"
	creds := &oa.ChannelCreds{
		AppID: "oa-app", SecretKey: "oa-sk", OAID: "oa-mt",
		AccessToken: "AT", RefreshToken: "RT", ExpiresAt: time.Now().Add(time.Hour),
		WebhookSecretKey: secret,
	}
	cfg := config.ZaloOAConfig{
		Transport:                  "webhook",
		WebhookSignatureMode:       "strict",
		WebhookReplayWindowSeconds: 300,
	}
	ch, err := oa.New("oa-reload", cfg, creds, &oaIntegrationStubStore{}, msgBus, nil)
	if err != nil {
		t.Fatalf("oa.New: %v", err)
	}
	ch.SetInstanceID(instID)
	ch.SetTenantID(tenantID)
	const slug = "oa-reload"
	if err := router.RegisterInstance(instID, ch, tenantID, slug); err != nil {
		t.Fatalf("RegisterInstance: %v", err)
	}

	body, sig := buildSignedOAEvent(t, "oa-app", "oa-mt", "user-r1", "before-reload", secret)
	resp, err := postWebhook(t, srv.URL, slug, http.Header{
		"X-Zevent-Signature": []string{sig},
		"Content-Type":       []string{"application/json"},
	}, body)
	if err != nil {
		t.Fatalf("pre-reload POST: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pre-reload status = %d, want 200", resp.StatusCode)
	}
	msg, ok := drainOneInbound(t, msgBus, time.Second)
	if !ok || msg.Content != "before-reload" {
		t.Fatalf("pre-reload inbound: got=%v ok=%v, want before-reload", msg, ok)
	}

	// Simulate instance_loader.Reload: unregister the instance, then
	// re-register a fresh channel under the same UUID. Critically, the
	// route was already mounted once; the second MountRoute MUST stay
	// silent so a cold-path re-mount cannot panic the mux.
	router.UnregisterInstance(instID)

	path2, h2 := router.MountRoute()
	if path2 != "" || h2 != nil {
		t.Fatalf("second MountRoute after Unregister = (%q, %v), want (\"\", nil) — re-mount would panic the mux", path2, h2)
	}

	ch2, err := oa.New("oa-reload-2", cfg, creds, &oaIntegrationStubStore{}, msgBus, nil)
	if err != nil {
		t.Fatalf("oa.New (post-reload): %v", err)
	}
	ch2.SetInstanceID(instID)
	ch2.SetTenantID(tenantID)
	if err := router.RegisterInstance(instID, ch2, tenantID, slug); err != nil {
		t.Fatalf("RegisterInstance (post-reload): %v", err)
	}
	t.Cleanup(func() { router.UnregisterInstance(instID) })

	// Dispatch through the same route still works post-reload.
	body2, sig2 := buildSignedOAEvent(t, "oa-app", "oa-mt", "user-r2", "after-reload", secret)
	resp, err = postWebhook(t, srv.URL, slug, http.Header{
		"X-Zevent-Signature": []string{sig2},
		"Content-Type":       []string{"application/json"},
	}, body2)
	if err != nil {
		t.Fatalf("post-reload POST: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("post-reload status = %d, want 200", resp.StatusCode)
	}
	msg, ok = drainOneInbound(t, msgBus, time.Second)
	if !ok || msg.Content != "after-reload" {
		t.Fatalf("post-reload inbound: got=%v ok=%v, want after-reload", msg, ok)
	}
}
