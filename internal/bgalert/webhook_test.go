package bgalert

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// resetWebhookState clears the package-level cooldown timestamp so tests run
// deterministically regardless of execution order.
func resetWebhookState() {
	webhookMu.Lock()
	webhookLastSend = time.Time{}
	webhookMu.Unlock()
}

func countServer(t *testing.T, bodyCh chan<- []byte) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var got atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.Add(1)
		if bodyCh != nil {
			body, _ := io.ReadAll(r.Body)
			bodyCh <- body
		}
		w.WriteHeader(http.StatusOK)
	}))
	return srv, &got
}

// TestSendWebhookPayload checks the delivered JSON body: severity for auth,
// title, sanitized message, worker, reason, and timestamp.
func TestSendWebhookPayload(t *testing.T) {
	resetWebhookState()
	bodyCh := make(chan []byte, 1)
	srv, got := countServer(t, bodyCh)
	defer srv.Close()

	err := errors.New("authentication failed: Invalid API key provided: sk-ant-abcdef123456789")
	SendWebhook(context.Background(), AlertDeps{WebhookURL: srv.URL}, "workerA", "auth", err)

	var raw []byte
	select {
	case raw = <-bodyCh:
	case <-time.After(2 * time.Second):
		t.Fatal("webhook server received no request")
	}
	if n := got.Load(); n != 1 {
		t.Fatalf("requests = %d, want 1", n)
	}

	var p WebhookPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if p.Severity != "critical" {
		t.Errorf("severity = %q, want critical", p.Severity)
	}
	if p.Title != "GoClaw background provider error" {
		t.Errorf("title = %q, want %q", p.Title, "GoClaw background provider error")
	}
	if want := "authentication failed: Invalid API key provided: sk-****"; p.Message != want {
		t.Errorf("message = %q, want %q (API key must be masked)", p.Message, want)
	}
	if p.Worker != "workerA" {
		t.Errorf("worker = %q, want workerA", p.Worker)
	}
	if p.Reason != "auth" {
		t.Errorf("reason = %q, want auth", p.Reason)
	}
	if p.Timestamp == "" {
		t.Error("timestamp is empty")
	}
	if p.Meta != nil {
		t.Errorf("meta = %v, want omitted", p.Meta)
	}
}

// TestSendWebhookEmptyURL verifies a blank WebhookURL sends nothing.
func TestSendWebhookEmptyURL(t *testing.T) {
	resetWebhookState()
	srv, got := countServer(t, nil)
	defer srv.Close()

	SendWebhook(context.Background(), AlertDeps{}, "workerB", "billing", errors.New("boom"))
	if n := got.Load(); n != 0 {
		t.Errorf("requests = %d, want 0 (empty URL must not send)", n)
	}
}

// TestSendWebhookServerErrorBestEffort verifies a failing receiver does not
// panic and does not block the caller past the HTTP round-trip.
func TestSendWebhookServerErrorBestEffort(t *testing.T) {
	resetWebhookState()
	var got atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.Add(1)
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	SendWebhook(context.Background(), AlertDeps{WebhookURL: srv.URL}, "workerC", "model_not_found", errors.New("model not found"))
	if n := got.Load(); n != 1 {
		t.Errorf("requests = %d, want 1", n)
	}
}

// TestSendWebhookCooldown verifies the min-interval throttle: two sends within
// a 60s interval result in exactly one HTTP request.
func TestSendWebhookCooldown(t *testing.T) {
	resetWebhookState()
	srv, got := countServer(t, nil)
	defer srv.Close()

	deps := AlertDeps{WebhookURL: srv.URL, MinIntervalSeconds: 60}
	SendWebhook(context.Background(), deps, "workerD", "auth", errors.New("boom"))
	SendWebhook(context.Background(), deps, "workerD", "billing", errors.New("boom2"))
	if n := got.Load(); n != 1 {
		t.Errorf("requests = %d, want 1 (second send within min-interval must be skipped)", n)
	}
}

// TestWebhookSeverityMapping covers the alertable reason → severity mapping.
func TestWebhookSeverityMapping(t *testing.T) {
	cases := []struct {
		reason string
		want   string
	}{
		{"auth", "critical"},
		{"auth_permanent", "critical"},
		{"billing", "critical"},
		{"model_not_found", "critical"},
		{"server_error", "warning"},
		{"unknown", "warning"},
	}
	for _, c := range cases {
		if got := webhookSeverity(c.reason); got != c.want {
			t.Errorf("webhookSeverity(%q) = %q, want %q", c.reason, got, c.want)
		}
	}
}