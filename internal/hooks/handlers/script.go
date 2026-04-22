package handlers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"
	"github.com/google/uuid"
	lru "github.com/hashicorp/golang-lru/v2"

	"github.com/nextlevelbuilder/goclaw/internal/hooks"
)

// MaxScriptSourceBytes is the mandatory safety-floor cap on script source size
// (brainstorm §Critical safety items #2). Validated again at execute time as a
// belt-and-suspenders check after Phase 03 wires the Create/Update validation.
const MaxScriptSourceBytes = 32 * 1024

// MaxStdoutBytes is the mandatory safety-floor cap on captured console output
// per execution. Writes past the cap are dropped with a `... truncated` marker.
const MaxStdoutBytes = 4 * 1024

// defaultGlobalConcurrency / defaultPerTenantConcurrency / defaultCacheSize
// match the brainstorm safety-floor defaults. Overridden by NewScriptHandler
// arguments when the gateway config specifies different values.
const (
	defaultGlobalConcurrency    = 10
	defaultPerTenantConcurrency = 3
	defaultCacheSize            = 500
)

// ScriptHandler implements hooks.Handler via Goja ES5.1.
//
// Safety floor — all mandatory, independent of any future budget system:
//   - two-layer semaphore (global cap + per-tenant cap) prevents one runaway
//     tenant from starving the global pool.
//   - LRU program cache (bounded) so repeated invocations skip recompile without
//     unbounded memory growth.
//   - source size cap + stdout size cap on every execution.
//
// Concurrency: a ScriptHandler is safe to share across goroutines. Each
// Execute call allocates a fresh goja.Runtime — runtimes are never shared.
type ScriptHandler struct {
	// globalSem is a buffered channel acting as the global concurrency ceiling.
	globalSem chan struct{}
	// perTenantCap bounds the per-tenant sub-semaphore size.
	perTenantCap int
	// tenantSems: lazily allocated per-tenant slot pool; protects runaway tenant
	// from saturating all global slots.
	tenantMu   sync.Mutex
	tenantSems map[uuid.UUID]*tenantSlot

	// progCache: LRU bounded at cacheSize entries, keyed by (hookID, version).
	// Invalidated on hook update/delete through InvalidateHook.
	progCache *lru.Cache[progCacheKey, *goja.Program]
}

// tenantSlot tracks a single tenant's concurrency allowance plus last-use time
// so a background cleanup can reclaim idle tenants (future Phase; scaffolding
// here is intentional).
type tenantSlot struct {
	sem      chan struct{}
	lastUsed time.Time
}

type progCacheKey struct {
	ID      uuid.UUID
	Version int
}

// NewScriptHandler constructs a ScriptHandler with the given caps. Any cap <= 0
// falls back to the package default. The LRU allocation only fails on
// size <= 0, which we already guard, so the error branch is unreachable.
func NewScriptHandler(globalConcurrency, perTenantConcurrency, cacheSize int) *ScriptHandler {
	if globalConcurrency <= 0 {
		globalConcurrency = defaultGlobalConcurrency
	}
	if perTenantConcurrency <= 0 {
		perTenantConcurrency = defaultPerTenantConcurrency
	}
	if cacheSize <= 0 {
		cacheSize = defaultCacheSize
	}
	cache, _ := lru.New[progCacheKey, *goja.Program](cacheSize)
	return &ScriptHandler{
		globalSem:    make(chan struct{}, globalConcurrency),
		perTenantCap: perTenantConcurrency,
		tenantSems:   make(map[uuid.UUID]*tenantSlot),
		progCache:    cache,
	}
}

// InvalidateHook removes cached program entries related to the given hook.
// Phase 03 wires this from hooks.update / hooks.delete store events. Wave 1
// uses a full purge for simplicity — LRU v2 does not expose keyed predicates,
// and refill is cheap (single recompile per subsequent invocation).
func (h *ScriptHandler) InvalidateHook(_ uuid.UUID) {
	h.progCache.Purge()
}

