package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/safego"
)

// limitHistoryTurns keeps only the last N user turns (and their associated
// assistant/tool messages) from history. A "turn" = one user message plus
// all subsequent non-user messages until the next user message.
// Matching TS src/agents/pi-embedded-runner/history.ts limitHistoryTurns().
func limitHistoryTurns(msgs []providers.Message, limit int) []providers.Message {
	if limit <= 0 || len(msgs) == 0 {
		return msgs
	}

	// Walk backwards counting user messages.
	userCount := 0
	lastUserIndex := len(msgs)

	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			userCount++
			if userCount > limit {
				return msgs[lastUserIndex:]
			}
			lastUserIndex = i
		}
	}

	return msgs
}

// sanitizeHistory repairs tool_use/tool_result pairing in session history.
// Matching TS session-transcript-repair.ts sanitizeToolUseResultPairing().
//
// Problems this fixes:
//   - Orphaned tool messages at start of history (after truncation)
//   - tool_result without matching tool_use in preceding assistant message
//   - assistant with tool_calls but missing tool_results
//   - Duplicate tool call IDs across turns (legacy sessions before uniquifyToolCallIDs)
//
// Returns the cleaned messages and the number of messages that were dropped or synthesized.
func sanitizeHistory(msgs []providers.Message) ([]providers.Message, int) {
	if len(msgs) == 0 {
		return msgs, 0
	}

	dropped := 0

	// 1. Skip leading orphaned tool messages (no preceding assistant with tool_calls).
	start := 0
	for start < len(msgs) && msgs[start].Role == "tool" {
		slog.Debug("sanitizeHistory: dropping orphaned tool message at history start",
			"tool_call_id", msgs[start].ToolCallID)
		dropped++
		start++
	}

	if start >= len(msgs) {
		return nil, dropped
	}

	// 2. Walk through messages ensuring tool_result follows matching tool_use
	// and that roles alternate correctly (user↔assistant).
	// Also dedup tool call IDs across the transcript for legacy sessions that
	// may have persisted duplicates before the live uniquify fix was deployed.
	var result []providers.Message
	globalSeen := make(map[string]bool) // tracks IDs seen across entire transcript

	for i := start; i < len(msgs); i++ {
		msg := msgs[i]

		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			// Deep-copy ToolCalls to avoid mutating the original session history.
			oldCalls := msg.ToolCalls
			msg.ToolCalls = make([]providers.ToolCall, len(oldCalls))
			copy(msg.ToolCalls, oldCalls)

			// Dedup: rewrite any ID that was already seen in an earlier turn or
			// within the same turn. Uses a queue per original ID so multiple tool
			// results with the same raw ID pair correctly in encounter order.
			idQueue := make(map[string][]string, len(msg.ToolCalls)) // origID → []newID
			expectedIDs := make(map[string]bool, len(msg.ToolCalls))
			didDedup := false
			for j := range msg.ToolCalls {
				origID := msg.ToolCalls[j].ID
				newID := origID
				if globalSeen[origID] {
					newID = fmt.Sprintf("%s_dedup_%d", origID, j)
					slog.Debug("sanitizeHistory: dedup tool call ID", "orig", origID, "new", newID)
					didDedup = true
					dropped++ // count as change so cleaned history is persisted back to DB
				}
				msg.ToolCalls[j].ID = newID
				globalSeen[newID] = true
				idQueue[origID] = append(idQueue[origID], newID)
				expectedIDs[newID] = true
			}
			// When dedup rewrites IDs, clear RawAssistantContent so the provider
			// uses the corrected ToolCalls instead of raw JSON with stale IDs.
			if didDedup {
				msg.RawAssistantContent = nil
			}

			result = append(result, msg)

			// Collect matching tool results that follow
			for i+1 < len(msgs) && msgs[i+1].Role == "tool" {
				i++
				toolMsg := msgs[i]
				if queue, ok := idQueue[toolMsg.ToolCallID]; ok && len(queue) > 0 {
					newID := queue[0]
					idQueue[toolMsg.ToolCallID] = queue[1:]
					toolMsg.ToolCallID = newID
					result = append(result, toolMsg)
					delete(expectedIDs, newID)
				} else {
					slog.Debug("sanitizeHistory: dropping mismatched tool result",
						"tool_call_id", toolMsg.ToolCallID)
					dropped++
				}
			}

			// Synthesize missing tool results
			for _, tc := range msg.ToolCalls {
				if expectedIDs[tc.ID] {
					slog.Debug("sanitizeHistory: synthesizing missing tool result", "tool_call_id", tc.ID)
					result = append(result, providers.Message{
						Role:       "tool",
						Content:    "[Tool result missing — session was compacted]",
						ToolCallID: tc.ID,
					})
					dropped++
				}
			}
		} else if msg.Role == "tool" {
			// Orphaned tool message mid-history (no preceding assistant with matching tool_calls)
			slog.Debug("sanitizeHistory: dropping orphaned tool message mid-history",
				"tool_call_id", msg.ToolCallID)
			dropped++
		} else {
			result = append(result, msg)
		}
	}

	// 3. Fix role alternation: LLM APIs require user↔assistant alternation.
	// Merge consecutive same-role messages (e.g. two user messages) into one,
	// which can happen from bootstrap nudges, inject channel, or session corruption.
	if len(result) > 1 {
		merged := make([]providers.Message, 0, len(result))
		merged = append(merged, result[0])
		for j := 1; j < len(result); j++ {
			prev := &merged[len(merged)-1]
			curr := result[j]
			// Only merge plain messages (no tool_calls, no tool role)
			if curr.Role == prev.Role && curr.Role != "tool" && len(curr.ToolCalls) == 0 && len(prev.ToolCalls) == 0 {
				slog.Debug("sanitizeHistory: merging consecutive same-role messages",
					"role", curr.Role, "index", j)
				prev.Content += "\n\n" + curr.Content
				// Preserve media refs from merged message so compaction
				// summary retains knowledge of shared media files.
				if len(curr.MediaRefs) > 0 {
					prev.MediaRefs = append(prev.MediaRefs, curr.MediaRefs...)
				}
				dropped++
			} else {
				merged = append(merged, curr)
			}
		}
		result = merged
	}

	return result, dropped
}

