// Package teamworkconfig resolves the request-time Team Work classifier settings
// on a per-tenant basis. It exists to stop cross-tenant leakage of the three
// proven request-time Team Work keys — gateway.team_work_classify,
// gateway.team_work_classify_provider, gateway.team_work_classify_model — which
// were previously overlaid onto the process-wide *config.Config by
// ApplySystemConfigs and so bled between tenants.
//
// It lives in its own package (not internal/config) because internal/store
// imports internal/config; a resolver that reads store.SystemConfigStore cannot
// live in config without creating an import cycle. It takes its defaults as plain
// values rather than the *config.Config type for the same reason.
//
// Resolution mirrors the semantics of config.ApplySystemConfigs exactly:
//   - the enable bool is overridden only when the DB key is present AND non-empty
//     ("true"/"1" => true, anything else => false);
//   - provider/model overrides apply whenever the DB key is PRESENT, even if the
//     value is empty — this preserves the "clear the override" behavior that the
//     shared-config path had.
//
// Fail-safe posture: a store read error falls back to the immutable file-config
// defaults for THAT resolution, but the failed read is NOT cached — the next
// Resolve retries the store so a transient system_configs outage cannot pin a
// tenant to defaults until restart (Phase 7 review 7B-H2). Successful reads are
// cached until Invalidate.
//
// Concurrency / invalidation model (Phase 7 review 7B-H1): the store read
// happens outside the cache lock, so a config update + Invalidate can interleave
// with an in-flight read. Each tenant carries a monotonic generation counter that
// Invalidate bumps. Resolve snapshots the generation BEFORE the read and only
// publishes the result to the cache if the generation is unchanged afterward
// (compare-before-publish). A read that raced with an invalidation is returned to
// its caller but never cached, so no stale value can outlive the invalidation.
//
// Scope: invalidation is process-local (delivered via the in-process MessageBus).
// This is correct for the single-gateway-process deployment GoClaw runs today;
// there is no second replica holding a competing cache. If a multi-replica
// gateway is ever introduced, this resolver needs a bounded TTL or a distributed
// invalidation signal — see [[project-goclaw-teamwork-progress]] and Phase 7
// review 7B-H3.
package teamworkconfig

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// DB keys — must match cmd/gateway_system_config_sync.go and
// config.ApplySystemConfigs.
const (
	KeyClassifyEnabled    = "gateway.team_work_classify"
	KeyClassifyProvider   = "gateway.team_work_classify_provider"
	KeyClassifyModel      = "gateway.team_work_classify_model"
	KeyClassifyTimeoutSec = "gateway.team_work_classify_timeout_sec"
)

// maxClassifyTimeoutSec bounds the configurable per-stage classifier deadline.
// The classifier runs up to five sequential LLM stages, so an unbounded value
// could hold a chat turn (and its scheduler slot) for many minutes before the
// gate resolves. 300s per stage is already far beyond any healthy provider.
const maxClassifyTimeoutSec = 300

// Settings is the resolved, request-time Team Work classifier configuration for
// one tenant. It is an immutable value type: Enabled/EnabledSet replace the old
// *bool so a caller can neither mutate the resolver's cached state nor race on a
// shared pointer (Phase 7 review 7B-M1).
type Settings struct {
	// Enabled reports whether the Team Work classifier gate runs for this tenant.
	// Only meaningful when EnabledSet is true; when EnabledSet is false the value
	// is "unset" and callers treat it as disabled (matching the old nil *bool).
	Enabled bool
	// EnabledSet reports whether an explicit enable value exists (file default or
	// DB override). false == the old nil *bool.
	EnabledSet bool
	// ClassifierProvider / ClassifierModel are the optional classifier-specific
	// provider/model overrides. Empty means "use the agent's runtime pair", per
	// ResolveTeamWorkClassifier.
	ClassifierProvider string
	ClassifierModel    string
	// ClassifyTimeout is the optional per-call LLM deadline for the Team Work
	// pipeline: the classifier stages AND the agent loop's directive-enforcement
	// calls. Zero means "use the package defaults" (30s arbiter / 60s planner in
	// internal/teamworkclassify, 30s enforcement in internal/agent). It exists
	// because those defaults are hard-coded constants while model latency is a
	// per-agent property: an agent whose runtime model needs ~30s per turn
	// degrades at whichever of the sequential calls happens to cross the line, so
	// the routing decision is correct but the turn is stamped degraded — or worse,
	// the enforcement call dies and a validated plan is discarded. Bounded by
	// maxClassifyTimeoutSec.
	ClassifyTimeout time.Duration
}

