package tokencount

import (
	"cmp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// FallbackCounter uses rune-count/3 heuristic (matches v2 behavior).
// Used when tiktoken-go is unavailable or model is unknown.
type FallbackCounter struct{}

func NewFallbackCounter() *FallbackCounter { return &FallbackCounter{} }

func (c *FallbackCounter) Count(_ string, text string) int {
	return utf8.RuneCountInString(text) / 3
}

func (c *FallbackCounter) CountMessages(_ string, msgs []providers.Message) int {
	total := 0
	for _, m := range msgs {
		total += utf8.RuneCountInString(m.Content)/3 + PerMessageOverhead
		for _, tc := range m.ToolCalls {
			total += utf8.RuneCountInString(tc.ID)/3 + utf8.RuneCountInString(tc.Name)/3
			for k, v := range tc.Arguments {
				total += utf8.RuneCountInString(k) / 3
				if s, ok := v.(string); ok {
					total += utf8.RuneCountInString(s) / 3
				} else {
					total += 10
				}
			}
		}
	}
	return total
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
