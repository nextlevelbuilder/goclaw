package tokencount

import (
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
			total += len(tc.ID)/3 + len(tc.Name)/3
			for k, v := range tc.Arguments {
				total += len(k) / 3
				if s, ok := v.(string); ok {
					total += len(s) / 3
				} else {
					total += 10
				}
			}
		}
	}
	return total
}

func (c *FallbackCounter) ModelContextWindow(model string) int {
	for prefix, info := range DefaultRegistry {
		if strings.HasPrefix(model, prefix) {
			return info.ContextWindow
		}
	}
	return 200_000 // conservative default
}