func (l *Loop) maybeSummarize(ctx context.Context, sessionKey string) {
	history := l.sessions.GetHistory(ctx, sessionKey)

	// Use calibrated token estimation, adjusted for overhead.
	// lastPromptTokens includes everything (system prompt, tools, context files, history).
	// We subtract estimated overhead so the threshold comparison is history-only.
	lastPT, lastMC := l.sessions.GetLastPromptTokens(ctx, sessionKey)
	adjustedLastPT := max(lastPT-l.estimateOverhead(history, lastPT, lastMC), 0)
	tokenEstimate := EstimateTokensWithCalibration(history, adjustedLastPT, lastMC)

	// Resolve compaction threshold from config: token-only (no message count guard).
	// Industry standard — Claude Code, Anthropic API, LangChain all use token-based thresholds.
	historyShare := config.DefaultHistoryShare
	if l.compactionCfg != nil && l.compactionCfg.MaxHistoryShare > 0 {
		historyShare = l.compactionCfg.MaxHistoryShare
	}

	threshold := int(float64(l.contextWindow) * historyShare)
	if tokenEstimate <= threshold {
		return
	}

	// Per-session lock: prevent concurrent summarize+flush goroutines for the same session.
	// TryLock is non-blocking — if another run is already summarizing this session, skip.
	// The next run will trigger summarization again if still needed.
	muI, _ := l.summarizeMu.LoadOrStore(sessionKey, &sync.Mutex{})
	sessionMu := muI.(*sync.Mutex)
	if !sessionMu.TryLock() {
		slog.Debug("summarization already in progress, skipping", "session", sessionKey)
		return
	}

	// Memory flush runs synchronously INSIDE the guard
	// (so concurrent runs don't both trigger flush for the same compaction cycle).
	flushSettings := ResolveMemoryFlushSettings(l.compactionCfg)
	if l.shouldRunMemoryFlush(ctx, sessionKey, tokenEstimate, flushSettings) {
		l.runMemoryFlush(ctx, sessionKey, flushSettings)
	}

	// Resolve keepLast before spawning goroutine (reads config under caller's scope).
	keepLast := 4
	if l.compactionCfg != nil && l.compactionCfg.KeepLastMessages > 0 {
		keepLast = l.compactionCfg.KeepLastMessages
	}

	// Summarize in background (holds the per-session lock until done)
	go func() {
		defer sessionMu.Unlock()
		defer safego.Recover(nil, "session", sessionKey)

		// Re-check: history may have been truncated by a concurrent summarize
		// that finished between our threshold check and acquiring the lock.
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 120*time.Second)
		defer cancel()

		history := l.sessions.GetHistory(sctx, sessionKey)
		if len(history) <= keepLast {
			return
		}

		summary := l.sessions.GetSummary(sctx, sessionKey)
		toSummarize := history[:len(history)-keepLast]

		var sb strings.Builder
		var mediaKinds []string
		for _, m := range toSummarize {
			if m.Role == "user" {
				sb.WriteString(fmt.Sprintf("user: %s\n", m.Content))
			} else if m.Role == "assistant" {
				sb.WriteString(fmt.Sprintf("assistant: %s\n", SanitizeAssistantContent(m.Content)))
			}
			for _, ref := range m.MediaRefs {
				mediaKinds = append(mediaKinds, ref.Kind)
			}
		}

		var prompt strings.Builder
		prompt.WriteString(compactionSummaryPrompt)
		if len(mediaKinds) > 0 {
			// Deduplicate and count media types for a compact note.
			counts := make(map[string]int)
			for _, k := range mediaKinds {
				counts[k]++
			}
			prompt.WriteString("Note: user shared media files (")
			first := true
			for k, n := range counts {
				if !first {
					prompt.WriteString(", ")
				}
				prompt.WriteString(fmt.Sprintf("%d %s(s)", n, k))
				first = false
			}
			prompt.WriteString(") which are no longer in context. Mention briefly if relevant.\n\n")
		}
		if summary != "" {
			prompt.WriteString("Existing context: " + summary + "\n\n")
		}
		prompt.WriteString(sb.String())

		resp, err := l.provider.Chat(sctx, providers.ChatRequest{
			Messages: []providers.Message{{Role: "user", Content: prompt.String()}},
			Model:    l.model,
			Options:  map[string]any{"max_tokens": 1024, "temperature": 0.3},
		})
		if err != nil {
			slog.Warn("summarization failed", "session", sessionKey, "error", err)
			return
		}

		l.sessions.SetSummary(sctx, sessionKey, SanitizeAssistantContent(resp.Content))
		l.sessions.TruncateHistory(sctx, sessionKey, keepLast)
		l.sessions.IncrementCompaction(sctx, sessionKey)
		l.sessions.Save(sctx, sessionKey)
	}()
}

// estimateOverhead derives the non-history token overhead (system prompt + tool definitions +
// context files) from calibration data. Used by maybeSummarize to compare history-only tokens
// against the compaction threshold.
func (l *Loop) estimateOverhead(history []providers.Message, lastPromptTokens, lastMsgCount int) int {
	if lastPromptTokens <= 0 || lastMsgCount <= 0 {
		// No calibration data — use conservative default (20% of context, capped at 40k).
		fallback := min(int(float64(l.contextWindow)*0.2), 40000)
		return fallback
	}

	// Overhead = total prompt tokens - estimated history tokens at calibration time.
	count := min(lastMsgCount, len(history))
	historyEstAtCalibration := EstimateHistoryTokens(history[:count])
	overhead := max(lastPromptTokens-historyEstAtCalibration, 0)
	// Clamp: overhead shouldn't exceed 40% of context window.
	maxOverhead := int(float64(l.contextWindow) * 0.4)
	if overhead > maxOverhead {
		overhead = maxOverhead
	}
	return overhead
}
