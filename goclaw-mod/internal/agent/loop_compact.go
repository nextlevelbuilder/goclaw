package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// compactionSummaryPrompt is the structured summarization instruction used by both
// mid-loop compaction and background summarization. Matching OpenClaw TS compaction.ts
// MERGE_SUMMARIES_INSTRUCTIONS + IDENTIFIER_PRESERVATION_INSTRUCTIONS.
const compactionSummaryPrompt = `Summarize this conversation concisely for the AI agent to resume work.

MUST PRESERVE:
- Active tasks and their current status (in-progress, blocked, pending)
- Pending subagent tasks (IDs, labels, statuses) — agent needs to know what is still running
- Pending team task results awaiting delivery (task IDs, assignees, statuses)
- Any "waiting for..." state — do NOT drop expectations of future results
- Batch operation progress (e.g., "5/17 items completed")
- The last thing the user requested and what was being done about it
- Decisions made and their rationale
- TODOs, open questions, and constraints
- Any commitments or follow-ups promised

IDENTIFIER PRESERVATION:
Preserve all opaque identifiers exactly as written (no shortening or reconstruction),
including UUIDs, hashes, IDs, tokens, API keys, hostnames, IPs, ports, URLs, and file names.

PRIORITIZE recent context over older history. The agent needs to know
what it was doing, not just what was discussed.

Conversation to summarize:

`

const defaultCompactionTimeout = 120 * time.Second

const (
	maxCompactionChunks      = 16
	maxCompactionMergeLevels = 3
	defaultCompactionShare   = 0.85
)

func (l *Loop) compactionTimeout() time.Duration {
	if l.compactionCfg != nil && l.compactionCfg.TimeoutSeconds > 0 {
		return time.Duration(l.compactionCfg.TimeoutSeconds) * time.Second
	}
	return defaultCompactionTimeout
}

// compactMessagesInPlace summarizes the first ~70% of messages into a condensed
// summary, keeping the last ~30% intact. Operates purely on the local messages
// slice — no session state touched, no locks needed.
// Returns nil on failure (caller keeps original messages).
func (l *Loop) compactMessagesInPlace(ctx context.Context, messages []providers.Message) []providers.Message {
	if len(messages) < 6 {
		return nil
	}

	// Resolve keepCount from compaction config (same defaults as maybeSummarize).
	keepCount := 4
	if l.compactionCfg != nil && l.compactionCfg.KeepLastMessages > 0 {
		keepCount = l.compactionCfg.KeepLastMessages
	}
	// Ensure we keep at least 30% of messages.
	if minKeep := len(messages) * 3 / 10; minKeep > keepCount {
		keepCount = minKeep
	}

	splitIdx := len(messages) - keepCount

	// Walk backward from splitIdx to find a clean boundary —
	// avoid splitting tool_use → tool_result pairs.
	for splitIdx > 0 {
		m := messages[splitIdx]
		if m.Role == "tool" || (m.Role == "assistant" && len(m.ToolCalls) > 0) {
			splitIdx--
			continue
		}
		break
	}
	if splitIdx <= 1 {
		return nil
	}

	toSummarize := messages[:splitIdx]
	timeout := l.compactionTimeout()
	sctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	inputCap := l.compactionInputCap()
	if inputCap <= 0 {
		slog.Warn("mid_loop_compaction_failed", "agent", l.id, "error", "context_window_unresolved")
		return nil
	}
	units := buildCompactionUnits(toSummarize)
	summaryContent, chunkCount, err := l.summarizeCompactionUnits(sctx, units, inputCap, 1)
	if err != nil {
		slog.Warn("mid_loop_compaction_failed", "agent", l.id, "timeout_seconds", int(timeout/time.Second), "error", err)
		return nil
	}
	slog.Info("compact_budget",
		"path", "mid-loop",
		"agent", l.id,
		"in_tokens", l.estimateSummaryInputTokens(toSummarize),
		"input_cap_tokens", inputCap,
		"chunks", chunkCount,
		"timeout_seconds", int(timeout/time.Second),
	)

	// Collect MediaRefs from compacted messages (keep up to 30 most recent).
	const maxPreservedMediaRefs = 30
	var preservedRefs []providers.MediaRef
	for i := len(toSummarize) - 1; i >= 0 && len(preservedRefs) < maxPreservedMediaRefs; i-- {
		for _, ref := range toSummarize[i].MediaRefs {
			preservedRefs = append(preservedRefs, ref)
			if len(preservedRefs) >= maxPreservedMediaRefs {
				break
			}
		}
	}

	summary := providers.Message{
		Role:      "user",
		Content:   "[Summary of earlier conversation]\n" + summaryContent,
		MediaRefs: preservedRefs,
	}
	result := make([]providers.Message, 0, 1+keepCount)
	result = append(result, summary)
	result = append(result, messages[splitIdx:]...)

	slog.Info("mid_loop_compacted",
		"agent", l.id,
		"original_msgs", len(messages),
		"summarized", splitIdx,
		"kept", len(result))

	return result
}

