package facebook

// TestTenantContext_* tests demonstrate the tenant context propagation bug that
// was fixed in the facebook channel handlers.
//
// Bug: handleCommentEvent and handleMessagingEvent were invoked with a bare
// context.Context derived from the HTTP request (r.Context() in webhook_handler.go).
// That context carried no tenant_id. Any ctx-aware downstream store call would
// fall back to uuid.Nil / MasterTenantID instead of the channel's tenant.
//
// Fix: both handlers now call ctx = store.WithTenantID(ctx, ch.TenantID())
// as their very first statement.
//
// These tests pass on the fixed code. A regression (removing the fix) would
// cause the tenant to be lost at the handler boundary, breaking multi-tenant
// isolation for all Facebook channel store calls.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// TestTenantContext_FacebookCommentHandlerPropagatesTenantID verifies that
// handleCommentEvent publishes an InboundMessage whose TenantID matches the
// channel's configured tenant, even when called with a bare context.Background().
func TestTenantContext_FacebookCommentHandlerPropagatesTenantID(t *testing.T) {
	t.Helper()

	tenantID := uuid.MustParse("01930000-fb00-7000-8000-000000000011")

	// Reset global router to avoid cross-test interference.
	globalRouter = &webhookRouter{instances: make(map[string]*Channel)}

	cfg := facebookInstanceConfig{}
	cfg.Features.CommentReply = true
	ch := newTestChannel(t, "fb-page-1", cfg)
	ch.SetTenantID(tenantID)

	// Call handler with a bare context (simulates r.Context() from an HTTP webhook).
	ch.handleCommentEvent(context.Background(),
		WebhookEntry{ID: "fb-page-1"},
		ChangeValue{
			Verb:      "add",
			CommentID: "comment-tenant-1",
			PostID:    "post-123",
			From:      FBUser{ID: "external-user-1", Name: "Alice"},
			Message:   "Hello from tenant",
		},
	)

	// Consume from the bus embedded in the channel via BaseChannel.Bus().
	msgBus := ch.Bus()
	if msgBus == nil {
		t.Fatal("channel bus is nil — test setup error")
	}

	ctxTimeout, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	msg, ok := msgBus.ConsumeInbound(ctxTimeout)
	if !ok {
		t.Fatal("expected InboundMessage on bus, got none")
	}

	if msg.TenantID == uuid.Nil {
		t.Fatalf("InboundMessage.TenantID is uuid.Nil: tenant context was not propagated; want %s", tenantID)
	}
	if msg.TenantID != tenantID {
		t.Fatalf("InboundMessage.TenantID = %s, want %s", msg.TenantID, tenantID)
	}
}

// TestTenantContext_FacebookMessengerHandlerPropagatesTenantID verifies the
// same tenant propagation for handleMessagingEvent.
func TestTenantContext_FacebookMessengerHandlerPropagatesTenantID(t *testing.T) {
	t.Helper()

	tenantID := uuid.MustParse("01930000-fb00-7000-8000-000000000022")

	globalRouter = &webhookRouter{instances: make(map[string]*Channel)}

	cfg := facebookInstanceConfig{}
	cfg.Features.MessengerAutoReply = true
	ch := newTestChannel(t, "fb-page-2", cfg)
	ch.SetTenantID(tenantID)

	ch.handleMessagingEvent(context.Background(),
		WebhookEntry{ID: "fb-page-2"},
		MessagingEvent{
			Sender:  FBUser{ID: "messenger-user-1"},
			Message: &IncomingMessage{MID: "mid-tenant-1", Text: "Hello from messenger"},
		},
	)

	msgBus := ch.Bus()
	if msgBus == nil {
		t.Fatal("channel bus is nil — test setup error")
	}

	ctxTimeout, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	msg, ok := msgBus.ConsumeInbound(ctxTimeout)
	if !ok {
		t.Fatal("expected InboundMessage on bus, got none")
	}

	if msg.TenantID == uuid.Nil {
		t.Fatalf("InboundMessage.TenantID is uuid.Nil: tenant context was not propagated; want %s", tenantID)
	}
	if msg.TenantID != tenantID {
		t.Fatalf("InboundMessage.TenantID = %s, want %s", msg.TenantID, tenantID)
	}
}

