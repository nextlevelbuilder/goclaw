package http

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// TestRequireAuth_AuditLoginAndLoginFailed verifies that the HTTP auth path
// emits auth.login on success and auth.login_failed on failure with the
// detected auth method, via the package-level audit bus.
func TestRequireAuth_AuditLoginAndLoginFailed(t *testing.T) {
	setupTestCache(t, nil)
	setupTestToken(t, "secret")

	mb := bus.New()
	InitAuditBus(mb)
	t.Cleanup(func() { InitAuditBus(nil) })

	var mu sync.Mutex
	var events []bus.AuditEventPayload
	mb.Subscribe(bus.TopicAudit, func(evt bus.Event) {
		if evt.Name != protocol.EventAuditLog {
			return
		}
		payload, ok := evt.Payload.(bus.AuditEventPayload)
		if !ok {
			return
		}
		mu.Lock()
		events = append(events, payload)
		mu.Unlock()
	})

	handler := requireAuth("", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Success: bearer token with a user ID.
	r := httptest.NewRequest("GET", "/v1/agents", nil)
	r.Header.Set("Authorization", "Bearer secret")
	r.Header.Set("X-GoClaw-User-Id", "userA")
	w := httptest.NewRecorder()
	handler(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("success status = %d, want 200", w.Code)
	}

	// Failure: no token.
	r2 := httptest.NewRequest("GET", "/v1/agents", nil)
	w2 := httptest.NewRecorder()
	handler(w2, r2)
	if w2.Code != http.StatusUnauthorized {
		t.Fatalf("failure status = %d, want 401", w2.Code)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 2 {
		t.Fatalf("audit events = %d, want 2 (login + login_failed)", len(events))
	}

	var login, failed *bus.AuditEventPayload
	for i := range events {
		switch events[i].Action {
		case "auth.login":
			login = &events[i]
		case "auth.login_failed":
			failed = &events[i]
		}
	}
	if login == nil {
		t.Fatal("no auth.login event emitted")
	}
	if login.EntityType != "auth" {
		t.Errorf("login EntityType = %q, want auth", login.EntityType)
	}
	if login.ActorID != "userA" {
		t.Errorf("login ActorID = %q, want userA", login.ActorID)
	}
	if failed == nil {
		t.Fatal("no auth.login_failed event emitted")
	}
	if failed.EntityID != "none" {
		t.Errorf("login_failed method = %q, want none", failed.EntityID)
	}
	if failed.ActorID == "" {
		t.Error("login_failed ActorID empty, want extracted user ID (fallback system)")
	}
}

// TestRequireAuth_AuditMethodDetection verifies the auth method reported in
// audit events matches the credential type.
func TestRequireAuth_AuditMethodDetection(t *testing.T) {
	setupTestCache(t, nil)
	setupTestToken(t, "secret")

	mb := bus.New()
	InitAuditBus(mb)
	t.Cleanup(func() { InitAuditBus(nil) })

	var mu sync.Mutex
	var methods []string
	mb.Subscribe(bus.TopicAudit, func(evt bus.Event) {
		if evt.Name != protocol.EventAuditLog {
			return
		}
		payload, ok := evt.Payload.(bus.AuditEventPayload)
		if !ok {
			return
		}
		if payload.Action != "auth.login" {
			return
		}
		mu.Lock()
		methods = append(methods, payload.EntityID)
		mu.Unlock()
	})

	handler := requireAuth(permissions.RoleAdmin, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Bearer uses the "bearer" method.
	r := httptest.NewRequest("GET", "/v1/agents", nil)
	r.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	handler(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("bearer status = %d, want 200", w.Code)
	}

	mu.Lock()
	methodsGot := append([]string(nil), methods...)
	mu.Unlock()
	if len(methodsGot) != 1 || methodsGot[0] != "bearer" {
		t.Errorf("methods = %v, want [bearer]", methodsGot)
	}
}