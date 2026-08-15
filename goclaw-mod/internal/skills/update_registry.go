package skills

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// ErrUnknownUpdateSource is returned when Apply is called with a source that
// has no registered executor.
var ErrUnknownUpdateSource = errors.New("skills: unknown update source")

// UpdateCheckResult is what a checker returns for a single CheckAll invocation.
// The registry merges Updates and ETags from all checkers under lock; the
// checker owns only its local maps until return (red-team fix C2: never mutate
// shared cache concurrently across goroutines).
type UpdateCheckResult struct {
	Source   string
	Updates  []UpdateInfo
	ETags    map[string]string // subset to merge into UpdateCache.GitHubETags
	Err      error             // per-source error; non-fatal for other checkers
	// Available signals whether the source is actionable on this host.
	// false (zero-value) means exec.LookPath / edition gate rejected the source,
	// or the checker was never run. The HTTP availability map surfaces this so
	// the UI can hide sources that are not actionable.
	// Interpretation: false === "not actionable"; a non-error check with
	// Updates == nil but Available == true means "source reachable, zero updates".
	// Checkers MUST set Available=true on a normal successful check and leave
	// it false only on LookPath miss or edition gate rejection.
	Available bool
}

// UpdateChecker polls a package source for available updates.
// Implementations MUST NOT mutate the shared UpdateCache; return a local
// UpdateCheckResult and let the registry merge.
type UpdateChecker interface {
	Source() string
	// Check returns the updates + new ETags for this source.
	// `knownETags` is a read-only snapshot of the cached ETags for this
	// source (caller-scoped keys). Implementations issue If-None-Match
	// requests using these and return NEW ETags in the result.
	Check(ctx context.Context, knownETags map[string]string) UpdateCheckResult
}

// UpdateExecutor applies a single update for a source.
// Callers acquire PackageLocker before invoking Update so the executor itself
// is lock-free and composable.
type UpdateExecutor interface {
	Source() string
	// Update applies the target version.
	// `meta` is the snapshot from UpdateInfo.Meta at check time; implementations
	// MUST treat every value as optional and re-fetch authoritative data when
	// missing or stale (red-team C3).
	Update(ctx context.Context, name, toVersion string, meta map[string]any) error
}

// UpdateRegistry is the façade over registered checkers + executors + the
// cache + the package locker. One instance per gateway; injected into HTTP
// handlers and the background refresher.
type UpdateRegistry struct {
	checkers  map[string]UpdateChecker
	executors map[string]UpdateExecutor
	Locker    *PackageLocker
	Cache     *UpdateCache
	CachePath string
	TTL       time.Duration

	mu           sync.RWMutex
	refreshing   atomic.Bool     // single-flight gate for background refresh
	availability map[string]bool // per-source availability from last CheckAll; guarded by mu
}

// NewUpdateRegistry constructs an empty registry. Register checkers/executors
// via RegisterChecker / RegisterExecutor before use.
func NewUpdateRegistry(cache *UpdateCache, cachePath string, ttl time.Duration) *UpdateRegistry {
	if cache == nil {
		cache = &UpdateCache{GitHubETags: make(map[string]string)}
	}
	if ttl <= 0 {
		ttl = time.Hour
	}
	return &UpdateRegistry{
		checkers:     make(map[string]UpdateChecker),
		executors:    make(map[string]UpdateExecutor),
		Locker:       NewPackageLocker(),
		Cache:        cache,
		CachePath:    cachePath,
		TTL:          ttl,
		availability: make(map[string]bool),
	}
}

// RegisterChecker associates a checker with its source name. Overwrites any
// prior registration (useful for tests).
func (r *UpdateRegistry) RegisterChecker(c UpdateChecker) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.checkers[c.Source()] = c
}

// RegisterExecutor associates an executor with its source name.
func (r *UpdateRegistry) RegisterExecutor(e UpdateExecutor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.executors[e.Source()] = e
}

// Sources returns the registered checker source names, stable order.
func (r *UpdateRegistry) Sources() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.checkers))
	for s := range r.checkers {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// Availability returns a snapshot of per-source availability from the last CheckAll.
// A missing key means "never checked" — callers should treat a missing key as true
// (first-boot default: source is visible until confirmed unavailable).
// The returned map is a safe clone; mutating it does not affect the registry.
func (r *UpdateRegistry) Availability() map[string]bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]bool, len(r.availability))
	for k, v := range r.availability {
		out[k] = v
	}
	return out
}

// setAvailability records per-source availability under write lock.
func (r *UpdateRegistry) setAvailability(source string, available bool) {
	r.mu.Lock()
	r.availability[source] = available
	r.mu.Unlock()
}

// SetAvailability records per-source availability under write lock.
// Intended for wiring code to seed availability entries when a source's
// checker is deliberately not registered (e.g. apk on non-Alpine runtime).
// Safe to call before the first CheckAll; the value persists until the
// next CheckAll for this source overwrites it.
func (r *UpdateRegistry) SetAvailability(source string, available bool) {
	r.setAvailability(source, available)
}

