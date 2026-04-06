// Package tokencount provides accurate per-model token counting.
// Replaces the chars/3 heuristic used in compaction + pruning decisions.
//
// V3 design: Phase 1A — foundation interface.
// Implementation: tiktoken-go with per-message hash cache.
package tokencount

import "github.com/nextlevelbuilder/goclaw/internal/providers"

// TokenCounter provides accurate per-model token counting.
type TokenCounter interface {
	// Count returns token count for raw text using the model's tokenizer.
	Count(model string, text string) int

	// CountMessages returns token count for a message list,
	// including per-message overhead (role tokens, separators).
	CountMessages(model string, msgs []providers.Message) int

	// ModelContextWindow returns max context tokens for a model.
	// Falls back to provider default if model unknown.
	ModelContextWindow(model string) int
}

// TokenizerID identifies which tokenizer a model uses.
type TokenizerID string

const (
	TokenizerCL100K   TokenizerID = "cl100k_base" // Claude, GPT-3.5/4
	TokenizerO200K    TokenizerID = "o200k_base"   // GPT-4o, GPT-5
	TokenizerFallback TokenizerID = "fallback"      // rune-count / 3
)

// ModelInfo maps a model name prefix to its tokenizer + context window.
type ModelInfo struct {
	TokenizerID   TokenizerID
	ContextWindow int
}

// DefaultRegistry provides built-in model prefix -> info mappings.
// Extend at runtime via RegisterModel().
var DefaultRegistry = map[string]ModelInfo{
	"claude-":   {TokenizerCL100K, 200_000},
	"gpt-4o":    {TokenizerO200K, 128_000},
	"gpt-4":     {TokenizerCL100K, 128_000},
	"gpt-5":     {TokenizerO200K, 1_000_000},
	"qwen-":     {TokenizerCL100K, 128_000},
	"deepseek-": {TokenizerCL100K, 128_000},
}

// PerMessageOverhead is the token overhead per message
// (role marker + separators). System messages add 4 extra.
const PerMessageOverhead = 4
