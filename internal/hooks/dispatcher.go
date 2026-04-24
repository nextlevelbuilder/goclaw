package hooks

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// ── Public surface ───────────────────────────────────────────────────────────

// Handler executes a single hook config against an event. Returned Decision
// drives the blocking-path outcome; the audit writer records the row.
type Handler interface {
	Execute(ctx context.Context, cfg HookConfig, ev Event) (Decision, error)
}

// Dispatcher is the pipeline integration surface. Stages call Fire and act on
// FireResult.Decision (allow → continue; block → abort with i18n error).
// FireResult.Updated* carry mutations authored by builtin-source hooks —
// callers apply them to their own tc.Arguments / state.Input when non-nil.
type Dispatcher interface {
	Fire(ctx context.Context, ev Event) (FireResult, error)
}

// MaxLoopDepth caps nested hook invocation (M5). Depth increments when a hook
// triggers a sub-agent whose own events feed back into the dispatcher.
const MaxLoopDepth = 3

// ErrLoopDepthExceeded signals the M5 circuit: refuse to process further
// events once the chain exceeds MaxLoopDepth.
var ErrLoopDepthExceeded = errors.New("hooks: loop depth exceeded")

// Defaults for the dispatcher knobs. Set via StdDispatcherOpts when a test or
// deployment needs tighter timings (unit tests use sub-second values).
const (
	defaultPerHookTimeout   = 5 * time.Second
	defaultChainBudget      = 10 * time.Second
	defaultCircuitThreshold = 5
	defaultCircuitWindow    = 1 * time.Minute
)

// ctxDepthKey is the context-value key for the nested-hook depth counter.
// Private type prevents collisions with other packages' context keys.
type ctxDepthKey struct{}

// WithDepth stores d in ctx; Fire reads it to enforce MaxLoopDepth (M5).
// Callers that dispatch sub-agent events must increment before re-entering.
func WithDepth(ctx context.Context, d int) context.Context {
	return context.WithValue(ctx, ctxDepthKey{}, d)
}

func depthFromCtx(ctx context.Context) int {
	if v, ok := ctx.Value(ctxDepthKey{}).(int); ok {
		return v
	}
	return 0
}

// DepthFrom returns the current hook loop depth from ctx (0 if unset).
func DepthFrom(ctx context.Context) int { return depthFromCtx(ctx) }

// IncDepth returns a new ctx with depth incremented by 1.
// Callers wrapping sub-agent invocations must call this before re-entering.
func IncDepth(ctx context.Context) context.Context {
	return WithDepth(ctx, depthFromCtx(ctx)+1)
}

// StdDispatcherOpts configures the production dispatcher. Unset fields fall
// back to the default* constants above.
type StdDispatcherOpts struct {
	Store    HookStore
	Audit    *AuditWriter
	Handlers map[HandlerType]Handler

	PerHookTimeout time.Duration
	ChainBudget    time.Duration

	CircuitThreshold int
	CircuitWindow    time.Duration

	// Now is injectable for tests that need deterministic circuit-breaker timing.
	Now func() time.Time
}