// Execute runs the script hook: acquire semaphores → compile/cache →
// fresh goja runtime → harden → bind frozen event → run → parse return.
// Panics, goja interrupts, and return-parse failures all map to DecisionError;
// ctx cancellation maps to DecisionTimeout.
func (h *ScriptHandler) Execute(ctx context.Context, cfg hooks.HookConfig, ev hooks.Event) (hooks.Decision, error) {
	select {
	case h.globalSem <- struct{}{}:
	case <-ctx.Done():
		return hooks.DecisionTimeout, ctx.Err()
	}
	defer func() { <-h.globalSem }()

	tsem := h.acquireTenantSem(ev.TenantID)
	select {
	case tsem <- struct{}{}:
	case <-ctx.Done():
		return hooks.DecisionTimeout, ctx.Err()
	}
	defer func() { <-tsem }()

	source, ok := cfg.Config["source"].(string)
	if !ok || source == "" {
		return hooks.DecisionError, errors.New("script: missing 'source' in config")
	}
	if len(source) > MaxScriptSourceBytes {
		return hooks.DecisionError, fmt.Errorf("script: source exceeds %d bytes", MaxScriptSourceBytes)
	}

	prog, err := h.compile(cfg, source)
	if err != nil {
		return hooks.DecisionError, sanitizeError(err)
	}

	rt := goja.New()
	if err := applyHardening(rt); err != nil {
		return hooks.DecisionError, sanitizeError(err)
	}
	var stdout strings.Builder
	captureStdout(rt, &stdout)
	if err := bindEvent(rt, ev); err != nil {
		return hooks.DecisionError, sanitizeError(err)
	}

	// Watchdog: interrupt the runtime on ctx cancellation. done closes on normal
	// exit so the goroutine always returns.
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			rt.Interrupt("context cancelled")
		case <-done:
		}
	}()

	var (
		result  goja.Value
		execErr error
	)
	func() {
		defer func() {
			if r := recover(); r != nil {
				execErr = fmt.Errorf("script panic: %v", r)
			}
		}()
		if _, perr := rt.RunProgram(prog); perr != nil {
			execErr = perr
			return
		}
		handleFn, ok := goja.AssertFunction(rt.Get("handle"))
		if !ok {
			execErr = errors.New("script: export function handle(event)")
			return
		}
		result, execErr = handleFn(goja.Undefined(), rt.Get("event"))
	}()
	close(done)

	if execErr != nil {
		if ctx.Err() != nil {
			return hooks.DecisionTimeout, ctx.Err()
		}
		return hooks.DecisionError, sanitizeError(execErr)
	}

	dec, reason, updated, parseErr := parseReturn(rt, result)
	if parseErr != nil {
		return hooks.DecisionError, parseErr
	}

	// Wave 1 reserves ask/defer but does not implement them. Treat as block +
	// warn so operators can see that a hook wants human/external arbitration.
	if dec == hooks.DecisionAsk || dec == hooks.DecisionDefer {
		slog.Warn("hooks.decision_not_yet_implemented",
			"hook_id", cfg.ID, "decision", string(dec))
		dec = hooks.DecisionBlock
	}

	// Write non-decision outputs for dispatcher pickup (Phase 03 applies them
	// only when cfg.Source == "builtin"). Standalone tests may not provision a
	// ScriptResult — ScriptResultFrom returns nil in that case, which is fine.
	if r := hooks.ScriptResultFrom(ctx); r != nil {
		r.Reason = reason
		r.UpdatedInput = updated
		r.Stdout = stdout.String()
	}
	return dec, nil
}

// tenantSlotIdleTimeout is how long a per-tenant semaphore can sit unused
// before it becomes eligible for sweep. Picked at 1h: long enough to cover
// idle gaps between user sessions; short enough that long-running multi-tenant
// gateways with churning tenants don't accumulate dead entries forever.
const tenantSlotIdleTimeout = 1 * time.Hour

// tenantSemSweepThreshold defers sweep cost until the map is large enough to
// matter. Below this, the O(N) scan per acquire is wasted work.
const tenantSemSweepThreshold = 64

// acquireTenantSem returns the per-tenant slot channel, lazily allocating on
// first use. Zero-UUID tenants (system calls) share the same slot keyed by
// uuid.Nil, which is acceptable because system-source hooks are still bounded
// by the global cap.
//
// Sweep: when the map crosses tenantSemSweepThreshold entries, idle slots
// (no in-flight grants AND last-used > tenantSlotIdleTimeout ago) are dropped
// opportunistically. Goroutine-free design — sweep happens under the same
// lock the acquire path already takes, so steady-state cost stays one
// CompareAndSwap-equivalent for the common path.
func (h *ScriptHandler) acquireTenantSem(tid uuid.UUID) chan struct{} {
	h.tenantMu.Lock()
	defer h.tenantMu.Unlock()
	if len(h.tenantSems) >= tenantSemSweepThreshold {
		now := time.Now()
		for k, v := range h.tenantSems {
			if k == tid {
				continue
			}
			if len(v.sem) == 0 && now.Sub(v.lastUsed) > tenantSlotIdleTimeout {
				delete(h.tenantSems, k)
			}
		}
	}
	s, ok := h.tenantSems[tid]
	if !ok {
		s = &tenantSlot{sem: make(chan struct{}, h.perTenantCap)}
		h.tenantSems[tid] = s
	}
	s.lastUsed = time.Now()
	return s.sem
}

// sanitizeError trims stack-frame text from goja errors. Two strip passes:
// first newline (multi-line stacks) and first ` at ` (single-line inline
// frames like `Error: boom at handle (<id>:1:32(3))`). Goja embeds source
// file IDs + line/col numbers in both forms; we don't want that leaking to
// tenant admins through dispatcher audit. One-line messages without frame
// info stay intact.
func sanitizeError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if idx := strings.Index(msg, "\n"); idx > 0 {
		msg = msg[:idx]
	}
	if idx := strings.Index(msg, " at "); idx > 0 {
		msg = strings.TrimRight(msg[:idx], " ")
	}
	return errors.New(msg)
}
