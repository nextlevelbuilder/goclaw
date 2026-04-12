package http

import (
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// TestSkillsHandler_EmitCacheInvalidate_TenantScopedPropagatesTenantID
// verifies tenant-config handlers emit payloads carrying the caller's tenant.
func TestSkillsHandler_EmitCacheInvalidate_TenantScopedPropagatesTenantID(t *testing.T) {
	mb := bus.New()
	defer mb.Close()

	h := &SkillsHandler{msgBus: mb}

	var captured bus.CacheInvalidatePayload
	var gotEvent bool
	mb.Subscribe("test-capture", func(e bus.Event) {
		if e.Name != protocol.EventCacheInvalidate {
			return
		}
		if p, ok := e.Payload.(bus.CacheInvalidatePayload); ok {
			captured = p
			gotEvent = true
		}
	})

	tid := uuid.New()
	skillID := uuid.New().String()
	h.emitCacheInvalidate(bus.CacheKindSkills, skillID, tid)

	if !gotEvent {
		t.Fatal("no cache invalidate event received")
	}
	if captured.Kind != bus.CacheKindSkills {
		t.Errorf("kind = %q, want %q", captured.Kind, bus.CacheKindSkills)
	}
	if captured.Key != skillID {
		t.Errorf("key = %q, want %q", captured.Key, skillID)
	}
	if captured.TenantID != tid {
		t.Errorf("tenantID = %v, want %v", captured.TenantID, tid)
	}
}

// TestSkillsHandler_EmitCacheInvalidate_GrantsGlobal verifies grant-change
// callers pass uuid.Nil so subscribers treat the event as global.
func TestSkillsHandler_EmitCacheInvalidate_GrantsGlobal(t *testing.T) {
	mb := bus.New()
	defer mb.Close()

	h := &SkillsHandler{msgBus: mb}

	var captured bus.CacheInvalidatePayload
	mb.Subscribe("test-capture", func(e bus.Event) {
		if p, ok := e.Payload.(bus.CacheInvalidatePayload); ok {
			captured = p
		}
	})

	h.emitCacheInvalidate(bus.CacheKindSkillGrants, "", uuid.Nil)

	if captured.Kind != bus.CacheKindSkillGrants {
		t.Errorf("kind = %q, want %q", captured.Kind, bus.CacheKindSkillGrants)
	}
	if captured.TenantID != uuid.Nil {
		t.Errorf("tenantID = %v, want uuid.Nil for grant-change global event", captured.TenantID)
	}
}

// TestSkillsHandler_EmitCacheInvalidate_NilMsgBusNoop verifies the helper is
// safe with an unset msgBus.
func TestSkillsHandler_EmitCacheInvalidate_NilMsgBusNoop(t *testing.T) {
	h := &SkillsHandler{msgBus: nil}
	// Must not panic.
	h.emitCacheInvalidate(bus.CacheKindSkills, "x", uuid.New())
}
