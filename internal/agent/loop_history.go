package agent

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// buildMessages constructs the full message list for an LLM request.
// Returns the messages and whether BOOTSTRAP.md was present in context files
// (used by the caller for auto-cleanup without an extra DB roundtrip).
func (l *Loop) buildMessages(ctx context.Context, history []providers.Message, summary, userMessage, extraSystemPrompt, sessionKey, channel, userID string, historyLimit int) ([]providers.Message, bool) {
	var messages []providers.Message

	// Build full system prompt using the new builder (matching TS buildAgentSystemPrompt)
	mode := PromptFull
	if bootstrap.IsSubagentSession(sessionKey) || bootstrap.IsCronSession(sessionKey) {
		mode = PromptMinimal
	}

	_, hasSpawn := l.tools.Get("spawn")
	_, hasSkillSearch := l.tools.Get("skill_search")

	// Per-user workspace: show the user's subdirectory in the system prompt (managed mode)
	promptWorkspace := l.workspace
	if l.agentUUID != uuid.Nil && userID != "" && l.workspace != "" {
		promptWorkspace = filepath.Join(l.workspace, sanitizePathSegment(userID))
	}

	// Resolve context files once — also detect BOOTSTRAP.md presence.
	contextFiles := l.resolveContextFiles(ctx, userID)
	hadBootstrap := false
	for _, cf := range contextFiles {
		if cf.Path == bootstrap.BootstrapFile {
			hadBootstrap = true
			break
		}
	}

	systemPrompt := BuildSystemPrompt(SystemPromptConfig{
		AgentID:        l.id,
		Model:          l.model,
		Workspace:      promptWorkspace,
		Channel:        channel,
		OwnerIDs:       l.ownerIDs,
		Mode:           mode,
		ToolNames:      l.tools.List(),
		SkillsSummary:  l.resolveSkillsSummary(),
		HasMemory:      l.hasMemory,
		HasSpawn:       l.tools != nil && hasSpawn,
		HasSkillSearch: hasSkillSearch,
		ContextFiles:   contextFiles,
		ExtraPrompt:    extraSystemPrompt,
		IdentityName:   l.identityName,
		IdentityEmoji:  l.identityEmoji,
		SandboxEnabled:        l.sandboxEnabled,
		SandboxContainerDir:   l.sandboxContainerDir,
		SandboxWorkspaceAccess: l.sandboxWorkspaceAccess,
	})

	messages = append(messages, providers.Message{
		Role:    "system",
		Content: systemPrompt,
	})

	// Summary context
	if summary != "" {
		messages = append(messages, providers.Message{
			Role:    "user",
			Content: fmt.Sprintf("[Previous conversation summary]\n%s", summary),
		})
		messages = append(messages, providers.Message{
			Role:    "assistant",
			Content: "I understand the context from our previous conversation. How can I help you?",
		})
	}

	// History pipeline matching TS: limitHistoryTurns → pruneContext → sanitizeHistory.
	trimmed := limitHistoryTurns(history, historyLimit)
	pruned := pruneContextMessages(trimmed, l.contextWindow, l.contextPruningCfg)
	messages = append(messages, sanitizeHistory(pruned)...)

	// Current user message
	messages = append(messages, providers.Message{
		Role:    "user",
		Content: userMessage,
	})

	return messages, hadBootstrap
}

// resolveContextFiles merges base context files (from resolver, e.g. auto-generated
// delegation targets) with per-user files. Per-user files override same-name base files,
// but base-only files (like auto-injected delegation info) are preserved.
func (l *Loop) resolveContextFiles(ctx context.Context, userID string) []bootstrap.ContextFile {
	if l.contextFileLoader == nil || userID == "" {
		return l.contextFiles
	}
	userFiles := l.contextFileLoader(ctx, l.agentUUID, userID, l.agentType)
	if len(userFiles) == 0 {
		return l.contextFiles
	}
	if len(l.contextFiles) == 0 {
		return userFiles
	}

	// Merge: start with per-user files, then append base-only files
	userSet := make(map[string]struct{}, len(userFiles))
	for _, f := range userFiles {
		userSet[f.Path] = struct{}{}
	}
	merged := make([]bootstrap.ContextFile, len(userFiles))
	copy(merged, userFiles)
	for _, base := range l.contextFiles {
		if _, exists := userSet[base.Path]; !exists {
			merged = append(merged, base)
		}
	}
	return merged
}

// Hybrid skill thresholds: when skill count and total token estimate are below
// these limits, inline all skills as XML in the system prompt (like TS).
// Above these limits, only include skill_search instructions.
const (
	skillInlineMaxCount  = 20   // max skills to inline
	skillInlineMaxTokens = 3500 // max estimated tokens for skill descriptions
)