func (l *Loop) compactionInputCap() int {
	contextWindow := l.resolveEffectiveContextWindow()
	if contextWindow <= 0 {
		return 0
	}
	maxTokens := l.effectiveMaxTokens()
	hardInputCap := contextWindow - maxTokens
	share := defaultCompactionShare
	if l.compactionCfg != nil && l.compactionCfg.MaxRequestShare > 0 && l.compactionCfg.MaxRequestShare <= 1 {
		share = l.compactionCfg.MaxRequestShare
	}
	softTarget := int(float64(contextWindow)*share) - maxTokens
	return min(hardInputCap, softTarget)
}

func buildCompactionUnits(messages []providers.Message) []string {
	units := make([]string, 0, len(messages))
	for i := 0; i < len(messages); {
		end := i + 1
		if messages[i].Role == "assistant" && len(messages[i].ToolCalls) > 0 {
			for end < len(messages) && messages[end].Role == "tool" {
				end++
			}
		}
		if text := renderCompactionMessages(messages[i:end]); text != "" {
			units = append(units, text)
		}
		i = end
	}
	return units
}

func renderCompactionMessages(messages []providers.Message) string {
	var sb strings.Builder
	for _, m := range messages {
		switch m.Role {
		case "user":
			fmt.Fprintf(&sb, "user: %s\n", m.Content)
		case "assistant":
			if content := SanitizeAssistantContent(m.Content); content != "" {
				fmt.Fprintf(&sb, "assistant: %s\n", content)
			}
			// Tool calls carry the assistant's intent (which tool, what args);
			// dropping them loses the "why" behind each tool result below.
			for _, tc := range m.ToolCalls {
				if args, err := json.Marshal(tc.Arguments); err == nil && len(tc.Arguments) > 0 {
					fmt.Fprintf(&sb, "assistant tool call %s(%s)\n", tc.Name, string(args))
				} else {
					fmt.Fprintf(&sb, "assistant tool call %s()\n", tc.Name)
				}
			}
		case "tool":
			// Tool results hold the technical payload the summary must retain
			// (search hits, file contents, API responses). buildCompactionUnits
			// groups these with their assistant tool_call; the previous renderer
			// silently dropped them, erasing the data before summarization.
			if m.Content != "" {
				fmt.Fprintf(&sb, "tool result: %s\n", m.Content)
			}
		}
	}
	return sb.String()
}

func (l *Loop) summarizeCompactionUnits(ctx context.Context, units []string, inputCap, level int) (string, int, error) {
	if len(units) == 0 {
		return "", 0, fmt.Errorf("no compactable conversation content")
	}
	chunks, err := l.packCompactionChunks(units, inputCap)
	if err != nil {
		return "", 0, err
	}
	if len(chunks) > maxCompactionChunks {
		return "", 0, fmt.Errorf("compaction chunk limit exceeded: chunks=%d limit=%d", len(chunks), maxCompactionChunks)
	}

	summaries := make([]string, 0, len(chunks))
	for i, chunk := range chunks {
		inputTokens := l.estimateCompactionRequestTokens(chunk)
		if inputTokens > inputCap {
			return "", 0, fmt.Errorf("compaction chunk exceeds input cap: chunk=%d input=%d cap=%d", i, inputTokens, inputCap)
		}
		outputTokens := dynamicSummaryMax(inputTokens)
		slog.Debug("compact_chunk_budget",
			"path", "mid-loop",
			"agent", l.id,
			"level", level,
			"chunk", i+1,
			"chunks", len(chunks),
			"in_tokens", inputTokens,
			"out_tokens", outputTokens,
			"input_cap_tokens", inputCap,
		)
		resp, callErr := l.callInternalLLMWithUsage(ctx, providers.ChatRequest{
			Messages: []providers.Message{{Role: "user", Content: compactionSummaryPrompt + chunk}},
			Model:    l.model,
			Options:  map[string]any{"max_tokens": outputTokens, "temperature": 0.3},
		}, "mid-loop-compaction")
		if callErr != nil {
			return "", 0, callErr
		}
		summary := SanitizeAssistantContent(resp.Content)
		if strings.TrimSpace(summary) == "" {
			return "", 0, fmt.Errorf("compaction returned empty summary")
		}
		summaries = append(summaries, summary)
	}
	if len(summaries) == 1 {
		return summaries[0], len(chunks), nil
	}
	if level >= maxCompactionMergeLevels {
		return "", 0, fmt.Errorf("compaction merge level exceeded: level=%d limit=%d", level, maxCompactionMergeLevels)
	}

	mergeUnits := make([]string, len(summaries))
	for i, summary := range summaries {
		mergeUnits[i] = fmt.Sprintf("partial summary %d: %s\n", i+1, summary)
	}
	merged, mergeChunks, mergeErr := l.summarizeCompactionUnits(ctx, mergeUnits, inputCap, level+1)
	return merged, len(chunks) + mergeChunks, mergeErr
}