// NewStdDispatcher returns the production Dispatcher with circuit-breaker,
// per-hook timeouts, and audit writing wired up.
func NewStdDispatcher(opts StdDispatcherOpts) Dispatcher {
	if opts.PerHookTimeout <= 0 {
		opts.PerHookTimeout = defaultPerHookTimeout
	}
	if opts.ChainBudget <= 0 {
		opts.ChainBudget = defaultChainBudget
	}
	if opts.CircuitThreshold <= 0 {
		opts.CircuitThreshold = defaultCircuitThreshold
	}
	if opts.CircuitWindow <= 0 {
		opts.CircuitWindow = defaultCircuitWindow
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Handlers == nil {
		opts.Handlers = map[HandlerType]Handler{}
	}
	return &stdDispatcher{
		store:       opts.Store,
		audit:       opts.Audit,
		handlers:    opts.Handlers,
		perTimeout:  opts.PerHookTimeout,
		chainBudget: opts.ChainBudget,
		now:         opts.Now,
		cb: &circuitBreaker{
			threshold: opts.CircuitThreshold,
			window:    opts.CircuitWindow,
			hits:      map[uuid.UUID][]time.Time{},
			tripped:   map[uuid.UUID]bool{},
		},
	}
}

// NewNoopDispatcher returns a dispatcher that always allows. Used when the
// hook system is disabled (e.g. CI without DB) or before init completes.
func NewNoopDispatcher() Dispatcher {
	return noopDispatcher{}
}

type noopDispatcher struct{}

func (noopDispatcher) Fire(context.Context, Event) (FireResult, error) {
	return FireResult{Decision: DecisionAllow}, nil
}

// ── stdDispatcher ────────────────────────────────────────────────────────────

type stdDispatcher struct {
	store       HookStore
	audit       *AuditWriter
	handlers    map[HandlerType]Handler
	perTimeout  time.Duration
	chainBudget time.Duration
	now         func() time.Time
	cb          *circuitBreaker
}

func (d *stdDispatcher) Fire(ctx context.Context, ev Event) (FireResult, error) {
	if depthFromCtx(ctx) > MaxLoopDepth {
		slog.Warn("security.hook.loop_depth_exceeded",
			"event_id", ev.EventID,
			"hook_event", ev.HookEvent,
		)
		return FireResult{Decision: DecisionError}, ErrLoopDepthExceeded
	}

	hooks, err := d.store.ResolveForEvent(ctx, ev)
	if err != nil {
		// Fail-closed: a DB blip must not let a pre-tool gate open silently.
		slog.Warn("security.hook.resolve_error", "err", err, "event_id", ev.EventID)
		if ev.HookEvent.IsBlocking() {
			return FireResult{Decision: DecisionBlock}, err
		}
		return FireResult{Decision: DecisionAllow}, err
	}
	if len(hooks) == 0 {
		return FireResult{Decision: DecisionAllow}, nil
	}

	if ev.HookEvent.IsBlocking() {
		return d.runSync(ctx, ev, hooks)
	}
	d.runAsync(ctx, ev, hooks)
	return FireResult{Decision: DecisionAllow}, nil
}

// runSync executes the blocking chain with a wall-time budget and per-hook
// timeouts. First block wins; any fail-closed condition aborts to Block.
//
// Mutation propagation (Phase 03): the dispatcher keeps a local evMut copy
// with its own ToolInput map so a builtin-source hook's updatedInput becomes
// visible to downstream hooks in the chain (and to the caller via FireResult)
// without leaking mutations back into the caller's original event on error
// paths. Non-builtin scripts returning updatedInput get their mutation
// dropped + logged — source tier enforced at the dispatcher.
func (d *stdDispatcher) runSync(ctx context.Context, ev Event, chain []HookConfig) (FireResult, error) {
	chainCtx, cancel := context.WithTimeout(ctx, d.chainBudget)
	defer cancel()

	evMut := ev
	if ev.ToolInput != nil {
		evMut.ToolInput = cloneMap(ev.ToolInput)
	}
	mutated := false

	for _, cfg := range chain {
		if !cfg.Enabled {
			continue
		}
		if d.cb.isTripped(cfg.ID, d.now()) {
			d.writeExec(ctx, cfg, evMut, DecisionBlock, 0, "circuit breaker open")
			return FireResult{Decision: DecisionBlock}, nil
		}
		if !d.prefilter(cfg, evMut) {
			continue
		}

		// Attach fresh ScriptResult carrier per-hook so the script handler can
		// report its non-decision outputs (reason/updatedInput/stdout). Other
		// handler types ignore the ctx value — zero-cost.
		scriptRes := &ScriptResult{}
		hctx := WithScriptResult(chainCtx, scriptRes)

		dec, execErr, duration := d.runOne(hctx, cfg, evMut)
		errMsg := ""
		if execErr != nil {
			errMsg = execErr.Error()
		}
		d.writeExec(ctx, cfg, evMut, dec, duration, errMsg)

		// Apply mutation to the local copy only when the hook is builtin-source.
		// Non-builtin scripts get their updatedInput stripped + warned.
		if cfg.HandlerType == HandlerScript && dec == DecisionAllow && scriptRes.UpdatedInput != nil {
			if cfg.Source == SourceBuiltin {
				applyBuiltinMutation(&evMut, scriptRes.UpdatedInput, builtinAllowlistFor(cfg.ID))
				mutated = true
			} else {
				slog.Warn("hooks.script_mutation_denied",
					"hook_id", cfg.ID,
					"source", cfg.Source,
					"field_count", len(scriptRes.UpdatedInput),
				)
			}
		}

		switch dec {
		case DecisionBlock:
			d.cb.record(ctx, cfg.ID, d.now(), d.store)
			return FireResult{Decision: DecisionBlock}, nil
		case DecisionTimeout:
			d.cb.record(ctx, cfg.ID, d.now(), d.store)
			if cfg.OnTimeout == DecisionBlock {
				return FireResult{Decision: DecisionBlock}, nil
			}
			// OnTimeout=allow: degrade gracefully but keep scanning.
		case DecisionError:
			// Unexpected error in a blocking chain → fail-closed.
			return FireResult{Decision: DecisionBlock}, nil
		}

		if chainCtx.Err() != nil {
			// Chain wall-time budget exhausted (H3): fail-closed.
			return FireResult{Decision: DecisionBlock}, nil
		}
	}

	result := FireResult{Decision: DecisionAllow}
	if mutated {
		if evMut.ToolInput != nil {
			result.UpdatedToolInput = evMut.ToolInput
		}
		if evMut.RawInput != ev.RawInput {
			s := evMut.RawInput
			result.UpdatedRawInput = &s
		}
	}
	return result, nil
}

// cloneMap returns a shallow copy of m. Used so runSync's mutations on the
// local evMut don't leak back to the caller's event when a downstream hook
// blocks or the chain aborts.
func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	maps.Copy(out, m)
	return out
}

