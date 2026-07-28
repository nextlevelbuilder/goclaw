package teamworkconfig

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func boolPtr(b bool) *bool { return &b }

// mustEnabled asserts an explicit enabled==want and fails otherwise.
func mustEnabled(t *testing.T, s Settings, want bool) {
	t.Helper()
	if !s.EnabledSet {
		t.Fatalf("expected EnabledSet=true (explicit), got unset %+v", s)
	}
	if s.Enabled != want {
		t.Fatalf("expected Enabled=%v, got %+v", want, s)
	}
}

// fakeConfigStore returns per-tenant config maps keyed by the tenant in context.
// A tenant present in errFor makes List fail, exercising the fail-safe path.
type fakeConfigStore struct {
	mu     sync.Mutex
	byTn   map[uuid.UUID]map[string]string
	errFor map[uuid.UUID]error
	calls  map[uuid.UUID]int
	// listHook, when set, is invoked OUTSIDE the store lock with the running call
	// count after the data snapshot is taken — used to deterministically inject an
	// Invalidate between a read's generation snapshot and its cache publish.
	listHook func(calls int)
}

func newFakeConfigStore() *fakeConfigStore {
	return &fakeConfigStore{
		byTn:   map[uuid.UUID]map[string]string{},
		errFor: map[uuid.UUID]error{},
		calls:  map[uuid.UUID]int{},
	}
}

func (f *fakeConfigStore) Get(context.Context, string) (string, error) { return "", nil }
func (f *fakeConfigStore) Set(context.Context, string, string) error   { return nil }
func (f *fakeConfigStore) Delete(context.Context, string) error        { return nil }
func (f *fakeConfigStore) List(ctx context.Context) (map[string]string, error) {
	tid := store.TenantIDFromContext(ctx)
	f.mu.Lock()
	f.calls[tid]++
	calls := f.calls[tid]
	err := f.errFor[tid]
	// Snapshot (copy) the tenant map under lock so an in-flight read returns the
	// value as of read-start even if the test mutates byTn before the read
	// "completes" (models a real store's read isolation).
	var data map[string]string
	if src := f.byTn[tid]; src != nil {
		data = make(map[string]string, len(src))
		for k, v := range src {
			data[k] = v
		}
	}
	hook := f.listHook
	f.mu.Unlock()

	if hook != nil {
		hook(calls)
	}
	if err != nil {
		return nil, err
	}
	return data, nil
}

func ctxFor(tid uuid.UUID) context.Context {
	return store.WithTenantID(context.Background(), tid)
}

// A tenant with no overrides gets the immutable file-config defaults.
func TestResolveFallsBackToDefaultsWithoutOverride(t *testing.T) {
	defs := Defaults{ClassifyEnabled: boolPtr(true), ClassifierProvider: "anthropic", ClassifierModel: "claude-x"}
	r := NewResolver(defs, newFakeConfigStore())
	got := r.Resolve(ctxFor(uuid.New()))
	mustEnabled(t, got, true)
	if got.ClassifierProvider != "anthropic" || got.ClassifierModel != "claude-x" {
		t.Fatalf("expected default provider/model, got %q/%q", got.ClassifierProvider, got.ClassifierModel)
	}
}

// Tenant A's DB override does NOT bleed into tenant B — the core isolation
// invariant this resolver exists to enforce.
func TestResolveIsolatesTenants(t *testing.T) {
	tnA, tnB := uuid.New(), uuid.New()
	fake := newFakeConfigStore()
	fake.byTn[tnA] = map[string]string{
		KeyClassifyEnabled:  "false",
		KeyClassifyProvider: "openai",
		KeyClassifyModel:    "gpt-z",
	}
	// tnB has no rows at all.
	defs := Defaults{ClassifyEnabled: boolPtr(true), ClassifierProvider: "anthropic", ClassifierModel: "claude-x"}
	r := NewResolver(defs, fake)

	a := r.Resolve(ctxFor(tnA))
	mustEnabled(t, a, false)
	if a.ClassifierProvider != "openai" || a.ClassifierModel != "gpt-z" {
		t.Fatalf("tenant A override provider/model wrong: %q/%q", a.ClassifierProvider, a.ClassifierModel)
	}

	b := r.Resolve(ctxFor(tnB))
	mustEnabled(t, b, true)
	if b.ClassifierProvider != "anthropic" || b.ClassifierModel != "claude-x" {
		t.Fatalf("tenant B must keep default provider/model, got %q/%q", b.ClassifierProvider, b.ClassifierModel)
	}
}

