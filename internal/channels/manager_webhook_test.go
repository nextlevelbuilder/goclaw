package channels

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// mockWebhookChannel implements Channel + WebhookChannel for testing.
type mockWebhookChannel struct {
	name    string
	path    string
	handler http.Handler
}

func (m *mockWebhookChannel) Name() string                                              { return m.name }
func (m *mockWebhookChannel) Type() string                                              { return "mock" }
func (m *mockWebhookChannel) Start(context.Context) error                               { return nil }
func (m *mockWebhookChannel) Stop(context.Context) error                                { return nil }
func (m *mockWebhookChannel) Send(context.Context, bus.OutboundMessage) error            { return nil }
func (m *mockWebhookChannel) IsRunning() bool                                           { return true }
func (m *mockWebhookChannel) IsAllowed(string) bool                                     { return true }
func (m *mockWebhookChannel) WebhookHandler() (string, http.Handler)                    { return m.path, m.handler }

func TestWebhookRouteAfterDynamicRegistration(t *testing.T) {
	msgBus := bus.New()
	mgr := NewManager(msgBus)
	mux := http.NewServeMux()

	// Initially no channels — nothing to mount.
	mgr.MountNewWebhookRoutes(mux)

	// Register a webhook channel.
	ch1 := &mockWebhookChannel{
		name: "test-ch1",
		path: "/test/webhook",
		handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("ok"))
		}),
	}
	mgr.RegisterChannel(ch1.name, ch1)

	// Mount the new path.
	mgr.MountNewWebhookRoutes(mux)

	// Verify the route works.
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/test/webhook", nil)
	mux.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != "ok" {
		t.Fatalf("expected body 'ok', got %q", w.Body.String())
	}

	// Test dynamic handler update: unregister old, register new with same path but different response.
	mgr.UnregisterChannel(ch1.name)
	ch2 := &mockWebhookChannel{
		name: "test-ch2",
		path: "/test/webhook",
		handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("updated"))
		}),
	}
	mgr.RegisterChannel(ch2.name, ch2)

	// Do NOT call MountNewWebhookRoutes — path already mounted, delegation is dynamic.
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest("POST", "/test/webhook", nil)
	mux.ServeHTTP(w2, r2)
	if w2.Code != 200 {
		t.Fatalf("expected 200 after update, got %d", w2.Code)
	}
	if w2.Body.String() != "updated" {
		t.Fatalf("expected body 'updated', got %q", w2.Body.String())
	}
}

func TestWebhookServeHTTP_NotFound(t *testing.T) {
	msgBus := bus.New()
	mgr := NewManager(msgBus)
	mux := http.NewServeMux()

	// Mount with no channels — path registered but no handler in webhookMap.
	ch := &mockWebhookChannel{
		name: "temp",
		path: "/temp/webhook",
		handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("temp"))
		}),
	}
	mgr.RegisterChannel(ch.name, ch)
	mgr.MountNewWebhookRoutes(mux)

	// Remove the channel — webhookMap cleared, but mux route still exists.
	mgr.UnregisterChannel(ch.name)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/temp/webhook", nil)
	mux.ServeHTTP(w, r)
	if w.Code != 404 {
		t.Fatalf("expected 404 after unregister, got %d", w.Code)
	}
}
