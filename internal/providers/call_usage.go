package providers

// CallUsage attributes one LLM call (or a tool's internal LLM call) to its
// provider/model with the tokens it consumed and its cost. Usage is embedded
// so its token fields serialize flat (prompt_tokens, cache_read_input_tokens, …)
// alongside type/name/provider/model — this same value is both the in-memory
// accumulation record and the webhook `calls[]` element.
type CallUsage struct {
	Type     string  `json:"type"` // "llm_call" | "tool_call"
	Name     string  `json:"name"` // e.g. "9router/cx/gpt-5.6 #3" or "read_image"
	Provider string  `json:"provider"`
	Model    string  `json:"model"`
	Usage            // embedded, NO json tag → promotes token fields to flat CallUsage JSON
	CostUSD  float64 `json:"cost_usd,omitempty"`
}

// SumCallUsage folds per-call usage into one aggregate: summed tokens/cache,
// OR-ed PromptTokensIncludeCachedSegments. Zero value for an empty slice.
func SumCallUsage(calls []CallUsage) Usage {
	var total Usage
	for _, c := range calls {
		total.PromptTokens += c.PromptTokens
		total.CompletionTokens += c.CompletionTokens
		total.TotalTokens += c.TotalTokens
		total.CacheReadTokens += c.CacheReadTokens
		total.CacheCreationTokens += c.CacheCreationTokens
		total.ThinkingTokens += c.ThinkingTokens
		if c.PromptTokensIncludeCachedSegments {
			total.PromptTokensIncludeCachedSegments = true
		}
	}
	return total
}

// ContextTokens returns the full prompt-side size of a single LLM call —
// the actual context the model saw — normalizing provider cache accounting.
// Anthropic-style usage reports input_tokens EXCLUDING cached segments, so
// cache read/creation tokens must be added back; OpenAI-style usage
// (PromptTokensIncludeCachedSegments=true) already includes them.
// Only meaningful for per-call usage, not run-cumulative totals.
func (u Usage) ContextTokens() int {
	if u.PromptTokensIncludeCachedSegments {
		return u.PromptTokens
	}
	return u.PromptTokens + u.CacheReadTokens + u.CacheCreationTokens
}

// SumCallCost totals the per-call USD cost (0 when pricing was unavailable).
func SumCallCost(calls []CallUsage) float64 {
	var total float64
	for _, c := range calls {
		total += c.CostUSD
	}
	return total
}
