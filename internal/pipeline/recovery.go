// Package pipeline — Escalating recovery manager (CP-04).
// 5-tier error recovery: retry → escalate output → inject message → fallback model → surface.
// Each tier has its own budget and circuit breaker. No tier retries the same strategy.
package pipeline

import (
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
)

// RecoveryTier identifies the escalation level.
type RecoveryTier int

const (
	TierRetry          RecoveryTier = iota // Retry same config
	TierEscalateOutput                     // Increase maxOutputTokens
	TierInjectMessage                      // Inject "resume, no recap" message
	TierFallbackModel                      // Switch to fallback model
	TierSurfaceError                       // Give up — surface to user
)

func (t RecoveryTier) String() string {
	switch t {
	case TierRetry:
		return "retry"
	case TierEscalateOutput:
		return "escalate_output"
	case TierInjectMessage:
		return "inject_message"
	case TierFallbackModel:
		return "fallback_model"
	case TierSurfaceError:
		return "surface_error"
	}
	return "unknown"
}

// RecoveryConfig controls escalation behavior.
type RecoveryConfig struct {
	MaxRetries          int    // Tier 1 attempts. Default: 3
	OutputEscalateMulti int    // Tier 2 multiplier (e.g., 4 → 8K becomes 32K). Default: 4
	MaxOutputRecoveries int    // Tier 3 max inject attempts. Default: 3
	FallbackModel       string // Tier 4 model name. Empty = skip tier.
	RecoveryMessage     string // Tier 3 injected message.
}

// DefaultRecoveryConfig returns production defaults.
func DefaultRecoveryConfig() RecoveryConfig {
	return RecoveryConfig{
		MaxRetries:          3,
		OutputEscalateMulti: 4,
		MaxOutputRecoveries: 3,
		FallbackModel:       "",
		RecoveryMessage: `Output token limit hit. Resume directly — no apology, ` +
			`no recap of what you were doing. Pick up mid-thought if that is ` +
			`where the cut happened. Break remaining work into smaller pieces.`,
	}
}

// RecoveryAction tells the pipeline what to do after a failure.
type RecoveryAction struct {
	Tier         RecoveryTier
	ShouldRetry  bool
	NewMaxTokens int    // Tier 2: updated output limit
	InjectMsg    string // Tier 3: message to inject
	NewModel     string // Tier 4: model to switch to
	FinalError   error  // Tier 5: error to surface
}

// RecoveryManager tracks escalation state across a pipeline run.
type RecoveryManager struct {
	cfg RecoveryConfig

	retryCount          atomic.Int32
	hasEscalatedOutput  atomic.Bool
	outputRecoveryCount atomic.Int32
	hasSwitchedModel    atomic.Bool
}

// NewRecoveryManager creates a manager with the given config.
func NewRecoveryManager(cfg RecoveryConfig) *RecoveryManager {
	return &RecoveryManager{cfg: cfg}
}

// Decide examines an error and returns the appropriate recovery action.
//
// Decision tree:
//
//	max_tokens hit + not escalated → Tier 2 (escalate output)
//	max_tokens hit + escalated + count < max → Tier 3 (inject message)
//	max_tokens hit + count >= max → Tier 5 (surface)
//	retryable API error + retries < max → Tier 1 (retry)
//	retryable API error + retries >= max + fallback → Tier 4 (switch model)
//	non-retryable → Tier 5 (surface)
func (rm *RecoveryManager) Decide(err error, currentMaxTokens int) RecoveryAction {
	if err == nil {
		return RecoveryAction{ShouldRetry: false}
	}

	// Max output tokens hit
	if isMaxTokensError(err) {
		// Tier 2: Escalate output (one-shot)
		if !rm.hasEscalatedOutput.Load() {
			rm.hasEscalatedOutput.Store(true)
			newMax := currentMaxTokens * rm.cfg.OutputEscalateMulti
			slog.Info("recovery: escalating output tokens",
				"tier", TierEscalateOutput,
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
				"tier", TierInjectMessage,
				"attempt", count, "max", rm.cfg.MaxOutputRecoveries)
			return RecoveryAction{
				Tier:        TierInjectMessage,
				ShouldRetry: true,
				InjectMsg:   rm.cfg.RecoveryMessage,
			}
		}

		return RecoveryAction{
			Tier:       TierSurfaceError,
			FinalError: fmt.Errorf("max output tokens after %d recovery attempts: %w", count, err),
		}
	}

	// Retryable API errors (429, 500, 502, 503, 504)
	if isRetryableAPIError(err) {
		count := rm.retryCount.Add(1)

		// Tier 1: Retry same config
		if int(count) <= rm.cfg.MaxRetries {
			slog.Info("recovery: retrying API call",
				"tier", TierRetry,
				"attempt", count, "max", rm.cfg.MaxRetries)
			return RecoveryAction{
				Tier:        TierRetry,
				ShouldRetry: true,
			}
		}

		// Tier 4: Fallback model
		if rm.cfg.FallbackModel != "" && !rm.hasSwitchedModel.Load() {
			rm.hasSwitchedModel.Store(true)
			rm.retryCount.Store(0) // reset for new model
			slog.Info("recovery: switching to fallback model",
				"tier", TierFallbackModel,
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

	// Non-retryable → surface immediately
	// IMPORTANT: do NOT run stop hooks on API errors (death spiral prevention)
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
	s := strings.ToLower(err.Error())
	return containsAnyStr(s, "max_tokens", "maximum output", "output_limit",
		"length_limit", "output token limit")
}

func isRetryableAPIError(err error) bool {
	s := strings.ToLower(err.Error())
	return containsAnyStr(s, "429", "500", "502", "503", "504",
		"rate_limit", "rate limit", "overloaded", "service_unavailable",
		"service unavailable", "connection reset", "broken pipe",
		"timeout", "timed out", "temporary failure")
}

func containsAnyStr(s string, patterns ...string) bool {
	for _, p := range patterns {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}
