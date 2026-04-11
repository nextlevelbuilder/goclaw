// Package pipeline — Greedy tool call partitioning (CP-02).
// Groups consecutive concurrent-safe tools into batches.
// Exclusive tools always get their own single-item batch.
package pipeline

import (
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
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

// DefaultIsConcurrencySafe builds a safety check function using
// the tools registry metadata. Looks up each tool and calls
// IsConcurrencySafeForTool with the tool's metadata.
func DefaultIsConcurrencySafe(registry interface {
	GetMetadata(name string) tools.ToolMetadata
}) func(tc providers.ToolCall) bool {
	return func(tc providers.ToolCall) bool {
		meta := registry.GetMetadata(tc.Name)
		// Check if tool has a per-invocation classifier
		// For now, use static metadata (enhanced per-tool in future)
		return meta.IsReadOnly()
	}
}