// A present-but-empty provider/model key clears the default override (the
// clear-override behavior the old shared-cfg path had). An absent key keeps the
// default; an empty enable key does NOT flip the bool.
func TestResolveClearOverrideSemantics(t *testing.T) {
	tid := uuid.New()
	fake := newFakeConfigStore()
	fake.byTn[tid] = map[string]string{
		KeyClassifyProvider: "", // present + empty => clear
		KeyClassifyEnabled:  "", // present + empty => NO flip (matches ApplySystemConfigs)
		// KeyClassifyModel absent => keep default
	}
	defs := Defaults{ClassifyEnabled: boolPtr(true), ClassifierProvider: "anthropic", ClassifierModel: "claude-x"}
	r := NewResolver(defs, fake)
	got := r.Resolve(ctxFor(tid))
	if got.ClassifierProvider != "" {
		t.Fatalf("present-empty provider must clear override, got %q", got.ClassifierProvider)
	}
	if got.ClassifierModel != "claude-x" {
		t.Fatalf("absent model key must keep default, got %q", got.ClassifierModel)
	}
	mustEnabled(t, got, true) // present-empty enable must NOT flip default true
}

// A store read error falls back to file-config defaults (fail-safe), not to a
// disabled/empty state.
func TestResolveFailsSafeOnStoreError(t *testing.T) {
	tid := uuid.New()
	fake := newFakeConfigStore()
	fake.errFor[tid] = errors.New("db down")
	defs := Defaults{ClassifyEnabled: boolPtr(true), ClassifierProvider: "anthropic", ClassifierModel: "claude-x"}
	r := NewResolver(defs, fake)
	got := r.Resolve(ctxFor(tid))
	mustEnabled(t, got, true)
	if got.ClassifierProvider != "anthropic" {
		t.Fatalf("store error must keep default provider, got %q", got.ClassifierProvider)
	}
}

// A failed store read must NOT be cached as if it were a successful defaults
// read (Phase 7 review 7B-H2). After the store recovers, the very next Resolve
// must pick up the real override — not serve pinned defaults until restart.
func TestResolveDoesNotCacheStoreError(t *testing.T) {
	tid := uuid.New()
	fake := newFakeConfigStore()
	fake.errFor[tid] = errors.New("db down")
	fake.byTn[tid] = map[string]string{KeyClassifyProvider: "openai"}
	defs := Defaults{ClassifyEnabled: boolPtr(true), ClassifierProvider: "anthropic"}
	r := NewResolver(defs, fake)

	// First resolve: store errors → fail-safe defaults, NOT cached.
	if got := r.Resolve(ctxFor(tid)); got.ClassifierProvider != "anthropic" {
		t.Fatalf("error resolve must yield default provider, got %q", got.ClassifierProvider)
	}
	// Store recovers.
	fake.mu.Lock()
	delete(fake.errFor, tid)
	fake.mu.Unlock()
	// Next resolve MUST re-read (error was not cached) and see the override.
	if got := r.Resolve(ctxFor(tid)); got.ClassifierProvider != "openai" {
		t.Fatalf("post-recovery resolve must see override, got %q (error was cached as defaults)", got.ClassifierProvider)
	}
	if fake.calls[tid] != 2 {
		t.Fatalf("expected 2 store reads (error not cached), got %d", fake.calls[tid])
	}
}

