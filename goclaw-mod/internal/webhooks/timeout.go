package webhooks

import "time"

const (
	// DefaultAgentTimeout is the fallback webhook agent-run deadline when config is unset.
	DefaultAgentTimeout = 600 * time.Second
	// MaxAgentTimeout caps the configurable webhook agent-run deadline (1h) so a config
	// typo cannot hold worker/lane slots indefinitely.
	MaxAgentTimeout = 3600 * time.Second
)

// ResolveTimeoutSec converts a config value (seconds) into a bounded duration:
// <= 0 → DefaultAgentTimeout; > 3600 → MaxAgentTimeout; otherwise the value.
func ResolveTimeoutSec(sec int) time.Duration {
	if sec <= 0 {
		return DefaultAgentTimeout
	}
	d := time.Duration(sec) * time.Second
	if d > MaxAgentTimeout {
		return MaxAgentTimeout
	}
	return d
}
