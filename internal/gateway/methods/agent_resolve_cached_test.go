package methods

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// errSentinelMiss is a sentinel error used to verify DB fallback in cache-miss
// paths. The value is stable across tests so equality checks are reliable.
var errSentinelMiss = errors.New("sentinel: agent miss")

// errorAgentStore embeds store.AgentStore and overrides only GetByID and
// GetByKey to return a sentinel error. All other methods retain the nil embed,
// so any unexpected call path panics — loud failure is intentional.
type errorAgentStore struct {
	store.AgentStore
	err error
}

func (s *errorAgentStore) GetByID(context.Context, uuid.UUID) (*store.AgentData, error) {
	return nil, s.err
}

func (s *errorAgentStore) GetByKey(context.Context, string) (*store.AgentData, error) {
	return nil, s.err
}

// TestResolveAgentUUIDCached_NilRouterCallsResolveAgentUUID verifies that when
// router is nil the helper delegates directly to resolveAgentUUID — exercising
// the slow DB path. With a stub store returning a sentinel error, the helper
// must propagate that error unchanged.
func TestResolveAgentUUIDCached_NilRouterCallsResolveAgentUUID(t *testing.T) {
	stub := &errorAgentStore{err: errSentinelMiss}

	_, err := resolveAgentUUIDCached(context.Background(), nil, stub, uuid.New().String())

	if !errors.Is(err, errSentinelMiss) {
		t.Errorf("expected sentinel miss error, got %v", err)
	}
}

// TestResolveAgentUUIDCached_CacheMissFallsBack — when the router is set but
// has no cached entry for the given agent_key, the helper must fall back to
// the DB path. The stub store's sentinel error confirms we took the fallback.
func TestResolveAgentUUIDCached_CacheMissFallsBack(t *testing.T) {
	r := agent.NewRouter()
	stub := &errorAgentStore{err: errSentinelMiss}

	_, err := resolveAgentUUIDCached(context.Background(), r, stub, "missing-agent-key")

	if !errors.Is(err, errSentinelMiss) {
		t.Errorf("expected sentinel miss error, got %v", err)
	}
}

// TestResolveAgentUUIDCached_UUIDInputTakesDBPath verifies that a caller
// passing the UUID form falls through to the DB path. Router cache entries
// are canonicalized to `tenantID:agentKey`, so the raw UUID input never hits
// the cache and the helper must delegate to the store stub (whose sentinel
// error surfaces here).
func TestResolveAgentUUIDCached_UUIDInputTakesDBPath(t *testing.T) {
	r := agent.NewRouter()
	stub := &errorAgentStore{err: errSentinelMiss}

	_, err := resolveAgentUUIDCached(context.Background(), r, stub, uuid.New().String())

	if !errors.Is(err, errSentinelMiss) {
		t.Errorf("expected sentinel miss error, got %v", err)
	}
}

// cacheHitStubAgent implements agent.Agent + agentUUIDProvider so it can be
// registered in the router and the cache-aware helper can extract its UUID
// without hitting the store.
type cacheHitStubAgent struct {
	id  string
	uid uuid.UUID
}

func (s *cacheHitStubAgent) ID() string                                          { return s.id }
func (s *cacheHitStubAgent) UUID() uuid.UUID                                     { return s.uid }
func (s *cacheHitStubAgent) OtherConfig() json.RawMessage                        { return nil }
func (s *cacheHitStubAgent) Run(context.Context, agent.RunRequest) (*agent.RunResult, error) {
	return nil, nil
}
func (s *cacheHitStubAgent) IsRunning() bool              { return false }
func (s *cacheHitStubAgent) Model() string                { return "test-model" }
func (s *cacheHitStubAgent) ProviderName() string         { return "test" }
func (s *cacheHitStubAgent) Provider() providers.Provider { return nil }

// TestResolveAgentUUIDCached_CacheHitSkipsDBPath pins the fast path: when the
// caller passes an agent_key AND the Loop is cached in the router AND the
// cached Loop satisfies agentUUIDProvider, the helper must return the cached
// UUID without invoking the store. The store is an errorAgentStore that
// returns a sentinel error on any call — if the fast path misbehaves and
// falls through to the DB lookup, that sentinel surfaces as a test failure.
func TestResolveAgentUUIDCached_CacheHitSkipsDBPath(t *testing.T) {
	r := agent.NewRouter()
	expectedUUID := uuid.New()
	agentKey := "cached-agent"

	// Prime the router cache via a resolver that returns the stub. Router.Get
	// canonicalizes under `tenantID:agentKey` so a subsequent cache-aware
	// lookup with the same (ctx, agentKey) resolves via the fast path.
	r.SetResolver(func(_ context.Context, _ string) (agent.Agent, error) {
		return &cacheHitStubAgent{id: agentKey, uid: expectedUUID}, nil
	})
	tenantID := uuid.New()
	ctx := store.WithTenantID(context.Background(), tenantID)
	if _, err := r.Get(ctx, agentKey); err != nil {
		t.Fatalf("prime Router.Get: %v", err)
	}

	// Any store call is a test failure. If the fast path works, the store is
	// never touched and no error surfaces.
	stub := &errorAgentStore{err: errSentinelMiss}

	got, err := resolveAgentUUIDCached(ctx, r, stub, agentKey)
	if err != nil {
		t.Fatalf("fast path returned error %v — expected cache hit without DB lookup", err)
	}
	if got != expectedUUID {
		t.Errorf("fast path returned UUID %v, want %v", got, expectedUUID)
	}
}
