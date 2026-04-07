package providers

import (
	"context"
	"fmt"
	"strings"
)

// ModelCandidate represents a provider+model+key combination for failover.
type ModelCandidate struct {
	Provider  string
	Model     string
	ProfileID string // opaque identifier (never raw API key)
}

// FailoverConfig controls the failover behavior.
type FailoverConfig struct {
	Candidates            []ModelCandidate
	Classifier            ErrorClassifier
	Tracker               *CooldownTracker // optional cooldown tracker (nil = no cooldown)
	OverloadRotationLimit int              // max profile rotations on overloaded before model fallback (default 3)
	MaxProfileRotations   int              // max total profile rotations per model (default 5)
}

// FailoverAttempt records a single failover attempt for diagnostics.
type FailoverAttempt struct {
	Candidate      ModelCandidate
	Classification FailoverClassification
	Err            error
}

// FailoverSummaryError wraps all attempts when failover is exhausted.
type FailoverSummaryError struct {
	Attempts []FailoverAttempt
}

func (e *FailoverSummaryError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "all %d failover candidates exhausted:", len(e.Attempts))
	for i, a := range e.Attempts {
		fmt.Fprintf(&b, " [%d] %s/%s: %s (%v)", i+1, a.Candidate.Provider, a.Candidate.Model, a.Classification.Reason, a.Err)
	}
	return b.String()
}

// isProfileRotatable returns true for transient errors where rotating API key/profile may help.
func isProfileRotatable(reason FailoverReason) bool {
	switch reason {
	case FailoverRateLimit, FailoverOverloaded, FailoverTimeout, FailoverAuth:
		return true
	}
	return false
}

// isModelFallbackRequired returns true for permanent errors requiring a different model.
func isModelFallbackRequired(reason FailoverReason) bool {
	switch reason {
	case FailoverAuthPermanent, FailoverBilling, FailoverFormat, FailoverModelNotFound:
		return true
	}
	return false
}

// RunWithFailover executes runFn against candidates with two-tier failover:
// Tier 1: rotate profiles (same model) for transient errors.
// Tier 2: fall back to next model for permanent errors.
func RunWithFailover[T any](
	ctx context.Context,
	cfg FailoverConfig,
	runFn func(ctx context.Context, candidate ModelCandidate) (T, error),
) (T, []FailoverAttempt, error) {
	var zero T
	if len(cfg.Candidates) == 0 {
		return zero, nil, fmt.Errorf("failover: no candidates provided")
	}
	if cfg.Classifier == nil {
		cfg.Classifier = NewDefaultClassifier()
	}
	if cfg.OverloadRotationLimit <= 0 {
		cfg.OverloadRotationLimit = 3
	}
	if cfg.MaxProfileRotations <= 0 {
		cfg.MaxProfileRotations = 5
	}

	var attempts []FailoverAttempt
	overloadRotations := 0
	profileRotations := 0
	currentModel := cfg.Candidates[0].Model

	for i := 0; i < len(cfg.Candidates); i++ {
		if ctx.Err() != nil {
			return zero, attempts, ctx.Err()
		}

		candidate := cfg.Candidates[i]

		// Reset counters on model change
		if candidate.Model != currentModel {
			currentModel = candidate.Model
			overloadRotations = 0
			profileRotations = 0
		}

		// Cooldown check: skip candidates in active cooldown (unless probe allowed)
		if cfg.Tracker != nil {
			key := CooldownKey(candidate.Provider, candidate.Model)
			if !cfg.Tracker.IsAvailable(key) && !cfg.Tracker.ShouldProbe(key) {
				continue // in cooldown, no probe — skip
			}
		}

		result, err := runFn(ctx, candidate)
		if err == nil {
			if cfg.Tracker != nil {
				cfg.Tracker.RecordSuccess(CooldownKey(candidate.Provider, candidate.Model))
			}
			return result, attempts, nil
		}

		classification := ClassifyHTTPError(cfg.Classifier, err)
		attempts = append(attempts, FailoverAttempt{
			Candidate:      candidate,
			Classification: classification,
			Err:            err,
		})

		// Record failure in cooldown tracker
		if cfg.Tracker != nil && classification.Kind == "reason" {
			cfg.Tracker.RecordFailure(
				CooldownKey(candidate.Provider, candidate.Model),
				classification.Reason,
			)
		}

		// Context overflow is not a failover case — return immediately for compaction
		if classification.Kind == "context_overflow" {
			return zero, attempts, err
		}

		reason := classification.Reason

		// Tier 1: Profile rotation for transient errors
		if isProfileRotatable(reason) {
			profileRotations++

			// Cap overload rotations
			if reason == FailoverOverloaded {
				overloadRotations++
				if overloadRotations >= cfg.OverloadRotationLimit {
					// Escalate to Tier 2: skip remaining profiles for this model
					i = skipToNextModel(cfg.Candidates, i)
					continue
				}
			}

			// Cap total profile rotations
			if profileRotations >= cfg.MaxProfileRotations {
				i = skipToNextModel(cfg.Candidates, i)
				continue
			}

			// Try next candidate (which may be same model, different profile)
			continue
		}

		// Tier 2: Model fallback for permanent errors
		if isModelFallbackRequired(reason) {
			i = skipToNextModel(cfg.Candidates, i)
			continue
		}

		// Unknown: try next candidate
		continue
	}

	return zero, attempts, &FailoverSummaryError{Attempts: attempts}
}

// skipToNextModel advances the index past all remaining candidates with the same model.
// Returns the index of the last candidate for the current model (loop's i++ moves to next model).
func skipToNextModel(candidates []ModelCandidate, current int) int {
	currentModel := candidates[current].Model
	for j := current + 1; j < len(candidates); j++ {
		if candidates[j].Model != currentModel {
			return j - 1 // loop's i++ will advance to j
		}
	}
	return len(candidates) - 1 // exhausted
}
