// Package pipeline — Layer 4 of Context Defense (CP-01).
// Reactive compact: emergency compaction on 413 (prompt_too_long) errors.
// Single-shot circuit breaker — only attempts once per pipeline run.
package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// ReactiveCompactor handles emergency compaction triggered by prompt_too_long errors.
type ReactiveCompactor struct {
	attempted atomic.Bool
	compactFn func(ctx context.Context, msgs []providers.Message, model string) ([]providers.Message, error)
}

// NewReactiveCompactor creates a compactor using the given compaction function.
func NewReactiveCompactor(
	compactFn func(ctx context.Context, msgs []providers.Message, model string) ([]providers.Message, error),
) *ReactiveCompactor {
	return &ReactiveCompactor{compactFn: compactFn}
}

// HandleError checks if err is a prompt-too-long error and attempts emergency compact.
// Returns (shouldRetry, newError).
// Circuit breaker: only tries once. Second call returns (false, original error).
func (rc *ReactiveCompactor) HandleError(
	ctx context.Context,
	buf *MessageBuffer,
	err error,
	model string,
) (bool, error) {
	if !IsPromptTooLongError(err) {
		return false, err
	}

	if !rc.attempted.CompareAndSwap(false, true) {
		slog.Warn("reactive compact already attempted — surfacing error")
		return false, fmt.Errorf("prompt too long after reactive compact: %w", err)
	}

	slog.Warn("prompt_too_long — attempting reactive compact")

	compacted, compactErr := rc.compactFn(ctx, buf.History(), model)
	if compactErr != nil {
		return false, fmt.Errorf("reactive compact failed: %w (original: %w)", compactErr, err)
	}

	buf.ReplaceHistory(compacted)
	slog.Info("reactive compact succeeded — retrying API call")
	return true, nil
}

// IsPromptTooLongError checks for HTTP 413 or provider-specific prompt length errors.
func IsPromptTooLongError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	patterns := []string{
		"413",
		"prompt_too_long",
		"prompt is too long",
		"maximum context length",
		"input too long",
		"request too large",
		"context_length_exceeded",
	}
	for _, p := range patterns {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}
