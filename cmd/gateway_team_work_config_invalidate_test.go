package cmd

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/teamworkconfig"
)

// tenantScopedConfigStore returns per-tenant config maps keyed by the tenant in
// context, so a test can prove the resolver re-reads a CHANGED value only after
// the cache is invalidated.
type tenantScopedConfigStore struct {
	mu   sync.Mutex
	byTn map[uuid.UUID]map[string]string
}

func newTenantScopedConfigStore() *tenantScopedConfigStore {
	return &tenantScopedConfigStore{byTn: map[uuid.UUID]map[string]string{}}
}

func (s *tenantScopedConfigStore) set(tid uuid.UUID, m map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byTn[tid] = m
}

func (s *tenantScopedConfigStore) Get(context.Context, string) (string, error) { return "", nil }
func (s *tenantScopedConfigStore) Set(context.Context, string, string) error   { return nil }
func (s *tenantScopedConfigStore) Delete(context.Context, string) error        { return nil }
func (s *tenantScopedConfigStore) List(ctx context.Context) (map[string]string, error) {
	tid := store.TenantIDFromContext(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]string{}
	for k, v := range s.byTn[tid] {
		out[k] = v
	}
	return out, nil
}

// The local bus subscriber (7B-H3) must, on a system-config-changed event whose
// payload carries the changed tenant's context, invalidate exactly that tenant's
// cached Team Work settings so the next Resolve re-reads the store. This is the
// single-gateway-process invalidation path end to end: Broadcast → subscriber →
// resolver.Invalidate → fresh read.
func TestRegisterTeamWorkConfigInvalidator_ReReadsAfterConfigChange(t *testing.T) {
	tid := uuid.New()
	fake := newTenantScopedConfigStore()
	fake.set(tid, map[string]string{teamworkconfig.KeyClassifyProvider: "openai"})
	resolver := teamworkconfig.NewResolver(teamworkconfig.Defaults{ClassifierProvider: "anthropic"}, fake)

	msgBus := bus.New()
	defer msgBus.Close()
	registerTeamWorkConfigInvalidator(msgBus, resolver)

	ctx := store.WithTenantID(context.Background(), tid)
	if got := resolver.Resolve(ctx).ClassifierProvider; got != "openai" {
		t.Fatalf("initial resolve provider = %q, want openai", got)
	}

	// Config changes in the store, then the tenant's change is broadcast.
	fake.set(tid, map[string]string{teamworkconfig.KeyClassifyProvider: "google"})
	msgBus.Broadcast(bus.Event{Name: bus.TopicSystemConfigChanged, Payload: ctx})

	if got := resolver.Resolve(ctx).ClassifierProvider; got != "google" {
		t.Fatalf("post-invalidate resolve provider = %q, want google (subscriber did not invalidate)", got)
	}
}

// The invalidator must be tenant-scoped: a config change broadcast for tenant A
// must NOT drop tenant B's cached settings. If it did, one tenant's config write
// would force every other tenant to re-read on the next request.
func TestRegisterTeamWorkConfigInvalidator_OnlyInvalidatesChangedTenant(t *testing.T) {
	tnA, tnB := uuid.New(), uuid.New()
	fake := newTenantScopedConfigStore()
	fake.set(tnA, map[string]string{teamworkconfig.KeyClassifyProvider: "openai"})
	fake.set(tnB, map[string]string{teamworkconfig.KeyClassifyProvider: "azure"})
	resolver := teamworkconfig.NewResolver(teamworkconfig.Defaults{ClassifierProvider: "anthropic"}, fake)

	msgBus := bus.New()
	defer msgBus.Close()
	registerTeamWorkConfigInvalidator(msgBus, resolver)

	ctxA := store.WithTenantID(context.Background(), tnA)
	ctxB := store.WithTenantID(context.Background(), tnB)
	_ = resolver.Resolve(ctxA) // cache A
	_ = resolver.Resolve(ctxB) // cache B

	// Mutate BOTH tenants in the store, but broadcast only tenant A's change.
	fake.set(tnA, map[string]string{teamworkconfig.KeyClassifyProvider: "google"})
	fake.set(tnB, map[string]string{teamworkconfig.KeyClassifyProvider: "bedrock"})
	msgBus.Broadcast(bus.Event{Name: bus.TopicSystemConfigChanged, Payload: ctxA})

	if got := resolver.Resolve(ctxA).ClassifierProvider; got != "google" {
		t.Fatalf("tenant A provider = %q, want google (A should have been invalidated)", got)
	}
	if got := resolver.Resolve(ctxB).ClassifierProvider; got != "azure" {
		t.Fatalf("tenant B provider = %q, want azure (B must stay cached; only A changed)", got)
	}
}

// The subscriber must ignore events that are not system-config changes. Because
// MessageBus.Broadcast fans every event out to every subscriber with no per-topic
// routing, a name filter is the only thing stopping an unrelated event from
// dropping the cache — this guards that filter.
func TestRegisterTeamWorkConfigInvalidator_IgnoresUnrelatedEvents(t *testing.T) {
	tid := uuid.New()
	fake := newTenantScopedConfigStore()
	fake.set(tid, map[string]string{teamworkconfig.KeyClassifyProvider: "openai"})
	resolver := teamworkconfig.NewResolver(teamworkconfig.Defaults{ClassifierProvider: "anthropic"}, fake)

	msgBus := bus.New()
	defer msgBus.Close()
	registerTeamWorkConfigInvalidator(msgBus, resolver)

	ctx := store.WithTenantID(context.Background(), tid)
	_ = resolver.Resolve(ctx) // cache "openai"

	// Store changes, but an UNRELATED event fires — the cache must NOT be dropped.
	fake.set(tid, map[string]string{teamworkconfig.KeyClassifyProvider: "google"})
	msgBus.Broadcast(bus.Event{Name: bus.TopicConfigChanged, Payload: ctx})

	if got := resolver.Resolve(ctx).ClassifierProvider; got != "openai" {
		t.Fatalf("provider = %q, want openai (unrelated event must not invalidate)", got)
	}
}

// A nil bus or nil resolver must be a safe no-op (partial wiring guard).
func TestRegisterTeamWorkConfigInvalidator_NilArgsSafe(t *testing.T) {
	registerTeamWorkConfigInvalidator(nil, nil)
	msgBus := bus.New()
	defer msgBus.Close()
	registerTeamWorkConfigInvalidator(msgBus, nil)
	// No panic, no subscriber registered = pass.
}
