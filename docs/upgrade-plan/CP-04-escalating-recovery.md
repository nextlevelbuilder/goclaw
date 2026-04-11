# CP-04: Escalating Recovery

**Pattern**: #3 from Agentic OS analysis
**Priority**: HIGH — prevents agent death on transient errors
**Dependencies**: CP-01 (uses reactive compact from Layer 4)
**Estimated effort**: 1 week
**Branch**: `feature/cp-04-escalating-recovery`

---

## Objective

Replace single-tier `RetryDo()` with 5-tier escalating recovery.
Each tier has its own budget and circuit breaker. No tier retries the same strategy.

```
Tier 1: Retry same model (x3, exponential backoff)     ← EXISTS (RetryDo)
Tier 2: Escalate output tokens (default → 4x)          ← NEW
Tier 3: Inject recovery message ("resume, no recap")   ← NEW
Tier 4: Fallback to different model                     ← NEW
Tier 5: Surface error to user                           ← EXISTS
```

---

## Step 1: Create Recovery Manager

### 1.1 Create `internal/pipeline/recovery.go`

```go
package pipeline

import (
	"fmt"
	"log/slog"
	"sync/atomic"
)

// RecoveryTier identifies escalation level.
type RecoveryTier int

const (
	TierRetry          RecoveryTier = iota // Tier 1: retry same config
	TierEscalateOutput                     // Tier 2: increase maxOutputTokens
	TierInjectMessage                      // Tier 3: inject resume message
	TierFallbackModel                      // Tier 4: switch model
	TierSurfaceError                       // Tier 5: give up
)

// RecoveryConfig controls escalation behavior.
type RecoveryConfig struct {
	MaxRetries            int    // Tier 1 attempts. Default: 3
	OutputEscalateMulti   int    // Tier 2 multiplier. Default: 4 (8K → 32K)
	MaxOutputRecoveries   int    // Tier 3 max inject attempts. Default: 3
	FallbackModel         string // Tier 4 model. Empty = skip tier
	RecoveryMessage       string // Tier 3 injected message
}

func DefaultRecoveryConfig() RecoveryConfig {
	return RecoveryConfig{
		MaxRetries:          3,
		OutputEscalateMulti: 4,
		MaxOutputRecoveries: 3,
		FallbackModel:       "", // set per-provider
		RecoveryMessage: `Output token limit hit. Resume directly — no apology, ` +
			`no recap of what you were doing. Pick up mid-thought if that is ` +
			`where the cut happened. Break remaining work into smaller pieces.`,
	}
}

// RecoveryManager tracks escalation state across a pipeline run.
// Each tier's circuit breaker prevents infinite loops.
type RecoveryManager struct {
	cfg RecoveryConfig

	retryCount           atomic.Int32
	hasEscalatedOutput   atomic.Bool
	outputRecoveryCount  atomic.Int32
	hasSwitchedModel     atomic.Bool
}

func NewRecoveryManager(cfg RecoveryConfig) *RecoveryManager {
	return &RecoveryManager{cfg: cfg}
}

// RecoveryAction tells the caller what to do.
type RecoveryAction struct {
	Tier         RecoveryTier
	ShouldRetry  bool
	NewMaxTokens int    // Tier 2: new output token limit
	InjectMsg    string // Tier 3: message to inject into conversation
	NewModel     string // Tier 4: model to switch to
	FinalError   error  // Tier 5: error to surface
}

// Decide examines an error and returns the appropriate recovery action.
//
// Decision tree:
//   max_tokens error + not escalated → Tier 2 (escalate output)
//   max_tokens error + escalated + count < max → Tier 3 (inject message)
//   max_tokens error + count >= max → Tier 5 (surface)
//   retryable API error + retries < max → Tier 1 (retry)
//   retryable API error + retries >= max + fallback available → Tier 4 (switch model)
//   anything else → Tier 5 (surface error)
func (rm *RecoveryManager) Decide(err error, currentMaxTokens int) RecoveryAction {
	if err == nil {
		return RecoveryAction{ShouldRetry: false}
	}

	// Max output tokens hit
	if isMaxTokensError(err) {
		// Tier 2: Escalate output tokens (one-shot)
		if !rm.hasEscalatedOutput.Load() {
			rm.hasEscalatedOutput.Store(true)
			newMax := currentMaxTokens * rm.cfg.OutputEscalateMulti
			slog.Info("recovery: escalating output tokens",
				"from", currentMaxTokens, "to", newMax)
			return RecoveryAction{
				Tier:         TierEscalateOutput,
				ShouldRetry:  true,
				NewMaxTokens: newMax,
			}
		}

		// Tier 3: Inject recovery message
		count := rm.outputRecoveryCount.Add(1)
		if int(count) <= rm.cfg.MaxOutputRecoveries {
			slog.Info("recovery: injecting resume message",
				"attempt", count, "max", rm.cfg.MaxOutputRecoveries)
			return RecoveryAction{
				Tier:        TierInjectMessage,
				ShouldRetry: true,
				InjectMsg:   rm.cfg.RecoveryMessage,
			}
		}

		// Exhausted → surface error
		return RecoveryAction{
			Tier:       TierSurfaceError,
			FinalError: fmt.Errorf("max output tokens exceeded after %d recovery attempts: %w", count, err),
		}
	}

	// Retryable API errors (429, 500, 502, 503, 504)
	if isRetryableAPIError(err) {
		count := rm.retryCount.Add(1)

		// Tier 1: Retry same config
		if int(count) <= rm.cfg.MaxRetries {
			slog.Info("recovery: retrying API call",
				"attempt", count, "max", rm.cfg.MaxRetries)
			return RecoveryAction{
				Tier:        TierRetry,
				ShouldRetry: true,
			}
		}

		// Tier 4: Fallback model
		if rm.cfg.FallbackModel != "" && !rm.hasSwitchedModel.Load() {
			rm.hasSwitchedModel.Store(true)
			rm.retryCount.Store(0) // reset retry counter for new model
			slog.Info("recovery: switching to fallback model",
				"model", rm.cfg.FallbackModel)
			return RecoveryAction{
				Tier:        TierFallbackModel,
				ShouldRetry: true,
				NewModel:    rm.cfg.FallbackModel,
			}
		}

		return RecoveryAction{
			Tier:       TierSurfaceError,
			FinalError: fmt.Errorf("API error after %d retries: %w", count, err),
		}
	}

	// Non-retryable error → surface immediately
	// IMPORTANT: Do NOT run stop hooks on API errors (death spiral prevention).
	// Stop hooks inject tokens → if error is prompt_too_long, more tokens makes it worse.
	return RecoveryAction{
		Tier:       TierSurfaceError,
		FinalError: err,
	}
}

// RecordSuccess resets the retry counter (but not one-shot circuit breakers).
func (rm *RecoveryManager) RecordSuccess() {
	rm.retryCount.Store(0)
}

func isMaxTokensError(err error) bool {
	s := err.Error()
	return containsAny(s, "max_tokens", "maximum output", "output_limit", "length_limit")
}

func isRetryableAPIError(err error) bool {
	s := err.Error()
	return containsAny(s, "429", "500", "502", "503", "504",
		"rate_limit", "overloaded", "service_unavailable",
		"connection reset", "broken pipe", "timeout")
}
```