// ClassifyEnabled reports whether the classifier gate should run: an explicit
// value that is true. Unset (EnabledSet==false) is treated as disabled, exactly
// as the previous nil *bool was.
func (s Settings) ClassifyEnabled() bool { return s.EnabledSet && s.Enabled }

// Defaults are the immutable file-config values captured once at startup. The
// resolver copies these by value at construction; per-tenant DB overrides are
// layered on top per request.
type Defaults struct {
	// ClassifyEnabled is the file-config *bool. It is dereferenced and copied by
	// value in NewResolver — the resolver never retains this pointer.
	ClassifyEnabled    *bool
	ClassifierProvider string
	ClassifierModel    string
	// ClassifyTimeoutSec is the file-config per-stage classifier deadline in
	// seconds. 0 = unset (use the teamworkclassify package defaults).
	ClassifyTimeoutSec int
}

// cacheEntry is a cached resolution stamped with the tenant generation AND the
// global epoch it was read under, so a later Invalidate (per-tenant) or
// InvalidateAll (global) can detect and refuse a racing publish.
type cacheEntry struct {
	settings   Settings
	generation uint64
	epoch      uint64
}

// Resolver returns per-tenant Team Work classifier settings, layering
// system_configs DB overrides over the immutable file-config defaults. It is
// safe for concurrent use.
type Resolver struct {
	// defaults is the immutable startup snapshot (copied by value; no shared
	// pointer with the caller's Defaults).
	defaults Settings
	store    store.SystemConfigStore

	mu    sync.RWMutex
	cache map[uuid.UUID]cacheEntry
	// gen tracks the current generation per tenant. Invalidate bumps it; Resolve
	// compares before publishing. A tenant absent from gen has generation 0.
	gen map[uuid.UUID]uint64
	// epoch is a process-wide generation counter that InvalidateAll bumps. Resolve
	// snapshots it BEFORE the store read and refuses to publish if it changed,
	// which protects in-flight FIRST resolves for tenants not yet present in gen —
	// InvalidateAll no longer needs to iterate/bump every known tenant to cover
	// them (Phase 7 Decision 7).
	epoch uint64
}

// NewResolver builds a Resolver over the given immutable defaults and system
// config store. store may be nil (e.g. in unit wiring that never exercises
// overrides); in that case every resolution returns the defaults. The bool
// default is dereferenced and copied by value here so the resolver never
// retains the caller's pointer (Phase 7 review 7B-M1/deep-copy).
func NewResolver(defaults Defaults, cfgStore store.SystemConfigStore) *Resolver {
	base := Settings{
		ClassifierProvider: defaults.ClassifierProvider,
		ClassifierModel:    defaults.ClassifierModel,
		ClassifyTimeout:    clampClassifyTimeout(defaults.ClassifyTimeoutSec),
	}
	if defaults.ClassifyEnabled != nil {
		base.Enabled = *defaults.ClassifyEnabled
		base.EnabledSet = true
	}
	return &Resolver{
		defaults: base,
		store:    cfgStore,
		cache:    make(map[uuid.UUID]cacheEntry),
		gen:      make(map[uuid.UUID]uint64),
	}
}

// Resolve returns the effective settings for the tenant carried by ctx.
//
// A missing tenant (uuid.Nil) is NOT silently cached under a shared nil bucket:
// it logs once-per-call at debug and returns the defaults without touching the
// cache, so a mis-scoped call can never poison a real tenant's entry (Phase 7
// review 7B-L). On any store error it returns the file-config defaults for this
// call WITHOUT caching (fail-safe, no error pinning — 7B-H2). Successful reads
// are cached until Invalidate, using compare-before-publish so a read that raced
// an invalidation is returned but not cached (7B-H1).
func (r *Resolver) Resolve(ctx context.Context) Settings {
	if r == nil {
		return Settings{}
	}
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		// No tenant scope: return defaults, do not cache. Caching under uuid.Nil
		// would let an unscoped request's result be served to a real tenant.
		slog.Debug("teamworkconfig: Resolve called without tenant scope; returning defaults uncached")
		return r.defaults
	}

	r.mu.RLock()
	cached, ok := r.cache[tenantID]
	curGen := r.gen[tenantID]
	curEpoch := r.epoch
	r.mu.RUnlock()
	if ok && cached.generation == curGen && cached.epoch == curEpoch {
		return cached.settings
	}

	// Snapshot the generation AND global epoch we are reading under, then read the
	// store outside the lock.
	resolved, cacheable := r.resolveUncached(ctx)
	if !cacheable {
		// Store error (or no store on a would-be override read): return the
		// fail-safe value but do not cache it, so the next Resolve retries.
		return resolved
	}

	r.mu.Lock()
	// Compare-before-publish: only cache if NEITHER a per-tenant Invalidate bumped
	// this tenant's generation NOR an InvalidateAll bumped the global epoch since we
	// snapshotted them. Otherwise the read raced an invalidation and may be stale —
	// hand it back to this caller but leave the cache empty so the next Resolve
	// re-reads. The epoch check protects an in-flight FIRST resolve for a tenant not
	// yet present in gen, which a per-tenant bump alone could not cover.
	if r.gen[tenantID] == curGen && r.epoch == curEpoch {
		r.cache[tenantID] = cacheEntry{settings: resolved, generation: curGen, epoch: curEpoch}
	}
	r.mu.Unlock()
	return resolved
}

