package agent

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// InjectedMessage represents a user message injected into a running agent loop
// at the turn boundary (after tool results, before next LLM call).
type InjectedMessage struct {
	Content string
	UserID  string
}

// processedInjection holds the two message forms: one for the LLM (with context wrapper)
// and one for session persistence (original content).
type processedInjection struct {
	forLLM     providers.Message // wrapped with "[User sent a follow-up...]" prefix
	forSession providers.Message // original content for session history
}

// injectBufferSize is the capacity of the per-run injection channel.
const injectBufferSize = 5

// processInjectedMessage validates and wraps an injected message for the LLM.
// Returns nil, false if the message should be skipped (blocked by input guard).
func (l *Loop) processInjectedMessage(injected InjectedMessage, emitRun func(AgentEvent)) (*processedInjection, bool) {
	// Security: scan injected content with input guard
	if l.inputGuard != nil {
		if matches := l.inputGuard.Scan(injected.Content); len(matches) > 0 {
			matchStr := strings.Join(matches, ",")
			if l.injectionAction == "block" {
				slog.Warn("security.injection_blocked_midrun",
					"agent", l.id, "user", injected.UserID,
					"patterns", matchStr)
				return nil, false
			}
			slog.Warn("security.injection_detected_midrun",
				"agent", l.id, "user", injected.UserID,
				"patterns", matchStr)
		}
	}

	// Truncate oversized content
	content := injected.Content
	maxChars := l.maxMessageChars
	if maxChars <= 0 {
		maxChars = config.DefaultMaxMessageChars
	}
	if len(content) > maxChars {
		content = content[:maxChars] + "\n[Message truncated]"
	}

	// Wrap with context hint so LLM knows this is a mid-run follow-up
	wrapped := fmt.Sprintf("[User sent a follow-up message while you were working]\n%s", content)

	slog.Info("mid-run injection",
		"agent", l.id, "user", injected.UserID,
		"msg_len", len(content))

	// Emit activity event so UI/channels know about the injection
	if emitRun != nil {
		emitRun(AgentEvent{
			Type:    protocol.AgentEventActivity,
			AgentID: l.id,
			Payload: map[string]any{
				"phase":   "injected_message",
				"content": truncateForLog(content, 200),
			},
		})
	}

	return &processedInjection{
		forLLM:     providers.Message{Role: "user", Content: wrapped},
		forSession: providers.Message{Role: "user", Content: content},
	}, true
}

// drainInjectChannel reads all available messages from the injection channel
// without blocking. Returns processed messages ready to append to the loop.
func (l *Loop) drainInjectChannel(ch <-chan InjectedMessage, emitRun func(AgentEvent)) (forLLM, forSession []providers.Message) {
	if ch == nil {
		return nil, nil
	}
	for {
		select {
		case injected := <-ch:
			if result, ok := l.processInjectedMessage(injected, emitRun); ok {
				forLLM = append(forLLM, result.forLLM)
				forSession = append(forSession, result.forSession)
			}
		default:
			return forLLM, forSession
		}
	}
}

// truncateForLog truncates a string for log/event payloads.
func truncateForLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
