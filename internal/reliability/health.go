package reliability

import (
	"sort"
	"sync"
	"time"
)

// ModelHealth is the runtime reliability health snapshot for one
// provider:model pair. It is NOT an intelligence benchmark — it reflects how
// reliably the combination has behaved recently at the transport / tool-call
// level.
type ModelHealth struct {
	Provider           string
	Model              string
	ConsecutiveFailures int
	RateLimitUntil     time.Time
	TimeoutCount       int
	StreamStallCount   int
	ToolErrorRate      float64 // 0..1 over recent tool calls
	ToolCalls          int     // total tool calls observed
	ToolErrors         int
	EmptyOutputCount   int
	PrematureCompleteCount int
	Attempts           int
	Successes          int
	LastSuccessAt      time.Time
	LastFailureAt      time.Time
	CircuitState       CircuitState
}

// HealthRegistry tracks per-key health and computes a runtime reliability
// score. It shares a CircuitBreaker for state transitions.
type HealthRegistry struct {
	mu       sync.Mutex
	breaker  *CircuitBreaker
	entries  map[string]*modelHealthEntry
	nowFn    func() time.Time
}

type modelHealthEntry struct {
	attempts       int
	successes      int
	timeouts       int
	streamStalls   int
	emptyOutputs   int
	prematureCompletes int
	toolCalls      int
	toolErrors     int
	rateLimitUntil time.Time
	consecutiveFails int
	lastSuccessAt  time.Time
	lastFailureAt  time.Time
}

// NewHealthRegistry builds a registry sharing the given circuit breaker.
func NewHealthRegistry(breaker *CircuitBreaker) *HealthRegistry {
	nowFn := time.Now
	if breaker != nil && breaker.opts.nowFn != nil {
		nowFn = breaker.opts.nowFn
	}
	return &HealthRegistry{
		breaker: breaker,
		entries: make(map[string]*modelHealthEntry),
		nowFn:   nowFn,
	}
}

// ObserveSuccess records a successful LLM call for a provider:model.
func (h *HealthRegistry) ObserveSuccess(provider, model string) {
	key := key(provider, model)
	if h.breaker != nil {
		h.breaker.RecordSuccess(key)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	e := h.ensure(key)
	e.attempts++
	e.successes++
	e.consecutiveFails = 0
	e.lastSuccessAt = h.nowFn()
}

// ObserveFailure records a failed LLM call keyed by error code.
// Fatal/permanent codes still count as failures; every failure tightens the
// circuit breaker regardless of code.
func (h *HealthRegistry) ObserveFailure(provider, model string, code ErrorCode) {
	key := key(provider, model)
	if h.breaker != nil {
		h.breaker.RecordFailure(key)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	e := h.ensure(key)
	e.attempts++
	e.consecutiveFails++
	e.lastFailureAt = h.nowFn()

	switch code {
	case ErrProviderRateLimited:
		e.rateLimitUntil = h.nowFn().Add(30 * time.Second)
	case ErrProviderTimeout:
		e.timeouts++
	case ErrModelEmptyOutput:
		e.emptyOutputs++
	case ErrModelPrematureCompletion:
		e.prematureCompletes++
	}
}

// ObserveTimeout and friends are thin wrappers that record specific failure
// modes so the score can reflect them.
func (h *HealthRegistry) ObserveTimeout(provider, model string) {
	h.ObserveFailure(provider, model, ErrProviderTimeout)
}

// ObserveStreamStall records a stalled stream event.
func (h *HealthRegistry) ObserveStreamStall(provider, model string) {
	key := key(provider, model)
	h.mu.Lock()
	defer h.mu.Unlock()
	e := h.ensure(key)
	e.streamStalls++
}

// ObserveToolResult records the outcome of a tool call for health scoring.
func (h *HealthRegistry) ObserveToolResult(provider, model string, ok bool) {
	key := key(provider, model)
	h.mu.Lock()
	defer h.mu.Unlock()
	e := h.ensure(key)
	e.toolCalls++
	if !ok {
		e.toolErrors++
	}
}

// Status returns a snapshot of the health entry for a provider:model.
func (h *HealthRegistry) Status(provider, model string) ModelHealth {
	key := key(provider, model)
	h.mu.Lock()
	e := h.entries[key]
	h.mu.Unlock()

	state := CircuitHealthy
	if h.breaker != nil {
		state = h.breaker.State(key)
	}

	if e == nil {
		return ModelHealth{
			Provider:     provider,
			Model:        model,
			CircuitState: state,
		}
	}

	return ModelHealth{
		Provider:            provider,
		Model:               model,
		ConsecutiveFailures: e.consecutiveFails,
		RateLimitUntil:      e.rateLimitUntil,
		TimeoutCount:        e.timeouts,
		StreamStallCount:    e.streamStalls,
		ToolErrorRate:       toolErrorRate(e),
		ToolCalls:           e.toolCalls,
		ToolErrors:          e.toolErrors,
		EmptyOutputCount:    e.emptyOutputs,
		PrematureCompleteCount: e.prematureCompletes,
		Attempts:            e.attempts,
		Successes:           e.successes,
		LastSuccessAt:       e.lastSuccessAt,
		LastFailureAt:       e.lastFailureAt,
		CircuitState:        state,
	}
}

// Keys returns the sorted list of provider:model keys that have observed
// activity. It lets operator tooling enumerate registry entries without
// reaching into the internal map.
func (h *HealthRegistry) Keys() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	keys := make([]string, 0, len(h.entries))
	for k := range h.entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Score computes a 0..1 runtime reliability score for a provider:model.
// 1.0 = perfectly reliable. The score blends success ratio, timeout rate,
// stream-stall rate, tool-error rate, empty-output rate and circuit state.
func (h *HealthRegistry) Score(provider, model string) float64 {
	status := h.Status(provider, model)
	if status.Attempts == 0 {
		return 1.0 // no signal yet — default to healthy
	}

	// Base: success ratio. Timeouts, empty outputs and premature completions
	// are all already reflected here — they count toward attempts but not
	// successes — so they must NOT be penalized again below. Penalizing them a
	// second time made healthy providers look worse than they are and skewed
	// provider selection.
	success := float64(status.Successes) / float64(status.Attempts)
	// Bounded penalties for signals that do NOT reduce the success ratio:
	// a stalled stream and a failed tool call can occur on an otherwise
	// successful LLM completion, so they add independent cost.
	penalty := 0.0
	penalty += float64(status.StreamStallCount) / float64(status.Attempts) * 0.25
	if status.ToolCalls > 0 {
		penalty += status.ToolErrorRate * 0.2
	}

	score := success - penalty
	if score < 0 {
		score = 0
	}

	// Circuit state modifier.
	switch status.CircuitState {
	case CircuitOpen:
		score *= 0.1
	case CircuitHalfOpen:
		score *= 0.5
	case CircuitDegraded:
		score *= 0.8
	}
	if score < 0 {
		score = 0
	}
	return score
}

func toolErrorRate(e *modelHealthEntry) float64 {
	if e.toolCalls == 0 {
		return 0
	}
	return float64(e.toolErrors) / float64(e.toolCalls)
}

func (h *HealthRegistry) ensure(key string) *modelHealthEntry {
	e, ok := h.entries[key]
	if !ok {
		e = &modelHealthEntry{}
		h.entries[key] = e
	}
	return e
}

func key(provider, model string) string {
	return provider + ":" + model
}