// applyBuiltinMutation applies the builtin hook's updatedInput fields into ev,
// gated by the per-builtin allowlist. The allowlist is loaded in Phase 04 from
// builtins.yaml; Phase 03 uses a permissive default (rawInput + toolInput).
func applyBuiltinMutation(ev *Event, updated map[string]any, allowlist []string) {
	if updated == nil {
		return
	}
	allowSet := make(map[string]struct{}, len(allowlist))
	for _, f := range allowlist {
		allowSet[f] = struct{}{}
	}
	if _, ok := allowSet["rawInput"]; ok {
		if s, ok2 := updated["rawInput"].(string); ok2 {
			ev.RawInput = s
		}
	}
	if m, ok := updated["toolInput"].(map[string]any); ok {
		if _, wildcard := allowSet["toolInput"]; wildcard {
			if ev.ToolInput == nil {
				ev.ToolInput = map[string]any{}
			}
			maps.Copy(ev.ToolInput, m)
		} else {
			for k, v := range m {
				if _, ok := allowSet["toolInput."+k]; ok {
					if ev.ToolInput == nil {
						ev.ToolInput = map[string]any{}
					}
					ev.ToolInput[k] = v
				}
			}
		}
	}
}

// builtinAllowlistLookup is set at startup by the builtin package wiring
// (cmd/gateway_managed.go calls SetBuiltinAllowlistLookup(builtin.AllowlistFor)).
// When nil (tests that don't wire the registry) we fall back to the Phase 03
// permissive default so legacy mutation tests stay green.
//
// atomic.Pointer guards against the data race that fires when parallel tests
// install/clear the lookup while the dispatcher reads it on another goroutine.
var builtinAllowlistLookup atomic.Pointer[func(uuid.UUID) []string]

// SetBuiltinAllowlistLookup installs the per-id allowlist function. Called
// once at startup from cmd/gateway_managed.go. Safe to leave unset in tests —
// callers get the permissive default (rawInput + toolInput) below. Pass nil
// to clear (used by `defer` cleanup in tests).
func SetBuiltinAllowlistLookup(f func(uuid.UUID) []string) {
	if f == nil {
		builtinAllowlistLookup.Store(nil)
		return
	}
	builtinAllowlistLookup.Store(&f)
}

// builtinAllowlistFor returns the field allowlist for a builtin hook row.
// Registered builtin → YAML mutable_fields. Unknown id under a wired lookup
// → empty allowlist (mutations stripped, defense-in-depth). Unset lookup
// (unit tests) → Phase 03 permissive default.
func builtinAllowlistFor(id uuid.UUID) []string {
	if fp := builtinAllowlistLookup.Load(); fp != nil {
		return (*fp)(id)
	}
	return []string{"rawInput", "toolInput"}
}

// SourceBuiltin is the `source` value marking a hook seeded by the builtin
// infrastructure (Phase 04 loader). Only builtin-source hooks are allowed to
// mutate event input through the script handler's updatedInput field.
const SourceBuiltin = "builtin"

// runAsync fires non-blocking hooks concurrently. Phase 1 uses a simple
// goroutine-per-hook; Phase 2 will route through the eventbus worker pool.
func (d *stdDispatcher) runAsync(ctx context.Context, ev Event, chain []HookConfig) {
	for _, cfg := range chain {
		if !cfg.Enabled || !d.prefilter(cfg, ev) {
			continue
		}
		go func(c HookConfig) {
			dec, execErr, duration := d.runOne(ctx, c, ev)
			errMsg := ""
			if execErr != nil {
				errMsg = execErr.Error()
			}
			// Use WithoutCancel to preserve context values (TenantID, UserID)
			// but detach from parent deadline, then add a timeout to prevent indefinite hang
			writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			d.writeExec(writeCtx, c, ev, dec, duration, errMsg)
		}(cfg)
	}
}

