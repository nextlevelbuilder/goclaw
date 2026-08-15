package tokencount

import (
	"cmp"
	"encoding/json"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// FallbackCounter is the BEST-EFFORT heuristic counter used when tiktoken-go is
// unavailable or the model is unknown (e.g. 9router brand models that match no
// registry prefix). It is NOT a proven token bound: real tokenizers vary by
// language, and dense scripts like Vietnamese tokenize at ~2 chars/token or
// worse. Because the pre-transport guard treats this count as its ceiling, the
// heuristic deliberately uses a CONSERVATIVE chars-per-token ratio so it errs
// toward over-counting (compact/block early) rather than under-counting (send an
// oversized request). The only way to a provable ceiling for an un-tokenizable
// model is to register it with a real tokenizer or use the provider's own token
// count.
type FallbackCounter struct{}

func NewFallbackCounter() *FallbackCounter { return &FallbackCounter{} }

// fallbackCharsPerToken is the conservative chars-per-token divisor for the
// guard-facing count methods. Lower than a naive ~3-4 chars/token so mixed
// Vietnamese/code content is over-counted rather than under-counted. This is a
// safety heuristic, not an exact tokenization.
const fallbackCharsPerToken = 2

func (c *FallbackCounter) Count(_ string, text string) int {
	return utf8.RuneCountInString(text) / fallbackCharsPerToken
}

func (c *FallbackCounter) CountMessages(_ string, msgs []providers.Message) int {
	total := 0
	for _, m := range msgs {
		total += utf8.RuneCountInString(m.Content)/fallbackCharsPerToken + PerMessageOverhead
		// Match tiktokenCounter.CountMessages coverage so the fallback path
		// applies the same best-effort ceiling (thinking, tool-result id, raw
		// blocks, tool args, media all count toward the wire payload).
		total += utf8.RuneCountInString(m.Thinking) / fallbackCharsPerToken
		total += utf8.RuneCountInString(m.ToolCallID) / fallbackCharsPerToken
		total += utf8.RuneCountInString(string(m.RawAssistantContent)) / fallbackCharsPerToken
		for _, tc := range m.ToolCalls {
			total += utf8.RuneCountInString(tc.ID)/fallbackCharsPerToken + utf8.RuneCountInString(tc.Name)/fallbackCharsPerToken
			if b, err := json.Marshal(tc.Arguments); err == nil {
				total += utf8.RuneCountInString(string(b)) / fallbackCharsPerToken
			}
		}
		total += fallbackMediaTokenCost(m)
	}
	return total
}

// fallbackMediaTokenCost mirrors mediaTokenCost for the heuristic counter.
func fallbackMediaTokenCost(m providers.Message) int {
	const perInlineMediaItem = 1600
	cost := 0
	for _, img := range m.Images {
		if img.Data != "" || img.URL != "" {
			cost += perInlineMediaItem
		}
	}
	for _, vid := range m.Videos {
		if vid.Data != "" || vid.URL != "" {
			cost += perInlineMediaItem
		}
	}
	return cost
}

// CountToolSchemas returns a conservative heuristic count for the JSON-serialised
// tool list. Returns 0 for nil or empty slice.
func (c *FallbackCounter) CountToolSchemas(_ string, tools []providers.ToolDefinition) int {
	if len(tools) == 0 {
		return 0
	}
	blob, _ := json.Marshal(tools)
	return utf8.RuneCountInString(string(blob)) / fallbackCharsPerToken
}

// ModelContextWindow uses longest-prefix-match to avoid ambiguity
// (e.g., "gpt-4o" must match before "gpt-4").
func (c *FallbackCounter) ModelContextWindow(model string) int {
	// Sort prefixes longest-first for correct matching.
	keys := make([]string, 0, len(DefaultRegistry))
	for k := range DefaultRegistry {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b string) int { return cmp.Compare(len(b), len(a)) })

	for _, prefix := range keys {
		if strings.HasPrefix(model, prefix) {
			return DefaultRegistry[prefix].ContextWindow
		}
	}
	return 200_000 // conservative default
}
