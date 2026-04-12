// Package pipeline — Greedy tool call partitioning (CP-02).
// Groups consecutive concurrent-safe tools into batches.
// Exclusive tools always get their own single-item batch.
package pipeline

import (
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// ToolBatch groups tool calls by concurrency safety.
type ToolBatch struct {
	IsConcurrent bool
	Calls        []providers.ToolCall
}

// PartitionToolCalls groups tool calls into concurrent/exclusive batches
// using greedy consecutive grouping.
//
// Example:
//
//	[Read, Read, Grep, Write, Read, Read]
//	→ [Read+Read+Grep (concurrent), Write (exclusive), Read+Read (concurrent)]
//
// Max concurrent batch size defaults to 10.
func PartitionToolCalls(
	calls []providers.ToolCall,
	isSafeFn func(tc providers.ToolCall) bool,
	maxConcurrent int,
) []ToolBatch {
	if len(calls) == 0 {
		return nil
	}
	if maxConcurrent <= 0 {
		maxConcurrent = 10
	}

	batches := make([]ToolBatch, 0, len(calls))

	for _, tc := range calls {
		safe := false
		if isSafeFn != nil {
			safe = isSafeFn(tc)
		}

		lastIdx := len(batches) - 1

		if safe && lastIdx >= 0 && batches[lastIdx].IsConcurrent &&
			len(batches[lastIdx].Calls) < maxConcurrent {
			// Append to existing concurrent batch
			batches[lastIdx].Calls = append(batches[lastIdx].Calls, tc)
		} else {
			// Start new batch
			batches = append(batches, ToolBatch{
				IsConcurrent: safe,
				Calls:        []providers.ToolCall{tc},
			})
		}
	}

	return batches
}

// DefaultIsConcurrencySafe builds a safety check function from a simple
// read-only predicate. Kept for tests and lightweight callers that don't want
// to wire the full tools registry classifier.
func DefaultIsConcurrencySafe(isReadOnly func(name string) bool) func(tc providers.ToolCall) bool {
	return func(tc providers.ToolCall) bool {
		if isReadOnly == nil {
			return false
		}
		return isReadOnly(tc.Name)
	}
}
