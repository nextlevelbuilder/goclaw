package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// Default memory flush prompts matching TS memory-flush.ts.
const (
	DefaultMemoryFlushPrompt = "Pre-compaction memory flush. " +
		"Store durable memories now (use memory/YYYY-MM-DD.md; create memory/ if needed). " +
		"IMPORTANT: If the file already exists, APPEND new content only and do not overwrite existing entries. " +
		"If nothing to store, reply with NO_REPLY."

	DefaultMemoryFlushSystemPrompt = "Pre-compaction memory flush turn. " +
		"The session is near auto-compaction; capture durable memories to disk. " +
		"You may reply, but usually NO_REPLY is correct."

	DefaultSoftThresholdTokens = 4000
)

// MemoryFlushSettings holds resolved flush config with defaults applied.
type MemoryFlushSettings struct {
	Enabled             bool
	SoftThresholdTokens int
	Prompt              string
	SystemPrompt        string
}

// ResolveMemoryFlushSettings resolves flush settings from config, applying defaults.
// Returns nil if disabled.
func ResolveMemoryFlushSettings(compaction *config.CompactionConfig) *MemoryFlushSettings {
	if compaction == nil || compaction.MemoryFlush == nil {
		// Default: enabled
		return &MemoryFlushSettings{
			Enabled:             true,
			SoftThresholdTokens: DefaultSoftThresholdTokens,
			Prompt:              DefaultMemoryFlushPrompt,
			SystemPrompt:        DefaultMemoryFlushSystemPrompt,
		}
	}

	mf := compaction.MemoryFlush
	if mf.Enabled != nil && !*mf.Enabled {
		return nil
	}

	settings := &MemoryFlushSettings{
		Enabled:             true,
		SoftThresholdTokens: DefaultSoftThresholdTokens,
		Prompt:              DefaultMemoryFlushPrompt,
		SystemPrompt:        DefaultMemoryFlushSystemPrompt,
	}

	if mf.SoftThresholdTokens > 0 {
		settings.SoftThresholdTokens = mf.SoftThresholdTokens
	}
	if mf.Prompt != "" {
		settings.Prompt = mf.Prompt
	}
	if mf.SystemPrompt != "" {
		settings.SystemPrompt = mf.SystemPrompt
	}

	return settings
}

// shouldRunMemoryFlush checks whether a memory flush should run before compaction.
// Matching TS memory-flush.ts:shouldRunMemoryFlush.
func (l *Loop) shouldRunMemoryFlush(sessionKey string, totalTokens int, settings *MemoryFlushSettings) bool {
	if settings == nil || !settings.Enabled || !l.hasMemory {
		return false
	}

	if totalTokens <= 0 {
		return false
	}

	// Threshold = contextWindow - reserveTokensFloor - softThresholdTokens
	// When totalTokens >= threshold → flush
	reserveFloor := 20000 // default
	if l.compactionCfg != nil && l.compactionCfg.ReserveTokensFloor > 0 {
		reserveFloor = l.compactionCfg.ReserveTokensFloor
	}

	threshold := l.contextWindow - reserveFloor - settings.SoftThresholdTokens
	if threshold <= 0 {
		return false
	}

	if totalTokens < threshold {
		return false
	}

	// Deduplication: skip if already flushed in this compaction cycle
	compactionCount := l.sessions.GetCompactionCount(sessionKey)
	lastFlushAt := l.sessions.GetMemoryFlushCompactionCount(sessionKey)
	if lastFlushAt >= 0 && lastFlushAt == compactionCount {
		return false
	}

	return true
}

// runMemoryFlush executes a memory flush turn: sends flush prompt to LLM with tools
// so it can write memory files. Matching TS agent-runner-memory.ts.
func (l *Loop) runMemoryFlush(ctx context.Context, sessionKey string, settings *MemoryFlushSettings) {
	slog.Info("memory flush: starting", "session", sessionKey)

	flushCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	// Build messages: system prompt + history summary + flush prompt
	history := l.sessions.GetHistory(sessionKey)
	summary := l.sessions.GetSummary(sessionKey)

	var messages []providers.Message

	// System prompt: combine agent's normal system prompt context with flush system prompt
	systemPrompt := BuildSystemPrompt(SystemPromptConfig{
		AgentID:       l.id,
		Model:         l.model,
		Workspace:     l.workspace,
		Mode:          PromptMinimal,
		ToolNames:     l.tools.List(),
		HasMemory:     l.hasMemory,
		IdentityName:  l.identityName,
		IdentityEmoji: l.identityEmoji,
	})
	systemPrompt += "\n\n" + settings.SystemPrompt

	messages = append(messages, providers.Message{
		Role:    "system",
		Content: systemPrompt,
	})

	// Include conversation summary for context
	if summary != "" {
		messages = append(messages, providers.Message{
			Role:    "user",
			Content: fmt.Sprintf("[Previous conversation summary]\n%s", summary),
		})
		messages = append(messages, providers.Message{
			Role:    "assistant",
			Content: "Understood.",
		})
	}

	// Include recent history (last 10 messages for context)
	recentHistory := history
	if len(recentHistory) > 10 {
		recentHistory = recentHistory[len(recentHistory)-10:]
	}
	messages = append(messages, sanitizeHistory(recentHistory)...)

	// Flush prompt
	messages = append(messages, providers.Message{
		Role:    "user",
		Content: settings.Prompt,
	})

	// Build tool list — only file tools needed for memory flush
	var toolDefs []providers.ToolDefinition
	if l.toolPolicy != nil {
		toolDefs = l.toolPolicy.FilterTools(l.tools, l.id, l.provider.Name(), nil, nil, false, false)
	} else {
		toolDefs = l.tools.ProviderDefs()
	}

	// Run LLM iteration loop (max 5 iterations for flush)
	maxFlushIter := 5
	for i := 0; i < maxFlushIter; i++ {
		resp, err := l.provider.Chat(flushCtx, providers.ChatRequest{
			Messages: messages,
			Tools:    toolDefs,
			Model:    l.model,
			Options: map[string]interface{}{
				"max_tokens":  4096,
				"temperature": 0.3,
			},
		})
		if err != nil {
			slog.Warn("memory flush: LLM call failed", "error", err)
			break
		}

		// No tool calls → done
		if len(resp.ToolCalls) == 0 {
			content := SanitizeAssistantContent(resp.Content)
			if IsSilentReply(content) {
				slog.Info("memory flush: NO_REPLY (nothing to save)")
			} else if content != "" {
				slog.Info("memory flush: completed with response", "content_len", len(content))
			}
			break
		}

		// Process tool calls
		assistantMsg := providers.Message{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		}
		messages = append(messages, assistantMsg)

		for _, tc := range resp.ToolCalls {
			argsJSON, _ := json.Marshal(tc.Arguments)
			slog.Info("memory flush: tool call", "tool", tc.Name, "args_len", len(argsJSON))

			result := l.tools.ExecuteWithContext(flushCtx, tc.Name, tc.Arguments, "", "", "", sessionKey, nil)

			messages = append(messages, providers.Message{
				Role:       "tool",
				Content:    result.ForLLM,
				ToolCallID: tc.ID,
			})
		}
	}

	// Mark flush as done
	l.sessions.SetMemoryFlushDone(sessionKey)
	l.sessions.Save(sessionKey)

	slog.Info("memory flush: completed", "session", sessionKey)
}
