package providers

// codex_test_helpers_pool_chain.go — test constructor for CodexProvider with
// internal retries disabled. Used by integration tests in internal/tools that
// need a real *CodexProvider backed by an httptest.Server without the default
// 3-attempt retry loop adding test latency.
//
// This file is intentionally NOT a _test.go file so it can be imported from
// other packages' test binaries (e.g. internal/tools). The exported function is
// prefixed with "NewTest" to signal its intended scope.

// NewTestCodexProviderFast creates a *CodexProvider with Attempts=1 (no retries)
// and the given name/apiBase. Intended for use in tests that exercise router-level
// failover logic, where per-provider retries would add latency without coverage value.
func NewTestCodexProviderFast(name, apiBase string) *CodexProvider {
	p := NewCodexProvider(name, &staticTestTokenSource{token: "tok-" + name}, apiBase, "gpt-image-2")
	p.retryConfig.Attempts = 1
	return p
}

// staticTestTokenSource is a minimal TokenSource for test use: always returns a fixed token.
// Distinct from the unexported staticTokenSource in codex_test.go (which is _test.go scoped).
type staticTestTokenSource struct {
	token string
}

func (s *staticTestTokenSource) Token() (string, error) { return s.token, nil }