// A read that completes AFTER an Invalidate must not publish its (possibly
// stale) result into the cache; the next Resolve must re-read (Phase 7 review
// 7B-H1 lost-invalidation). Deterministic interleaving via a gate channel.
func TestResolveLostInvalidationRace(t *testing.T) {
	tid := uuid.New()
	fake := newFakeConfigStore()
	fake.byTn[tid] = map[string]string{KeyClassifyProvider: "stale"}

	// gate blocks the FIRST List until we release it, so we can slot an
	// Invalidate in between the generation snapshot and the cache publish.
	gate := make(chan struct{})
	release := make(chan struct{})
	fake.listHook = func(calls int) {
		if calls == 1 {
			close(gate) // signal: read #1 is in flight
			<-release   // block until the test says go
		}
	}
	r := NewResolver(Defaults{ClassifierProvider: "default"}, fake)

	done := make(chan Settings, 1)
	go func() { done <- r.Resolve(ctxFor(tid)) }()

	<-gate // read #1 is now in flight (generation snapshotted)
	// Config changed + invalidated while read #1 is still blocked.
	fake.mu.Lock()
	fake.byTn[tid] = map[string]string{KeyClassifyProvider: "fresh"}
	fake.mu.Unlock()
	r.Invalidate(tid)
	close(release) // let read #1 finish; it must NOT publish "stale"

	if got := <-done; got.ClassifierProvider != "stale" {
		t.Fatalf("in-flight read returns its own snapshot to its caller, got %q", got.ClassifierProvider)
	}
	// The racing read must not have cached: the next Resolve re-reads and sees fresh.
	if got := r.Resolve(ctxFor(tid)); got.ClassifierProvider != "fresh" {
		t.Fatalf("post-invalidate Resolve must re-read fresh, got %q (stale was published)", got.ClassifierProvider)
	}
}

// Phase 7 Decision 7: InvalidateAll must lose the compare-before-publish for an
// in-flight FIRST resolve of a tenant NOT YET in the gen map. The old
// InvalidateAll iterated gen and bumped known tenants only, so a tenant whose
// very first read was still in flight (absent from gen) would publish its stale
// snapshot after InvalidateAll. The global epoch bump covers it: Resolve
// snapshots the epoch before the store read and refuses to publish if it changed.
func TestInvalidateAllLosesInflightFirstResolve(t *testing.T) {
	tid := uuid.New()
	fake := newFakeConfigStore()
	fake.byTn[tid] = map[string]string{KeyClassifyProvider: "stale"}

	// gate blocks the FIRST List until we release it, so we can slot an
	// InvalidateAll in between the epoch snapshot and the cache publish. This is a
	// tenant's FIRST resolve, so it is NOT present in gen — only the epoch guards it.
	gate := make(chan struct{})
	release := make(chan struct{})
	fake.listHook = func(calls int) {
		if calls == 1 {
			close(gate)
			<-release
		}
	}
	r := NewResolver(Defaults{ClassifierProvider: "default"}, fake)

	done := make(chan Settings, 1)
	go func() { done <- r.Resolve(ctxFor(tid)) }()

	<-gate // read #1 is in flight (epoch snapshotted; tenant absent from gen)
	// Config changed + global invalidate while read #1 is still blocked.
	fake.mu.Lock()
	fake.byTn[tid] = map[string]string{KeyClassifyProvider: "fresh"}
	fake.mu.Unlock()
	r.InvalidateAll()
	close(release) // let read #1 finish; it must NOT publish "stale"

	if got := <-done; got.ClassifierProvider != "stale" {
		t.Fatalf("in-flight read returns its own snapshot to its caller, got %q", got.ClassifierProvider)
	}
	// The racing read must not have cached: the next Resolve re-reads fresh.
	if got := r.Resolve(ctxFor(tid)); got.ClassifierProvider != "fresh" {
		t.Fatalf("post-InvalidateAll Resolve must re-read fresh, got %q (stale was published)", got.ClassifierProvider)
	}
}

