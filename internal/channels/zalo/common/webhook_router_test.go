package common

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type stubWebhookHandler struct{}

func (stubWebhookHandler) HandleWebhookEvent(context.Context, json.RawMessage) error { return nil }
func (stubWebhookHandler) SignatureVerifier() SignatureVerifier                      { return stubVerifier{} }
func (stubWebhookHandler) MessageIDExtractor() MessageIDExtractor                    { return stubExtractor{} }

type stubVerifier struct{}

func (stubVerifier) Verify(http.Header, []byte) error { return nil }

type stubExtractor struct{}

func (stubExtractor) ExtractMessageID(json.RawMessage) string { return "mid-1" }

func TestRouter_RejectsBodyOverLimit(t *testing.T) {
	r := NewRouter()
	r.maxBodySize = 16
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	if err := r.RegisterInstance(id, stubWebhookHandler{}, uuid.Nil, "salesoa"); err != nil {
		t.Fatalf("register: %v", err)
	}
	t.Cleanup(func() { r.UnregisterInstance(id) })

	req := httptest.NewRequest(http.MethodPost, WebhookPathPrefix+"salesoa", strings.NewReader(strings.Repeat("x", 18)))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
}

func TestRouter_MismatchReleasesDispatchSlot(t *testing.T) {
	r := NewRouter()
	id := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	if err := r.RegisterInstance(id, stubWebhookHandler{}, uuid.Nil, "salesoa"); err != nil {
		t.Fatalf("register: %v", err)
	}

	_, inst, ok := r.reserveDispatchSlot("salesoa")
	if !ok || inst == nil {
		t.Fatal("expected reserved slot")
	}
	// Simulate ServeHTTP mismatch: reserved instance is not the verified one.
	inst.dispatchWG.Done()

	done := make(chan struct{})
	go func() {
		r.UnregisterInstance(id)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("UnregisterInstance hung — dispatch WaitGroup leak")
	}
}

func TestRouter_ExactLimitBodyAccepted(t *testing.T) {
	r := NewRouter()
	r.maxBodySize = 8
	id := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	if err := r.RegisterInstance(id, stubWebhookHandler{}, uuid.Nil, "salesoa"); err != nil {
		t.Fatalf("register: %v", err)
	}
	t.Cleanup(func() { r.UnregisterInstance(id) })

	req := httptest.NewRequest(http.MethodPost, WebhookPathPrefix+"salesoa", bytes.NewReader([]byte("12345678")))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		body, _ := io.ReadAll(rec.Body)
		t.Fatalf("status = %d body = %s, want 200", rec.Code, body)
	}
}