// runOne executes a single hook with its per-hook timeout. Returns the
// decision, any error from the handler, and the elapsed duration.
func (d *stdDispatcher) runOne(ctx context.Context, cfg HookConfig, ev Event) (Decision, error, time.Duration) {
	handler, ok := d.handlers[cfg.HandlerType]
	if !ok {
		return DecisionError, fmt.Errorf("hook: no handler registered for %q", cfg.HandlerType), 0
	}

	timeout := d.perTimeout
	if cfg.TimeoutMS > 0 {
		timeout = time.Duration(cfg.TimeoutMS) * time.Millisecond
	}
	hctx, hcancel := context.WithTimeout(ctx, timeout)
	defer hcancel()

	start := d.now()
	dec, err := handler.Execute(hctx, cfg, ev)
	duration := d.now().Sub(start)

	if errors.Is(hctx.Err(), context.DeadlineExceeded) {
		return DecisionTimeout, hctx.Err(), duration
	}
	return dec, err, duration
}

// prefilter applies the matcher + CEL gate; Phase 1 keeps it lightweight and
// pushes the actual compile through matcher.go's cached helpers.
func (d *stdDispatcher) prefilter(cfg HookConfig, ev Event) bool {
	if cfg.Matcher != "" {
		re, err := CompileMatcher(cfg.Matcher)
		if err != nil || !MatchToolName(re, ev.ToolName) {
			return false
		}
	}
	if cfg.IfExpr != "" {
		prg, err := CompileCELExpr(cfg.IfExpr)
		if err != nil {
			return false
		}
		ok, err := EvalCEL(prg, map[string]any{
			"tool_name":  ev.ToolName,
			"tool_input": ev.ToolInput,
			"depth":      ev.Depth,
		})
		if err != nil || !ok {
			return false
		}
	}
	return true
}

// writeExec assembles the audit row and routes it through AuditWriter which
// handles truncation, redaction, and encryption. Failures are logged but do
// not propagate — audit is observability, not policy.
func (d *stdDispatcher) writeExec(ctx context.Context, cfg HookConfig, ev Event, dec Decision, duration time.Duration, errMsg string) {
	if d.audit == nil {
		return
	}
	inputHash, _ := CanonicalInputHash(ev.ToolName, ev.ToolInput)
	hookID := cfg.ID
	exec := HookExecution{
		ID:         uuid.New(),
		HookID:     &hookID,
		SessionID:  ev.SessionID,
		Event:      ev.HookEvent,
		InputHash:  inputHash,
		Decision:   dec,
		DurationMS: int(duration / time.Millisecond),
		DedupKey:   cfg.ID.String() + ":" + ev.EventID,
		Error:      errMsg,
		Metadata:   map[string]any{},
		CreatedAt:  d.now(),
	}
	if err := d.audit.Log(ctx, exec); err != nil {
		slog.Warn("security.hook.audit_write_failed", "err", err, "hook_id", cfg.ID)
	}
}

// ── circuitBreaker ───────────────────────────────────────────────────────────

// circuitBreaker tracks recent block/timeout timestamps per hook; once the
// count inside the rolling window hits the threshold it persists the hook as
// disabled and short-circuits subsequent Fires (C4 mitigation).
type circuitBreaker struct {
	mu        sync.Mutex
	threshold int
	window    time.Duration
	hits      map[uuid.UUID][]time.Time
	tripped   map[uuid.UUID]bool
}

func (cb *circuitBreaker) isTripped(id uuid.UUID, _ time.Time) bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.tripped[id]
}

// record appends a block/timeout event and, if the window count hits the
// threshold, trips the breaker and asks the store to persist enabled=false.
// Persistence failure is logged only — the in-memory trip still protects the
// current process.
func (cb *circuitBreaker) record(ctx context.Context, id uuid.UUID, now time.Time, store HookStore) {
	cb.mu.Lock()
	cutoff := now.Add(-cb.window)
	kept := cb.hits[id][:0]
	for _, t := range cb.hits[id] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	kept = append(kept, now)
	cb.hits[id] = kept
	justTripped := !cb.tripped[id] && len(kept) >= cb.threshold
	if justTripped {
		cb.tripped[id] = true
	}
	cb.mu.Unlock()

	if !justTripped {
		return
	}
	slog.Warn("security.hook.circuit_breaker",
		"hook_id", id,
		"window", cb.window.String(),
		"threshold", cb.threshold,
	)
	if store == nil {
		return
	}
	// Use WithoutCancel to preserve context values, add 2s timeout for store update
	storeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if err := store.Update(storeCtx, id, map[string]any{"enabled": false}); err != nil {
		slog.Warn("security.hook.circuit_breaker_persist_failed", "hook_id", id, "err", err)
	}
}