// After InvalidateAll, a cached tenant's next Resolve re-reads the store and sees
// a changed override — proving InvalidateAll actually clears the cache, not just
// bumps a counter.
func TestInvalidateAllForcesRereadOfChangedValue(t *testing.T) {
	tid := uuid.New()
	fake := newFakeConfigStore()
	fake.byTn[tid] = map[string]string{KeyClassifyProvider: "openai"}
	r := NewResolver(Defaults{ClassifierProvider: "anthropic"}, fake)

	if got := r.Resolve(ctxFor(tid)); got.ClassifierProvider != "openai" {
		t.Fatalf("first resolve = %q", got.ClassifierProvider)
	}
	// Cached: second resolve does not re-read.
	if got := r.Resolve(ctxFor(tid)); got.ClassifierProvider != "openai" {
		t.Fatalf("cached resolve = %q", got.ClassifierProvider)
	}
	if fake.calls[tid] != 1 {
		t.Fatalf("expected 1 store read while cached, got %d", fake.calls[tid])
	}

	// Change override + global invalidate → next read must see it.
	fake.mu.Lock()
	fake.byTn[tid] = map[string]string{KeyClassifyProvider: "google"}
	fake.mu.Unlock()
	r.InvalidateAll()
	if got := r.Resolve(ctxFor(tid)); got.ClassifierProvider != "google" {
		t.Fatalf("post-InvalidateAll resolve = %q", got.ClassifierProvider)
	}
	if fake.calls[tid] != 2 {
		t.Fatalf("expected 2 store reads after InvalidateAll, got %d", fake.calls[tid])
	}
}

// A per-tenant Invalidate must NOT evict a different tenant's cache entry. Only
// the named tenant re-reads; the other stays served from cache.
func TestInvalidateOneDoesNotEvictOther(t *testing.T) {
	tnA, tnB := uuid.New(), uuid.New()
	fake := newFakeConfigStore()
	fake.byTn[tnA] = map[string]string{KeyClassifyProvider: "a1"}
	fake.byTn[tnB] = map[string]string{KeyClassifyProvider: "b1"}
	r := NewResolver(Defaults{ClassifierProvider: "def"}, fake)

	// Prime both caches.
	if got := r.Resolve(ctxFor(tnA)); got.ClassifierProvider != "a1" {
		t.Fatalf("tnA first = %q", got.ClassifierProvider)
	}
	if got := r.Resolve(ctxFor(tnB)); got.ClassifierProvider != "b1" {
		t.Fatalf("tnB first = %q", got.ClassifierProvider)
	}

	// Change both underlying values, then invalidate ONLY A.
	fake.mu.Lock()
	fake.byTn[tnA] = map[string]string{KeyClassifyProvider: "a2"}
	fake.byTn[tnB] = map[string]string{KeyClassifyProvider: "b2"}
	fake.mu.Unlock()
	r.Invalidate(tnA)

	// A re-reads (sees a2); B is still cached (sees old b1, only 1 read total).
	if got := r.Resolve(ctxFor(tnA)); got.ClassifierProvider != "a2" {
		t.Fatalf("post-Invalidate(A) tnA = %q, want a2", got.ClassifierProvider)
	}
	if got := r.Resolve(ctxFor(tnB)); got.ClassifierProvider != "b1" {
		t.Fatalf("Invalidate(A) must not evict B; tnB = %q, want cached b1", got.ClassifierProvider)
	}
	if fake.calls[tnA] != 2 {
		t.Fatalf("tnA expected 2 reads (primed + re-read), got %d", fake.calls[tnA])
	}
	if fake.calls[tnB] != 1 {
		t.Fatalf("tnB expected 1 read (still cached), got %d", fake.calls[tnB])
	}
}