func (l *Loop) packCompactionChunks(units []string, inputCap int) ([]string, error) {
	var chunks []string
	var current strings.Builder
	flush := func() {
		if current.Len() == 0 {
			return
		}
		chunks = append(chunks, current.String())
		current.Reset()
	}

	for _, unit := range units {
		parts, err := l.splitCompactionUnit(unit, inputCap)
		if err != nil {
			return nil, err
		}
		for _, part := range parts {
			candidate := current.String() + part
			if current.Len() > 0 && l.estimateCompactionRequestTokens(candidate) > inputCap {
				flush()
				candidate = part
			}
			if l.estimateCompactionRequestTokens(candidate) > inputCap {
				return nil, fmt.Errorf("atomic compaction unit exceeds input cap")
			}
			current.WriteString(part)
			if len(chunks) >= maxCompactionChunks {
				return nil, fmt.Errorf("compaction chunk limit exceeded: limit=%d", maxCompactionChunks)
			}
		}
	}
	flush()
	return chunks, nil
}

func (l *Loop) splitCompactionUnit(unit string, inputCap int) ([]string, error) {
	if l.estimateCompactionRequestTokens(unit) <= inputCap {
		return []string{unit}, nil
	}
	words := strings.Fields(unit)
	if len(words) == 0 {
		return nil, fmt.Errorf("empty oversized compaction unit")
	}

	var parts []string
	var current strings.Builder
	for _, word := range words {
		candidate := word
		if current.Len() > 0 {
			candidate = current.String() + " " + word
		}
		if l.estimateCompactionRequestTokens(candidate) <= inputCap {
			if current.Len() > 0 {
				current.WriteByte(' ')
			}
			current.WriteString(word)
			continue
		}
		if current.Len() == 0 {
			return nil, fmt.Errorf("atomic compaction token exceeds input cap")
		}
		parts = append(parts, current.String()+"\n")
		current.Reset()
		current.WriteString("[continued] ")
		current.WriteString(word)
		if l.estimateCompactionRequestTokens(current.String()) > inputCap {
			return nil, fmt.Errorf("atomic compaction token exceeds input cap")
		}
	}
	if current.Len() > 0 {
		parts = append(parts, current.String()+"\n")
	}
	return parts, nil
}

func (l *Loop) estimateCompactionRequestTokens(content string) int {
	message := providers.Message{Role: "user", Content: compactionSummaryPrompt + content}
	if l.tokenCounter != nil {
		return l.tokenCounter.CountMessages(l.model, []providers.Message{message})
	}
	return len([]rune(message.Content))/3 + 4
}

// dynamicSummaryMax returns the output-token budget for a compaction or
// summarization call, scaled to input size. Formula: in/25 (~4% compression),
// clamped to [1024, 8192]. Floor keeps short summaries coherent; cap prevents
// runaway output billing on pathological inputs.
func dynamicSummaryMax(inputTokens int) int {
	out := min(max(inputTokens/25, 1024), 8192)
	return out
}

// estimateSummaryInputTokens returns a best-effort input-token count. Prefers
// TokenCounter when attached; else rune/3 fallback (~±15% for UTF-8).
func (l *Loop) estimateSummaryInputTokens(messages []providers.Message) int {
	if l.tokenCounter != nil {
		return l.tokenCounter.CountMessages(l.model, messages)
	}
	total := 0
	for _, m := range messages {
		total += len([]rune(m.Content)) / 3
	}
	return total
}
