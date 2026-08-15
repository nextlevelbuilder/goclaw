package agent

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// This file mirrors the subscriber callback bodies registered in
// cmd/gateway_managed.go (TopicCacheBuiltinTools + TopicCacheSkills). Keeping
// the logic here — not in cmd — gives the test direct access to the router's
// private map so we can assert precisely which entries survive tenant-scoped
// vs global invalidation. If the real subscribers in gateway_managed.go
// diverge from these, update both in lockstep.

func handleBuiltinToolsInvalidate(router *Router, onGlobal func(), event bus.Event) {
	if event.Name != protocol.EventCacheInvalidate {
		return
	}
	payload, ok := event.Payload.(bus.CacheInvalidatePayload)
	if !ok || payload.Kind != bus.CacheKindBuiltinTools {
		return
	}
	if payload.TenantID != uuid.Nil {
		router.InvalidateTenant(payload.TenantID)
		return
	}
	if onGlobal != nil {
		onGlobal()
	}
	router.InvalidateAll()
}

func handleSkillsInvalidate(router *Router, onBump func(), event bus.Event) {
	if event.Name != protocol.EventCacheInvalidate {
		return
	}
	payload, ok := event.Payload.(bus.CacheInvalidatePayload)
	if !ok || payload.Kind != bus.CacheKindSkills {
		return
	}
	if onBump != nil {
		onBump()
	}
	if payload.TenantID != uuid.Nil {
		router.InvalidateTenant(payload.TenantID)
		return
	}
	router.InvalidateAll()
}

func seedTenantAgents(t *testing.T, tenantA, tenantB uuid.UUID) (*Router, [3]string) {
	t.Helper()
	r := NewRouter()
	ctxA := store.WithTenantID(context.Background(), tenantA)
	ctxB := store.WithTenantID(context.Background(), tenantB)
	keys := [3]string{
		agentCacheKey(ctxA, "agent-1"),
		agentCacheKey(ctxB, "agent-1"),
		agentCacheKey(context.Background(), "agent-1"),
	}
	for _, k := range keys {
		r.agents[k] = &agentEntry{}
	}
	return r, keys
}

// TestBuiltinToolsSubscriber_TenantEventOnlyInvalidatesThatTenant: the
// tenant branch must leave other tenants and bare entries alone and must
// NOT run the global re-apply callback.
func TestBuiltinToolsSubscriber_TenantEventOnlyInvalidatesThatTenant(t *testing.T) {
	tenantA := uuid.New()
	tenantB := uuid.New()
	r, keys := seedTenantAgents(t, tenantA, tenantB)

	globalCalled := false
	handleBuiltinToolsInvalidate(r, func() { globalCalled = true }, bus.Event{
		Name: protocol.EventCacheInvalidate,
		Payload: bus.CacheInvalidatePayload{
			Kind:     bus.CacheKindBuiltinTools,
			Key:      "tts",
			TenantID: tenantA,
		},
	})

	if globalCalled {
		t.Error("global reapply must NOT run for tenant-scoped event")
	}
	if _, ok := r.agents[keys[0]]; ok {
		t.Error("tenantA entry should have been invalidated")
	}
	if _, ok := r.agents[keys[1]]; !ok {
		t.Error("tenantB entry must survive tenantA invalidation")
	}
	if _, ok := r.agents[keys[2]]; !ok {
		t.Error("bare entry must survive tenantA invalidation")
	}
}

// TestBuiltinToolsSubscriber_GlobalEventInvalidatesAll: the master/global
// branch must wipe everything and run the global re-apply callback.
func TestBuiltinToolsSubscriber_GlobalEventInvalidatesAll(t *testing.T) {
	tenantA := uuid.New()
	tenantB := uuid.New()
	r, _ := seedTenantAgents(t, tenantA, tenantB)

	globalCalled := false
	handleBuiltinToolsInvalidate(r, func() { globalCalled = true }, bus.Event{
		Name: protocol.EventCacheInvalidate,
		Payload: bus.CacheInvalidatePayload{
			Kind: bus.CacheKindBuiltinTools,
			Key:  "tts",
		},
	})

	if !globalCalled {
		t.Error("global reapply must run for master/global event")
	}
	if len(r.agents) != 0 {
		t.Errorf("global event must wipe all entries, got %d remaining", len(r.agents))
	}
}

// TestSkillsSubscriber_BumpsVersionAndInvalidatesTenant: bump runs every
// event; tenant branch only wipes the target tenant.
func TestSkillsSubscriber_BumpsVersionAndInvalidatesTenant(t *testing.T) {
	tenantA := uuid.New()
	tenantB := uuid.New()
	r, keys := seedTenantAgents(t, tenantA, tenantB)

	bumped := false
	handleSkillsInvalidate(r, func() { bumped = true }, bus.Event{
		Name: protocol.EventCacheInvalidate,
		Payload: bus.CacheInvalidatePayload{
			Kind:     bus.CacheKindSkills,
			Key:      uuid.New().String(),
			TenantID: tenantA,
		},
	})

	if !bumped {
		t.Error("skills version must be bumped on every invalidate event")
	}
	if _, ok := r.agents[keys[0]]; ok {
		t.Error("tenantA entry should have been invalidated")
	}
	if _, ok := r.agents[keys[1]]; !ok {
		t.Error("tenantB entry must survive")
	}
}

// TestSkillsSubscriber_IgnoresMismatchedKind: events with a different Kind
// must be ignored entirely (no bump, no router touch).
func TestSkillsSubscriber_IgnoresMismatchedKind(t *testing.T) {
	r, keys := seedTenantAgents(t, uuid.New(), uuid.New())
	bumped := false

	handleSkillsInvalidate(r, func() { bumped = true }, bus.Event{
		Name: protocol.EventCacheInvalidate,
		Payload: bus.CacheInvalidatePayload{
			Kind: bus.CacheKindBuiltinTools, // wrong kind for skills subscriber
			Key:  "x",
		},
	})

	if bumped {
		t.Error("skills subscriber must ignore non-skill events")
	}
	if _, ok := r.agents[keys[0]]; !ok {
		t.Error("router entries must remain untouched on mismatched kind")
	}
}