// Concurrency: many Resolves racing InvalidateAll/Invalidate must never panic or
// deadlock and must never serve a value older than the last committed store
// state after quiescence. Run under -race.
func TestInvalidateAllConcurrent(t *testing.T) {
	fake := newFakeConfigStore()
	tenants := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	for _, tid := range tenants {
		fake.byTn[tid] = map[string]string{KeyClassifyProvider: "v0"}
	}
	r := NewResolver(Defaults{ClassifierProvider: "def"}, fake)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				r.Resolve(ctxFor(tenants[j%len(tenants)]))
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 200; j++ {
			if j%2 == 0 {
				r.InvalidateAll()
			} else {
				r.Invalidate(tenants[j%len(tenants)])
			}
		}
	}()
	wg.Wait()

	// After quiescence, set a known value + InvalidateAll and confirm every tenant
	// re-reads it (no stale pin survived the churn).
	fake.mu.Lock()
	for _, tid := range tenants {
		fake.byTn[tid] = map[string]string{KeyClassifyProvider: "final"}
	}
	fake.mu.Unlock()
	r.InvalidateAll()
	for _, tid := range tenants {
		if got := r.Resolve(ctxFor(tid)); got.ClassifierProvider != "final" {
			t.Fatalf("post-churn resolve for %v = %q, want final", tid, got.ClassifierProvider)
		}
	}
}

// Settings is a value type with no exported pointer; a caller mutating the
// returned struct cannot affect the resolver's cache (Phase 7 review 7B-M1).
func TestResolvedSettingsImmutableToCaller(t *testing.T) {
	tid := uuid.New()
	fake := newFakeConfigStore()
	fake.byTn[tid] = map[string]string{KeyClassifyEnabled: "true", KeyClassifyProvider: "openai"}
	r := NewResolver(Defaults{}, fake)

	got := r.Resolve(ctxFor(tid))
	mustEnabled(t, got, true)
	// Mutate the returned copy.
	got.Enabled = false
	got.ClassifierProvider = "tampered"
	// Cached value must be unaffected.
	again := r.Resolve(ctxFor(tid))
	mustEnabled(t, again, true)
	if again.ClassifierProvider != "openai" {
		t.Fatalf("caller mutation leaked into cache: %q", again.ClassifierProvider)
	}
}

// A construction-time Defaults pointer must be copied by value: mutating the
// caller's *bool after NewResolver must not change resolver output (7B-M1).
func TestDefaultsPointerCopiedAtConstruction(t *testing.T) {
	enabled := true
	defs := Defaults{ClassifyEnabled: &enabled, ClassifierProvider: "anthropic"}
	r := NewResolver(defs, newFakeConfigStore())
	// Mutate the caller's pointer AFTER construction.
	enabled = false
	got := r.Resolve(ctxFor(uuid.New()))
	mustEnabled(t, got, true) // resolver kept its own copy (true)
}

// The per-stage classifier timeout is resolvable per tenant, bounded, and
// tolerant of junk: a present-but-unparseable value must keep the file default
// rather than collapse the deadline to zero.
func TestResolveClassifyTimeout(t *testing.T) {
	cases := []struct {
		name     string
		override string
		hasRow   bool
		defSec   int
		want     time.Duration
	}{
		{name: "unset everywhere", want: 0},
		{name: "file default only", defSec: 45, want: 45 * time.Second},
		{name: "db overrides file", override: "90", hasRow: true, defSec: 45, want: 90 * time.Second},
		{name: "db clamps to max", override: "9999", hasRow: true, want: maxClassifyTimeoutSec * time.Second},
		{name: "explicit zero means built-in defaults", override: "0", hasRow: true, defSec: 45, want: 0},
		{name: "negative ignored, keeps file default", override: "-5", hasRow: true, defSec: 45, want: 45 * time.Second},
		{name: "garbage ignored, keeps file default", override: "soon", hasRow: true, defSec: 45, want: 45 * time.Second},
		{name: "empty row ignored, keeps file default", override: "", hasRow: true, defSec: 45, want: 45 * time.Second},
		{name: "file default clamped too", defSec: 9999, want: maxClassifyTimeoutSec * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tid := uuid.New()
			fake := newFakeConfigStore()
			if tc.hasRow {
				fake.byTn[tid] = map[string]string{KeyClassifyTimeoutSec: tc.override}
			}
			r := NewResolver(Defaults{ClassifyEnabled: boolPtr(true), ClassifyTimeoutSec: tc.defSec}, fake)
			if got := r.Resolve(ctxFor(tid)).ClassifyTimeout; got != tc.want {
				t.Fatalf("ClassifyTimeout = %v, want %v", got, tc.want)
			}
		})
	}
}

