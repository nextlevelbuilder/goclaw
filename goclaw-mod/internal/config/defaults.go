package config

// Default agent configuration values.
// These are the single source of truth — all fallback/default logic should reference these
// instead of hardcoding numeric literals.
const (
	DefaultContextWindow   = 200000
	DefaultMaxTokens       = 8192
	DefaultMaxMessageChars = 32000
	DefaultMaxIterations   = 30
	DefaultTemperature     = 0.7
	DefaultHistoryShare    = 0.85
)