---

## Step 2: Integrate into ThinkStage

**File**: `internal/pipeline/think_stage.go`

```go
type ThinkStage struct {
	deps              *PipelineDeps
	result            StageResult
	reactiveCompactor *ReactiveCompactor
	streamingExec     *StreamingToolExecutor
	recovery          *RecoveryManager // NEW
}

func (s *ThinkStage) Execute(ctx context.Context, state *RunState) error {
	s.result = Continue

	// ... build request ...

	for {
		resp, err := s.deps.ChatStream(ctx, req, onChunk)

		if err == nil {
			s.recovery.RecordSuccess()
			state.Think.LastResponse = resp
			break
		}

		// === Reactive compact (CP-01 Layer 4) ===
		if s.reactiveCompactor != nil {
			if retry, _ := s.reactiveCompactor.HandleError(ctx, state, err, model); retry {
				continue // retry with compacted context
			}
		}

		// === Escalating recovery ===
		action := s.recovery.Decide(err, req.MaxTokens)

		if !action.ShouldRetry {
			return action.FinalError // Tier 5: surface error
		}

		switch action.Tier {
		case TierRetry:
			continue // retry same config (backoff handled by provider)

		case TierEscalateOutput:
			req.MaxTokens = action.NewMaxTokens
			continue

		case TierInjectMessage:
			state.Messages.AppendPending(Message{
				Role:    "user",
				Content: action.InjectMsg,
			})
			continue

		case TierFallbackModel:
			req.Model = action.NewModel
			continue
		}
	}

	// ... rest of ThinkStage ...
}
```

---

## Step 3: Death Spiral Prevention

**CRITICAL**: When API returns an error, do NOT run stop hooks.
Stop hooks inject tokens into conversation. If the error is prompt_too_long,
adding more tokens makes it worse: error → hook → retry → error → ...

**File**: `internal/pipeline/finalize_stage.go`

```go
func (s *FinalizeStage) Execute(ctx context.Context, state *RunState) error {
	// Skip stop hooks if last error was API error
	if state.Think.LastError != nil && isAPIError(state.Think.LastError) {
		slog.Warn("skipping stop hooks due to API error — death spiral prevention")
		// Still persist state, just skip hooks
		return s.persistState(ctx, state)
	}

	// ... existing hook execution ...
}
```

---

## Verification Checklist

- [ ] Tier 1: 429 error → retry 3x with backoff → success on 2nd try
- [ ] Tier 2: max_tokens → output limit 4x'd → success
- [ ] Tier 3: max_tokens again → recovery message injected → success
- [ ] Tier 3: After 3 inject attempts → surface error (no infinite loop)
- [ ] Tier 4: 3 retries exhausted → fallback model used → success
- [ ] Tier 5: Non-retryable error → immediately surfaced
- [ ] Death spiral: API error → stop hooks SKIPPED
- [ ] RecordSuccess resets retry counter
- [ ] One-shot circuit breakers (escalate, fallback) don't reset

## Test File

Create `internal/pipeline/recovery_test.go`
