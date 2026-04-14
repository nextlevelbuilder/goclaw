package http

import (
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// TestBuiltinToolsHandler_EmitCacheInvalidate_CarriesTenantID verifies the
// emit helper populates TenantID in the broadcast payload so downstream
// subscribers can branch on tenant-scoped vs global invalidation.
func TestBuiltinToolsHandler_EmitCacheInvalidate_CarriesTenantID(t *testing.T) {
	mb := bus.New()
	defer mb.Close()

	h := &BuiltinToolsHandler{msgBus: mb}

	var captured bus.CacheInvalidatePayload
	var gotEvent bool
	mb.Subscribe("test-capture", func(e bus.Event) {
		if e.Name != protocol.EventCacheInvalidate {
			return
		}
		p, ok := e.Payload.(bus.CacheInvalidatePayload)
		if !ok {
			return
		}
		captured = p
		gotEvent = true
	})

	tid := uuid.New()
	h.emitCacheInvalidate("tts", tid)

	if !gotEvent {
		t.Fatal("no cache invalidate event received")
	}
	if captured.Kind != bus.CacheKindBuiltinTools {
		t.Errorf("kind = %q, want %q", captured.Kind, bus.CacheKindBuiltinTools)
	}
	if captured.Key != "tts" {
		t.Errorf("key = %q, want %q", captured.Key, "tts")
	}
	if captured.TenantID != tid {
		t.Errorf("tenantID = %v, want %v", captured.TenantID, tid)
	}
}

// TestBuiltinToolsHandler_EmitCacheInvalidate_NilTenantMeansGlobal verifies
// passing uuid.Nil yields a payload whose subscribers read as "global".
func TestBuiltinToolsHandler_EmitCacheInvalidate_NilTenantMeansGlobal(t *testing.T) {
	mb := bus.New()
	defer mb.Close()

	h := &BuiltinToolsHandler{msgBus: mb}

	var captured bus.CacheInvalidatePayload
	mb.Subscribe("test-capture", func(e bus.Event) {
		if p, ok := e.Payload.(bus.CacheInvalidatePayload); ok {
			captured = p
		}
	})

	h.emitCacheInvalidate("tts", uuid.Nil)

	if captured.TenantID != uuid.Nil {
		t.Errorf("tenantID = %v, want uuid.Nil for global invalidation", captured.TenantID)
	}
}

// TestBuiltinToolsHandler_EmitCacheInvalidate_NilMsgBusNoop verifies the
// helper is safe to call when msgBus is unset (e.g., in tests/desktop lite).
func TestBuiltinToolsHandler_EmitCacheInvalidate_NilMsgBusNoop(t *testing.T) {
	h := &BuiltinToolsHandler{msgBus: nil}
	// Must not panic.
	h.emitCacheInvalidate("tts", uuid.New())
}