// TestTenantContext_FacebookEnrichmentContract directly validates the exact fix
// applied to both handlers: ctx = store.WithTenantID(ctx, ch.TenantID()).
//
// Removing this line from either handler would cause store.TenantIDFromContext
// to return uuid.Nil for all downstream store calls in that handler, silently
// scoping DB writes to the wrong tenant (MasterTenantID fallback).
func TestTenantContext_FacebookEnrichmentContract(t *testing.T) {
	t.Helper()

	tenantID := uuid.MustParse("01930000-fb00-7000-8000-aabb00000001")

	globalRouter = &webhookRouter{instances: make(map[string]*Channel)}

	cfg := facebookInstanceConfig{}
	ch := newTestChannel(t, "fb-page-e", cfg)
	ch.SetTenantID(tenantID)

	bareCtx := context.Background()

	// Precondition: bare context has no tenant.
	if got := store.TenantIDFromContext(bareCtx); got != uuid.Nil {
		t.Fatalf("precondition: bare context should have uuid.Nil tenant, got %s", got)
	}

	// Apply the fix (what both handlers do as first statement).
	enrichedCtx := store.WithTenantID(bareCtx, ch.TenantID())

	got := store.TenantIDFromContext(enrichedCtx)
	if got != tenantID {
		t.Fatalf("enriched context TenantID = %s, want %s", got, tenantID)
	}
}

// TestTenantContext_FacebookMultiTenantIsolation verifies that two Facebook
// channel instances with different tenants do not cross-contaminate each other
// when handlers are called with bare contexts.
func TestTenantContext_FacebookMultiTenantIsolation(t *testing.T) {
	t.Helper()

	globalRouter = &webhookRouter{instances: make(map[string]*Channel)}

	tenantA := uuid.MustParse("01930000-fb00-7000-8000-aaaaaaaaaa01")
	tenantB := uuid.MustParse("01930000-fb00-7000-8000-bbbbbbbbbb02")

	cfgA := facebookInstanceConfig{}
	cfgA.Features.CommentReply = true
	chA := newTestChannel(t, "page-A", cfgA)
	chA.SetTenantID(tenantA)

	cfgB := facebookInstanceConfig{}
	cfgB.Features.CommentReply = true
	chB := newTestChannel(t, "page-B", cfgB)
	chB.SetTenantID(tenantB)

	busA := chA.Bus()
	busB := chB.Bus()
	if busA == nil || busB == nil {
		t.Fatal("channel bus is nil — test setup error")
	}

	chA.handleCommentEvent(context.Background(),
		WebhookEntry{ID: "page-A"},
		ChangeValue{Verb: "add", CommentID: "c-a1", From: FBUser{ID: "u1"}, PostID: "p1", Message: "hello A"},
	)
	chB.handleCommentEvent(context.Background(),
		WebhookEntry{ID: "page-B"},
		ChangeValue{Verb: "add", CommentID: "c-b1", From: FBUser{ID: "u2"}, PostID: "p2", Message: "hello B"},
	)

	timeout := 200 * time.Millisecond

	ctxA, cancelA := context.WithTimeout(context.Background(), timeout)
	defer cancelA()
	msgA, okA := busA.ConsumeInbound(ctxA)
	if !okA {
		t.Fatal("expected message from channel A bus")
	}

	ctxB, cancelB := context.WithTimeout(context.Background(), timeout)
	defer cancelB()
	msgB, okB := busB.ConsumeInbound(ctxB)
	if !okB {
		t.Fatal("expected message from channel B bus")
	}

	if msgA.TenantID != tenantA {
		t.Errorf("channel A: TenantID = %s, want %s", msgA.TenantID, tenantA)
	}
	if msgB.TenantID != tenantB {
		t.Errorf("channel B: TenantID = %s, want %s", msgB.TenantID, tenantB)
	}
	if msgA.TenantID == msgB.TenantID {
		t.Error("multi-tenant isolation broken: both channels published same TenantID")
	}
}