// resolveSkillsSummary dynamically builds the skills summary for the system prompt.
// Called per-message so it picks up hot-reloaded skills automatically.
// Returns (summary XML, useInline) — useInline=true means skills are inlined and
// the system prompt should use TS-style "scan <available_skills>" instructions
// instead of "use skill_search".
func (l *Loop) resolveSkillsSummary() string {
	if l.skillsLoader == nil {
		return ""
	}

	filtered := l.skillsLoader.FilterSkills(l.skillAllowList)
	if len(filtered) == 0 {
		return ""
	}

	// Estimate tokens: ~1 token per 4 chars for name+description
	totalChars := 0
	for _, s := range filtered {
		totalChars += len(s.Name) + len(s.Description) + 10 // +10 for XML tags overhead
	}
	estimatedTokens := totalChars / 4

	if len(filtered) <= skillInlineMaxCount && estimatedTokens <= skillInlineMaxTokens {
		// Inline mode: build full XML summary
		return l.skillsLoader.BuildSummary(l.skillAllowList)
	}

	// Search mode: no XML in prompt, agent uses skill_search tool
	return ""
}

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
func sanitizeHistory(msgs []providers.Message) []providers.Message {
	if len(msgs) == 0 {
		return msgs
	}

	// 1. Skip leading orphaned tool messages (no preceding assistant with tool_calls).
	start := 0
	for start < len(msgs) && msgs[start].Role == "tool" {
		slog.Warn("dropping orphaned tool message at history start",
			"tool_call_id", msgs[start].ToolCallID)
		start++
	}

	if start >= len(msgs) {
		return nil
	}

	// 2. Walk through messages ensuring tool_result follows matching tool_use.
	var result []providers.Message
	for i := start; i < len(msgs); i++ {
		msg := msgs[i]

		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			// Collect expected tool call IDs
			expectedIDs := make(map[string]bool, len(msg.ToolCalls))
			for _, tc := range msg.ToolCalls {
				expectedIDs[tc.ID] = true
			}

			result = append(result, msg)

			// Collect matching tool results that follow
			for i+1 < len(msgs) && msgs[i+1].Role == "tool" {
				i++
				toolMsg := msgs[i]
				if expectedIDs[toolMsg.ToolCallID] {
					result = append(result, toolMsg)
					delete(expectedIDs, toolMsg.ToolCallID)
				} else {
					slog.Warn("dropping mismatched tool result",
						"tool_call_id", toolMsg.ToolCallID)
				}
			}

			// Synthesize missing tool results
			for id := range expectedIDs {
				slog.Warn("synthesizing missing tool result", "tool_call_id", id)
				result = append(result, providers.Message{
					Role:       "tool",
					Content:    "[Tool result missing — session was compacted]",
					ToolCallID: id,
				})
			}
		} else if msg.Role == "tool" {
			// Orphaned tool message mid-history (no preceding assistant with matching tool_calls)
			slog.Warn("dropping orphaned tool message mid-history",
				"tool_call_id", msg.ToolCallID)
		} else {
			result = append(result, msg)
		}
	}

	return result
}

func (l *Loop) maybeSummarize(ctx context.Context, sessionKey string) {
	history := l.sessions.GetHistory(sessionKey)

	// Use calibrated token estimation when available.
	lastPT, lastMC := l.sessions.GetLastPromptTokens(sessionKey)
	tokenEstimate := EstimateTokensWithCalibration(history, lastPT, lastMC)

	// Resolve compaction thresholds from config with sensible defaults.
	historyShare := 0.75
	if l.compactionCfg != nil && l.compactionCfg.MaxHistoryShare > 0 {
		historyShare = l.compactionCfg.MaxHistoryShare
	}
	minMessages := 50
	if l.compactionCfg != nil && l.compactionCfg.MinMessages > 0 {
		minMessages = l.compactionCfg.MinMessages
	}

	threshold := int(float64(l.contextWindow) * historyShare)
	if len(history) <= minMessages && tokenEstimate <= threshold {
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
	if l.shouldRunMemoryFlush(sessionKey, tokenEstimate, flushSettings) {
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

		// Re-check: history may have been truncated by a concurrent summarize
		// that finished between our threshold check and acquiring the lock.
		history := l.sessions.GetHistory(sessionKey)
		if len(history) <= keepLast {
			return
		}

		sctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()

		summary := l.sessions.GetSummary(sessionKey)
		toSummarize := history[:len(history)-keepLast]

		var sb string
		for _, m := range toSummarize {
			if m.Role == "user" {
				sb += fmt.Sprintf("user: %s\n", m.Content)
			} else if m.Role == "assistant" {
				sb += fmt.Sprintf("assistant: %s\n", SanitizeAssistantContent(m.Content))
			}
		}

		prompt := "Provide a concise summary of this conversation, preserving key context:\n"
		if summary != "" {
			prompt += "Existing context: " + summary + "\n"
		}
		prompt += "\n" + sb

		resp, err := l.provider.Chat(sctx, providers.ChatRequest{
			Messages: []providers.Message{{Role: "user", Content: prompt}},
			Model:    l.model,
			Options:  map[string]interface{}{"max_tokens": 1024, "temperature": 0.3},
		})
		if err != nil {
			slog.Warn("summarization failed", "session", sessionKey, "error", err)
			return
		}

		l.sessions.SetSummary(sessionKey, SanitizeAssistantContent(resp.Content))
		l.sessions.TruncateHistory(sessionKey, keepLast)
		l.sessions.IncrementCompaction(sessionKey)
		l.sessions.Save(sessionKey)
	}()
}