// A missing tenant (uuid.Nil) returns defaults and is NOT cached under a shared
// nil bucket (Phase 7 review 7B-L).
func TestResolveNilTenantUncached(t *testing.T) {
	fake := newFakeConfigStore()
	r := NewResolver(Defaults{ClassifyEnabled: boolPtr(true), ClassifierProvider: "anthropic"}, fake)
	got := r.Resolve(context.Background()) // no tenant in ctx
	mustEnabled(t, got, true)
	if got.ClassifierProvider != "anthropic" {
		t.Fatalf("nil-tenant must yield defaults, got %q", got.ClassifierProvider)
	}
	// nil-tenant must never hit the store, and must not create a cache entry.
	if fake.calls[uuid.Nil] != 0 {
		t.Fatalf("nil-tenant must not read store, got %d reads", fake.calls[uuid.Nil])
	}
	r.mu.RLock()
	_, cached := r.cache[uuid.Nil]
	r.mu.RUnlock()
	if cached {
		t.Fatal("nil-tenant must not create a uuid.Nil cache bucket")
	}
}

// The cache is populated on first Resolve and reused until Invalidate; Invalidate
// forces a re-read that picks up a changed override.
func TestResolveCachesAndInvalidates(t *testing.T) {
	tid := uuid.New()
	fake := newFakeConfigStore()
	fake.byTn[tid] = map[string]string{KeyClassifyProvider: "openai"}
	r := NewResolver(Defaults{ClassifierProvider: "anthropic"}, fake)

	if got := r.Resolve(ctxFor(tid)); got.ClassifierProvider != "openai" {
		t.Fatalf("first resolve provider = %q", got.ClassifierProvider)
	}
	if got := r.Resolve(ctxFor(tid)); got.ClassifierProvider != "openai" {
		t.Fatalf("cached resolve provider = %q", got.ClassifierProvider)
	}
	if fake.calls[tid] != 1 {
		t.Fatalf("expected exactly 1 store read while cached, got %d", fake.calls[tid])
	}

	// Change the override, then invalidate — next read must see it.
	fake.mu.Lock()
	fake.byTn[tid] = map[string]string{KeyClassifyProvider: "google"}
	fake.mu.Unlock()
	r.Invalidate(tid)
	if got := r.Resolve(ctxFor(tid)); got.ClassifierProvider != "google" {
		t.Fatalf("post-invalidate provider = %q", got.ClassifierProvider)
	}
	if fake.calls[tid] != 2 {
		t.Fatalf("expected 2 store reads after invalidate, got %d", fake.calls[tid])
	}
}

// A nil store yields defaults without panicking.
func TestResolveNilStoreYieldsDefaults(t *testing.T) {
	r := NewResolver(Defaults{ClassifyEnabled: boolPtr(true), ClassifierProvider: "anthropic"}, nil)
	got := r.Resolve(ctxFor(uuid.New()))
	mustEnabled(t, got, true)
	if got.ClassifierProvider != "anthropic" {
		t.Fatalf("nil store must yield defaults, got %+v", got)
	}
}

// A nil resolver is safe (zero-value guard for partial wiring).
func TestNilResolverSafe(t *testing.T) {
	var r *Resolver
	if got := r.Resolve(context.Background()); got.EnabledSet {
		t.Fatalf("nil resolver must yield zero settings, got %+v", got)
	}
	r.Invalidate(uuid.New()) // must not panic
	r.InvalidateAll()        // must not panic
}
