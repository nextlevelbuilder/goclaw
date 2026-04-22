package config

// Default agent configuration values.
// These are the single source of truth — all fallback/default logic should reference these
// instead of hardcoding numeric literals.
const (
	DefaultContextWindow = 200000
	// DefaultMaxTokens: upper bound on output tokens per LLM call. Was 8192 —
	// enough for most replies, but modern agents doing step-by-step reasoning
	// plus a final answer plus tool call JSON can hit the ceiling and get
	// finish_reason=length, which presents to end-users as cut output (e.g.
	// Telegram message ending in an unclosed `**`). 16384 keeps a comfortable
	// margin for long answers without meaningfully changing cost-per-call —
	// providers bill on *actual* completion tokens, not the cap.
	DefaultMaxTokens       = 16384
	DefaultMaxMessageChars = 32000
	DefaultMaxIterations   = 30
	DefaultTemperature     = 0.7
	DefaultHistoryShare    = 0.85
)