// resolveUncached reads the tenant's system_configs and overlays them on the
// defaults. The bool return reports whether the result is safe to cache: false
// on a store error (or nil store), so callers never pin a failed read.
func (r *Resolver) resolveUncached(ctx context.Context) (Settings, bool) {
	resolved := r.defaults
	if r.store == nil {
		// No store configured: defaults are stable and cacheable.
		return resolved, true
	}
	configs, err := r.store.List(ctx)
	if err != nil {
		// Fail-safe: keep file-config defaults on a read error, and DO NOT cache
		// — a transient outage must not disable/enable classification until the
		// next config mutation or restart.
		slog.Warn("teamworkconfig: system_configs read failed; using file defaults (uncached)",
			"tenant", store.TenantIDFromContext(ctx), "error", err)
		return resolved, false
	}
	applyOverrides(&resolved, configs)
	return resolved, true
}

// applyOverrides layers DB config values onto s, matching
// config.ApplySystemConfigs semantics exactly.
func applyOverrides(s *Settings, configs map[string]string) {
	if v, ok := configs[KeyClassifyEnabled]; ok && v != "" {
		s.Enabled = v == "true" || v == "1"
		s.EnabledSet = true
	}
	// Provider/model override whenever present (even empty) to preserve the
	// clear-override behavior of the old shared-cfg path.
	if v, ok := configs[KeyClassifyProvider]; ok {
		s.ClassifierProvider = v
	}
	if v, ok := configs[KeyClassifyModel]; ok {
		s.ClassifierModel = v
	}
	// The timeout override applies only for a PRESENT, PARSEABLE, non-negative
	// value. A present-but-garbage value keeps the file default rather than
	// silently collapsing the deadline to zero (which would read as "use
	// built-in defaults" and hide the misconfiguration); an explicit 0 is a
	// legitimate "use built-in defaults".
	if v, ok := configs[KeyClassifyTimeoutSec]; ok && strings.TrimSpace(v) != "" {
		if secs, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && secs >= 0 {
			s.ClassifyTimeout = clampClassifyTimeout(secs)
		} else {
			slog.Warn("teamworkconfig: ignoring unparseable classifier timeout override",
				"key", KeyClassifyTimeoutSec, "value", v)
		}
	}
}

// clampClassifyTimeout converts a seconds value to a duration, treating
// non-positive input as "unset" (zero => package defaults) and bounding the
// upper end at maxClassifyTimeoutSec.
func clampClassifyTimeout(secs int) time.Duration {
	if secs <= 0 {
		return 0
	}
	if secs > maxClassifyTimeoutSec {
		secs = maxClassifyTimeoutSec
	}
	return time.Duration(secs) * time.Second
}

// Invalidate drops the cached settings for one tenant and bumps its generation
// so any store read already in flight for that tenant will refuse to publish its
// (possibly stale) result. Called from the system-config-changed subscriber with
// the changed tenant in context.
func (r *Resolver) Invalidate(tenantID uuid.UUID) {
	if r == nil {
		return
	}
	r.mu.Lock()
	delete(r.cache, tenantID)
	r.gen[tenantID]++
	r.mu.Unlock()
}

// InvalidateAll drops every cached tenant and bumps the global epoch, e.g. on a
// global config reload. Bumping the single epoch (rather than iterating and
// bumping every known tenant generation) also invalidates in-flight FIRST
// resolves for tenants not yet present in gen: Resolve snapshots the epoch before
// its store read and refuses to publish if it changed. Per-tenant generations are
// left untouched — the epoch mismatch alone loses the compare-before-publish for
// every tenant, known or not (Phase 7 Decision 7).
func (r *Resolver) InvalidateAll() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.cache = make(map[uuid.UUID]cacheEntry)
	r.epoch++
	r.mu.Unlock()
}
