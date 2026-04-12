package vault

import (
	"testing"
	"time"
)

// fastBackoffsForTest overrides the package-level enrichRetryBackoffs and
// enrichRetryTimeouts arrays so retry tests don't wait through the real
// exponential backoff (default {0, 2s, 4s} = 6 seconds per all-retry test).
//
// Production behavior is 100% unchanged — only the test sees the fast values.
// Original values are restored via t.Cleanup so parallel/sequential tests
// remain isolated.
//
// Use in any test that exercises callClassifyWithRetry / chatWithRetry with
// >1 attempt. Do NOT use in tests that explicitly assert on the default
// values (e.g. TestCallClassifyWithRetry_RetriesAndBackoffs).
func fastBackoffsForTest(t *testing.T) {
	t.Helper()
	savedBackoffs := enrichRetryBackoffs
	savedTimeouts := enrichRetryTimeouts
	enrichRetryBackoffs = [enrichMaxRetries]time.Duration{0, time.Millisecond, time.Millisecond}
	enrichRetryTimeouts = [enrichMaxRetries]time.Duration{time.Second, time.Second, time.Second}
	t.Cleanup(func() {
		enrichRetryBackoffs = savedBackoffs
		enrichRetryTimeouts = savedTimeouts
	})
}