// CheckAll runs every registered checker and merges results into the cache.
// Checkers run in parallel (each is an independent API). A single checker's
// error does NOT abort siblings (red-team M7 fix — don't use errgroup which
// cancels ctx on first error).
//
// Returns a slice of per-source errors (empty = all OK).
func (r *UpdateRegistry) CheckAll(ctx context.Context) []error {
	r.mu.RLock()
	checkers := make([]UpdateChecker, 0, len(r.checkers))
	for _, c := range r.checkers {
		checkers = append(checkers, c)
	}
	r.mu.RUnlock()

	// Snapshot ETags per source so each checker sees a stable read-only view.
	// Keys are global today (github uses "owner/repo"), but keep per-source
	// scoping so Phase 2 sources (pip/npm) can add their own keyspace without
	// collision risk.
	allETags := make(map[string]string)
	r.Cache.mu.Lock()
	for k, v := range r.Cache.GitHubETags {
		allETags[k] = v
	}
	r.Cache.mu.Unlock()

	results := make([]UpdateCheckResult, len(checkers))
	var wg sync.WaitGroup
	for i, c := range checkers {
		wg.Add(1)
		go func(idx int, checker UpdateChecker) {
			defer wg.Done()
			defer func() {
				if rec := recover(); rec != nil {
					slog.Error("skills.update: checker panic",
						"source", checker.Source(), "panic", fmt.Sprintf("%v", rec))
					results[idx] = UpdateCheckResult{
						Source: checker.Source(),
						Err:    fmt.Errorf("checker panic: %v", rec),
					}
				}
			}()
			results[idx] = checker.Check(ctx, allETags)
		}(i, c)
	}
	wg.Wait()

	// Aggregate under cache lock.
	var errs []error
	merged := make([]UpdateInfo, 0, 16)
	etagMerge := make(map[string]string)
	for _, res := range results {
		if res.Err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", res.Source, res.Err))
			// Still apply any partial etag merges from failed checker —
			// 304 cache reuse is independent of per-repo failures.
		}
		merged = append(merged, res.Updates...)
		for k, v := range res.ETags {
			etagMerge[k] = v
		}
		// Record per-source availability from this check cycle.
		r.setAvailability(res.Source, res.Available)
	}

	now := time.Now().UTC()
	r.Cache.MergeETags(etagMerge)
	r.Cache.ReplaceUpdates(merged, now)

	if r.CachePath != "" {
		if err := SaveUpdateCache(r.CachePath, r.Cache); err != nil {
			slog.Error("skills.update: save cache failed", "error", err)
			errs = append(errs, fmt.Errorf("save cache: %w", err))
		}
	}
	return errs
}

// RefreshInBackground triggers CheckAll in a detached goroutine iff no
// refresh is already in flight. Caller may use any ctx for lineage — the
// goroutine uses context.WithoutCancel to survive request-scoped cancels.
//
// Red-team H2: the goroutine installs defer-recover + defer-Store(false) so
// a panic never strands refreshing=true (which would block all future refreshes).
func (r *UpdateRegistry) RefreshInBackground(parent context.Context, timeout time.Duration) bool {
	if !r.refreshing.CompareAndSwap(false, true) {
		return false
	}
	// Detach from parent cancel so in-flight HTTP timeouts don't abort refresh.
	detached := context.WithoutCancel(parent)
	go func() {
		defer r.refreshing.Store(false)
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("skills.update: background refresh panic",
					"panic", fmt.Sprintf("%v", rec))
			}
		}()
		ctx, cancel := context.WithTimeout(detached, timeout)
		defer cancel()
		if errs := r.CheckAll(ctx); len(errs) > 0 {
			slog.Warn("skills.update: background refresh finished with errors",
				"error_count", len(errs))
		}
	}()
	return true
}

// IsStale returns true when the cache CheckedAt is older than TTL.
func (r *UpdateRegistry) IsStale() bool {
	_, checkedAt := r.Cache.Snapshot()
	if checkedAt.IsZero() {
		return true
	}
	return time.Since(checkedAt) > r.TTL
}

// Apply acquires the package lock and invokes the matching executor.
// Returns the elapsed duration + any executor error.
//
// The caller is responsible for publishing started/succeeded/failed events;
// Apply is deliberately lock-+-dispatch only so HTTP handlers keep event
// ordering under their control (publish "started" before Apply, etc.).
//
// `lockKey` MUST match the key used by the install path for the same package
// — for the "github" source, callers pass the repo (e.g. "lazygit") which
// the installer uses in Install(). Diverging lock keys defeats the shared
// PackageLocker's purpose (review CRIT-2).
func (r *UpdateRegistry) Apply(ctx context.Context, source, lockKey, name, toVersion string, meta map[string]any) (time.Duration, error) {
	r.mu.RLock()
	exec, ok := r.executors[source]
	r.mu.RUnlock()
	if !ok {
		return 0, fmt.Errorf("%w: %s", ErrUnknownUpdateSource, source)
	}
	if lockKey == "" {
		lockKey = name
	}

	release, err := r.Locker.Acquire(ctx, source, lockKey)
	if err != nil {
		return 0, fmt.Errorf("lock acquire: %w", err)
	}
	defer release()

	start := time.Now()
	err = exec.Update(ctx, name, toVersion, meta)
	elapsed := time.Since(start)
	if err == nil {
		// Drop the entry from cache so the UI immediately reflects success.
		r.Cache.RemoveUpdate(source, name)
		if r.CachePath != "" {
			if serr := SaveUpdateCache(r.CachePath, r.Cache); serr != nil {
				slog.Warn("skills.update: cache save after apply failed", "error", serr)
			}
		}
	}
	return elapsed, err
}
