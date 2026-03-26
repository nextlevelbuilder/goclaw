package agent

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Tool loop detection thresholds (per-run, not per-session).
const (
	toolLoopHistorySize       = 30
	toolLoopWarningThreshold  = 3 // inject warning into conversation
	toolLoopCriticalThreshold = 5 // force stop the iteration loop

	// Read-only streak: consecutive non-mutating tool calls without any write/edit.
	readOnlyStreakWarning  = 8
	readOnlyStreakCritical = 12

	// Same-result: same tool returning identical results with different args.
	sameResultWarning  = 4
	sameResultCritical = 6
)

// mutatingTools are tools that indicate real progress (write/create/action).
// exec is excluded: ambiguous (could be ls or rm). It neither resets nor
// increments the read-only streak.
var mutatingTools = map[string]bool{
	"write_file": true, "edit": true, "edit_file": true,
	"spawn": true, "team_tasks": true, "message": true,
	"create_image": true, "create_video": true, "create_audio": true,
	"tts": true, "cron": true, "publish_skill": true,
	"sessions_send": true,
}

// toolLoopState tracks recent tool calls within a single agent run
// to detect infinite loops (same tool + same args + same result).
type toolLoopState struct {
	history        []toolCallRecord
	readOnlyStreak int // consecutive non-mutating, non-exec tool calls
}

type toolCallRecord struct {
	toolName   string
	argsHash   string
	resultHash string // empty until result is recorded
}

// record adds a tool call to history and returns its argsHash.
func (s *toolLoopState) record(toolName string, args map[string]any) string {
	h := hashToolCall(toolName, args)
	s.history = append(s.history, toolCallRecord{
		toolName: toolName,
		argsHash: h,
	})
	if len(s.history) > toolLoopHistorySize {
		s.history = s.history[len(s.history)-toolLoopHistorySize:]
	}
	return h
}

// recordResult updates the most recent matching record with the result hash.
func (s *toolLoopState) recordResult(argsHash, resultContent string) {
	rh := hashResult(resultContent)
	// Walk backward to find the latest record with matching argsHash and no result yet.
	for i := len(s.history) - 1; i >= 0; i-- {
		rec := &s.history[i]
		if rec.argsHash == argsHash && rec.resultHash == "" {
			rec.resultHash = rh
			return
		}
	}
}

// detect checks for repeated no-progress tool calls.
// Returns level ("warning", "critical", or "") and a human-readable message.
func (s *toolLoopState) detect(toolName string, argsHash string) (level, message string) {
	if len(s.history) < toolLoopWarningThreshold {
		return "", ""
	}

	// Count records with identical argsHash AND identical non-empty resultHash.
	// This ensures we only flag true no-progress loops (same input → same output).
	var noProgressCount int
	var lastResultHash string

	for i := len(s.history) - 1; i >= 0; i-- {
		rec := s.history[i]
		if rec.argsHash != argsHash {
			continue
		}
		if rec.resultHash == "" {
			continue // incomplete record, skip
		}
		if lastResultHash == "" {
			lastResultHash = rec.resultHash
		}
		if rec.resultHash == lastResultHash {
			noProgressCount++
		}
	}

	if noProgressCount >= toolLoopCriticalThreshold {
		return "critical", fmt.Sprintf(
			"CRITICAL: %s has been called %d times with identical arguments and results. "+
				"Stopping to prevent runaway loop.", toolName, noProgressCount)
	}

	if noProgressCount >= toolLoopWarningThreshold {
		return "warning", fmt.Sprintf(
			"[System: WARNING — %s has been called %d times with the same arguments and identical results. "+
				"This is not making progress. Try a completely different approach, use different tools, "+
				"or respond directly to the user with what you know.]", toolName, noProgressCount)
	}

	return "", ""
}

// recordMutation updates the read-only streak based on tool type.
// Mutating tools reset the streak; exec/bash/mcp are neutral (ambiguous); all others increment.
func (s *toolLoopState) recordMutation(toolName string) {
	if mutatingTools[toolName] {
		s.readOnlyStreak = 0
		return
	}
	// exec/bash: ambiguous (could be ls or rm).
	// mcp_*: user-defined external tools — GoClaw cannot determine read vs write.
	// Neither reset nor increment the read-only streak.
	if toolName == "exec" || toolName == "bash" || strings.HasPrefix(toolName, "mcp_") {
		return
	}
	s.readOnlyStreak++
}

// detectReadOnlyStreak checks for long runs of read-only tool calls
// without any write/edit action. Returns level and message.
func (s *toolLoopState) detectReadOnlyStreak() (level, message string) {
	if s.readOnlyStreak >= readOnlyStreakCritical {
		return "critical", fmt.Sprintf(
			"CRITICAL: %d consecutive read-only tool calls without any write/edit action. "+
				"Stopping to prevent runaway loop.", s.readOnlyStreak)
	}
	if s.readOnlyStreak >= readOnlyStreakWarning {
		return "warning", fmt.Sprintf(
			"[System: WARNING — You have made %d consecutive read-only tool calls (list_files, read_file, find, etc.) "+
				"without writing or editing any file. If you already have the information you need, use the edit or "+
				"write_file tool to take action. If you cannot proceed, respond directly to the user explaining "+
				"what is blocking you.]", s.readOnlyStreak)
	}
	return "", ""
}

// detectSameResult checks if the same tool returned identical results multiple
// times with different arguments. This catches loops where the agent varies
// args slightly but gets no new information.
func (s *toolLoopState) detectSameResult(toolName, resultHash string) (level, message string) {
	if resultHash == "" {
		return "", ""
	}
	var count int
	for _, rec := range s.history {
		if rec.toolName == toolName && rec.resultHash == resultHash {
			count++
		}
	}
	if count >= sameResultCritical {
		return "critical", fmt.Sprintf(
			"CRITICAL: %s returned identical results %d times (with different arguments). "+
				"Stopping to prevent runaway loop.", toolName, count)
	}
	if count >= sameResultWarning {
		return "warning", fmt.Sprintf(
			"[System: WARNING — %s has returned the same result %d times with different arguments. "+
				"The information is already in your context. Stop re-reading and take action — "+
				"use edit/write_file to modify files, or respond to the user if you are stuck.]",
			toolName, count)
	}
	return "", ""
}

// hashToolCall produces a deterministic hash of tool name + arguments.
func hashToolCall(toolName string, args map[string]any) string {
	s := toolName + ":" + stableJSON(args)
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h[:16]) // 32 hex chars, enough for dedup
}

// hashResult produces a hash of tool result content.
func hashResult(content string) string {
	h := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x", h[:16])
}

// stableJSON serializes a value with sorted keys for deterministic hashing.
func stableJSON(v any) string {
	switch val := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = fmt.Sprintf("%q:%s", k, stableJSON(val[k]))
		}
		return "{" + strings.Join(parts, ",") + "}"
	case []any:
		parts := make([]string, len(val))
		for i, elem := range val {
			parts[i] = stableJSON(elem)
		}
		return "[" + strings.Join(parts, ",") + "]"
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}
