package agent

import (
	"fmt"
	"unicode/utf8"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// Context pruning defaults matching TS DEFAULT_CONTEXT_PRUNING_SETTINGS.
const (
	defaultKeepLastAssistants   = 3
	defaultSoftTrimRatio        = 0.25
	defaultHardClearRatio       = 0.5
	defaultMinPrunableToolChars = 50000
	defaultSoftTrimMaxChars     = 3000
	defaultSoftTrimHeadChars    = 1500
	defaultSoftTrimTailChars    = 1500
	defaultHardClearPlaceholder = "[Old tool result content cleared]"
	charsPerTokenEstimate       = 4
)

// effectivePruningSettings holds resolved pruning settings with defaults applied.
type effectivePruningSettings struct {
	keepLastAssistants   int
	softTrimRatio        float64
	hardClearRatio       float64
	minPrunableToolChars int
	softTrimMaxChars     int
	softTrimHeadChars    int
	softTrimTailChars    int
	hardClearEnabled     bool
	hardClearPlaceholder string
}

// resolvePruningSettings applies defaults to user config.
func resolvePruningSettings(cfg *config.ContextPruningConfig) *effectivePruningSettings {
	s := &effectivePruningSettings{
		keepLastAssistants:   defaultKeepLastAssistants,
		softTrimRatio:        defaultSoftTrimRatio,
		hardClearRatio:       defaultHardClearRatio,
		minPrunableToolChars: defaultMinPrunableToolChars,
		softTrimMaxChars:     defaultSoftTrimMaxChars,
		softTrimHeadChars:    defaultSoftTrimHeadChars,
		softTrimTailChars:    defaultSoftTrimTailChars,
		hardClearEnabled:     true,
		hardClearPlaceholder: defaultHardClearPlaceholder,
	}

	if cfg == nil {
		return s
	}

	if cfg.KeepLastAssistants > 0 {
		s.keepLastAssistants = cfg.KeepLastAssistants
	}
	if cfg.SoftTrimRatio > 0 && cfg.SoftTrimRatio <= 1 {
		s.softTrimRatio = cfg.SoftTrimRatio
	}
	if cfg.HardClearRatio > 0 && cfg.HardClearRatio <= 1 {
		s.hardClearRatio = cfg.HardClearRatio
	}
	if cfg.MinPrunableToolChars > 0 {
		s.minPrunableToolChars = cfg.MinPrunableToolChars
	}

	if cfg.SoftTrim != nil {
		if cfg.SoftTrim.MaxChars > 0 {
			s.softTrimMaxChars = cfg.SoftTrim.MaxChars
		}
		if cfg.SoftTrim.HeadChars > 0 {
			s.softTrimHeadChars = cfg.SoftTrim.HeadChars
		}
		if cfg.SoftTrim.TailChars > 0 {
			s.softTrimTailChars = cfg.SoftTrim.TailChars
		}
	}

	if cfg.HardClear != nil {
		if cfg.HardClear.Enabled != nil {
			s.hardClearEnabled = *cfg.HardClear.Enabled
		}
		if cfg.HardClear.Placeholder != "" {
			s.hardClearPlaceholder = cfg.HardClear.Placeholder
		}
	}

	return s
}

// pruneContextMessages trims old tool results to reduce context window usage.
// Matching TS src/agents/pi-extensions/context-pruning/pruner.ts.
//
// Two-pass approach:
//  1. Soft trim: keep head + tail of long tool results, drop middle.
//  2. Hard clear: replace entire tool result with placeholder.
//
// Only tool results older than keepLastAssistants are eligible for pruning.
// Returns a new slice if any changes were made, otherwise the original.
func pruneContextMessages(msgs []providers.Message, contextWindowTokens int, cfg *config.ContextPruningConfig) []providers.Message {
	// Pruning runs by default for all providers. Only skip when explicitly disabled.
	if cfg != nil && cfg.Mode == "off" {
		return msgs
	}
	if contextWindowTokens <= 0 || len(msgs) == 0 {
		return msgs
	}

	settings := resolvePruningSettings(cfg)
	charWindow := contextWindowTokens * charsPerTokenEstimate

	// Find cutoff: protect last N assistant messages.
	cutoffIndex := findAssistantCutoff(msgs, settings.keepLastAssistants)
	if cutoffIndex < 0 {
		return msgs
	}

	// Find first user message — never prune before it (protects bootstrap reads).
	pruneStart := len(msgs)
	for i, m := range msgs {
		if m.Role == "user" {
			pruneStart = i
			break
		}
	}

	// Estimate total chars.
	totalChars := 0
	for _, m := range msgs {
		totalChars += estimateMessageChars(m)
	}

	ratio := float64(totalChars) / float64(charWindow)
	if ratio < settings.softTrimRatio {
		return msgs // context is small enough
	}

	// Collect prunable tool result indexes.
	var prunableIndexes []int
	for i := pruneStart; i < cutoffIndex; i++ {
		if msgs[i].Role == "tool" && msgs[i].Content != "" {
			prunableIndexes = append(prunableIndexes, i)
		}
	}

	if len(prunableIndexes) == 0 {
		return msgs
	}

	// Pass 0: Per-result context guard — force-trim any single tool result
	// exceeding 30% of the context window. Catches outlier outputs even
	// when overall context ratio is low.
	maxSingleResultChars := charWindow * 3 / 10
	var result []providers.Message
	for _, idx := range prunableIndexes {
		msgChars := estimateMessageChars(msgs[idx])
		if msgChars > maxSingleResultChars {
			if result == nil {
				result = make([]providers.Message, len(msgs))
				copy(result, msgs)
			}
			msg := msgs[idx]
			head := takeHead(msg.Content, maxSingleResultChars*7/10)
			tail := takeTail(msg.Content, maxSingleResultChars*3/10)
			trimmed := fmt.Sprintf("%s\n\n⚠️ [... middle content omitted ...]\n\n%s\n\n[Single tool result trimmed: %d chars exceeded per-result limit of %d chars.]",
				head, tail, msgChars, maxSingleResultChars)
			result[idx] = providers.Message{
				Role:       msg.Role,
				Content:    trimmed,
				ToolCallID: msg.ToolCallID,
			}
			totalChars += len(trimmed) - msgChars
		}
	}
	if result != nil {
		msgs = result
		result = nil
		// Re-check ratio after per-result guard.
		ratio = float64(totalChars) / float64(charWindow)
		if ratio < settings.softTrimRatio {
			return msgs
		}
	}

	// Pass 1: Soft trim long tool results.
	for i := range prunableIndexes {
		idx := prunableIndexes[i]
		msg := msgs[idx]
		msgChars := estimateMessageChars(msg)

		if msgChars <= settings.softTrimMaxChars {
			continue
		}

		// Lazy copy
		if result == nil {
			result = make([]providers.Message, len(msgs))
			copy(result, msgs)
		}

		// Tail-aware split: if tail has important content (errors, summaries),
		// use dynamic 70/30 split. Otherwise use configured head/tail sizes.
		headChars := settings.softTrimHeadChars
		tailChars := settings.softTrimTailChars
		if hasImportantTail(msg.Content) {
			totalBudget := headChars + tailChars
			headChars = totalBudget * 7 / 10
			tailChars = totalBudget - headChars
		}
		head := takeHead(msg.Content, headChars)
		tail := takeTail(msg.Content, tailChars)
		trimmed := fmt.Sprintf("%s\n...\n%s\n\n[Tool result trimmed: kept first %d chars and last %d chars of %d chars.]",
			head, tail, headChars, tailChars, msgChars)

		result[idx] = providers.Message{
			Role:       msg.Role,
			Content:    trimmed,
			ToolCallID: msg.ToolCallID,
		}
		totalChars += len(trimmed) - msgChars
	}

	output := msgs
	if result != nil {
		output = result
	}

	// Re-check ratio after soft trim.
	ratio = float64(totalChars) / float64(charWindow)
	if ratio < settings.hardClearRatio || !settings.hardClearEnabled {
		return output
	}

	// Check min prunable chars threshold.
	prunableChars := 0
	for _, idx := range prunableIndexes {
		prunableChars += estimateMessageChars(output[idx])
	}
	if prunableChars < settings.minPrunableToolChars {
		return output
	}

	// Pass 2: Hard clear — replace entire tool results with placeholder.
	if result == nil {
		result = make([]providers.Message, len(msgs))
		copy(result, msgs)
		output = result
	}

	for _, idx := range prunableIndexes {
		if ratio < settings.hardClearRatio {
			break
		}
		msg := output[idx]
		beforeChars := estimateMessageChars(msg)

		output[idx] = providers.Message{
			Role:       msg.Role,
			Content:    settings.hardClearPlaceholder,
			ToolCallID: msg.ToolCallID,
		}
		afterChars := len(settings.hardClearPlaceholder)
		totalChars += afterChars - beforeChars
		ratio = float64(totalChars) / float64(charWindow)
	}

	return output
}

// findAssistantCutoff returns the index of the Nth-from-last assistant message.
// Messages at or after this index are protected from pruning.
// Returns -1 if not enough assistant messages exist.
func findAssistantCutoff(msgs []providers.Message, keepLast int) int {
	if keepLast <= 0 {
		return len(msgs)
	}

	remaining := keepLast
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" {
			remaining--
			if remaining == 0 {
				return i
			}
		}
	}
	return -1
}

// estimateMessageChars returns the character count of a message's content.
func estimateMessageChars(m providers.Message) int {
	return utf8.RuneCountInString(m.Content)
}

// hasImportantTail checks if the last ~500 chars of content contain error/summary keywords.
func hasImportantTail(content string) bool {
	runes := []rune(content)
	checkLen := min(500, len(runes))
	tail := string(runes[len(runes)-checkLen:])
	return importantTailRe.MatchString(tail)
}

// takeHead returns the first n runes of s.
func takeHead(s string, n int) string {
	if n <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

// takeTail returns the last n runes of s.
func takeTail(s string, n int) string {
	if n <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[len(runes)-n:])
}
