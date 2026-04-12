// Package pipeline — Layer 2 of Context Defense (CP-01).
// Microcompact: remove stale tool results without calling LLM.
package pipeline

import (
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// MicrocompactConfig controls stale tool result removal.
type MicrocompactConfig struct {
	// StaleAfterTurns: tool results older than N iterations get stubbed.
	StaleAfterTurns int
	// MinTokensSaved: only compact if we'd free at least this many tokens.
	MinTokensSaved int
}

// DefaultMicrocompactConfig returns production defaults.
func DefaultMicrocompactConfig() MicrocompactConfig {
	return MicrocompactConfig{
		StaleAfterTurns: 10,
		MinTokensSaved:  500,
	}
}

// MicrocompactResult tracks what was removed.
type MicrocompactResult struct {
	MessagesStubbed int
	TokensFreed     int
}

// Microcompact replaces stale tool results with short stubs.
// Operates on a slice of messages and returns modified slice + metrics.
// Does NOT call LLM — pure string replacement.
func Microcompact(msgs []providers.Message, currentIteration int, cfg MicrocompactConfig) ([]providers.Message, MicrocompactResult) {
	var result MicrocompactResult

	type candidate struct {
		idx   int
		freed int
	}
	var candidates []candidate
	totalFreed := 0

	for i, msg := range msgs {
		if msg.Role != "tool" {
			continue
		}

		// Estimate age based on position ratio (messages don't have Turn field in providers.Message)
		// Use position-based heuristic: stale if in first N% of conversation
		age := currentIteration - messageIterationEstimate(i, len(msgs), currentIteration)
		if age <= cfg.StaleAfterTurns {
			continue
		}

		content := messageContent(msg)
		originalTokens := estimateTokens(content)
		stubTokens := 15
		freed := originalTokens - stubTokens
		if freed > 0 {
			candidates = append(candidates, candidate{idx: i, freed: freed})
			totalFreed += freed
		}
	}

	if totalFreed < cfg.MinTokensSaved {
		return msgs, result
	}

	// Apply stubs
	out := make([]providers.Message, len(msgs))
	copy(out, msgs)
	for _, c := range candidates {
		stub := "[Tool result removed — stale. Re-run tool if needed.]"
		out[c.idx] = replaceMessageContent(out[c.idx], stub)
		result.MessagesStubbed++
	}
	result.TokensFreed = totalFreed

	return out, result
}

// estimateTokens gives a rough token count (~4 chars per token for English).
func estimateTokens(s string) int {
	return len(s) / 4
}

// messageIterationEstimate maps message position to approximate iteration.
func messageIterationEstimate(msgIdx, totalMsgs, currentIteration int) int {
	if totalMsgs <= 0 || currentIteration <= 0 {
		return 0
	}
	return (msgIdx * currentIteration) / totalMsgs
}

// messageContent extracts text content from a providers.Message.
func messageContent(msg providers.Message) string {
	return msg.Content
}

// replaceMessageContent creates a copy of msg with replaced text content.
func replaceMessageContent(msg providers.Message, newContent string) providers.Message {
	msg.Content = newContent
	return msg
}

// FormatMicrocompactNotice generates a notice for the agent about removed content.
func FormatMicrocompactNotice(result MicrocompactResult) string {
	if result.MessagesStubbed == 0 {
		return ""
	}
	return fmt.Sprintf("[System: %d stale tool results removed to free ~%d tokens. Re-run tools if you need that data.]",
		result.MessagesStubbed, result.TokensFreed)
}
