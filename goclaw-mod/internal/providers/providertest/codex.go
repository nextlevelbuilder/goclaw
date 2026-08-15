// Package providertest exposes constructors for provider types wired for
// fast, deterministic test runs. Not intended for production use.
package providertest

import "github.com/nextlevelbuilder/goclaw/internal/providers"

// staticTokenSource always returns a fixed token.
type staticTokenSource struct{ token string }

func (s *staticTokenSource) Token() (string, error) { return s.token, nil }

// NewCodexProviderFast returns a *providers.CodexProvider with Attempts=1 so
// that tests exercising router-level failover don't incur the default 3-attempt
// retry latency.
func NewCodexProviderFast(name, apiBase string) *providers.CodexProvider {
	return providers.NewCodexProvider(
		name,
		&staticTokenSource{token: "tok-" + name},
		apiBase,
		"gpt-image-2",
	).WithRetryConfig(providers.RetryConfig{Attempts: 1})
}
