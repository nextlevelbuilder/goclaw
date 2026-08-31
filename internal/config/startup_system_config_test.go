package config

import "testing"

// Phase 7 Decision 8 — gateway.task_recovery_interval_sec is a process-wide,
// startup-only key. It feeds the single team-task recovery ticker built once at
// startup (cmd/gateway_lifecycle.go), which never re-reads the field. It must be
// applied ONLY via ApplyStartupSystemConfigs (called once from the master-tenant
// seed) and MUST NOT be overlaid by the dynamic ApplySystemConfigs path, which
// runs on every system-config-changed event under the CHANGING tenant's context.
//
// If it were still in ApplySystemConfigs, any tenant's unrelated config edit would
// mutate the shared d.cfg.Gateway.TaskRecoveryIntervalSec — an ineffective (ticker
// never re-reads) cross-tenant write. These tests pin the split.

// The dynamic per-tenant path must NOT touch task_recovery_interval_sec. A config
// event carrying only that key must leave the shared field unchanged, so a
// tenant's edit cannot mutate the process-wide recovery interval at runtime.
func TestApplySystemConfigsIgnoresRecoveryInterval(t *testing.T) {
	c := &Config{}
	c.Gateway.TaskRecoveryIntervalSec = 300 // startup value

	// Simulate a tenant's system-config-changed event carrying a new value.
	c.ApplySystemConfigs(map[string]string{
		"gateway.task_recovery_interval_sec": "60",
	})

	if c.Gateway.TaskRecoveryIntervalSec != 300 {
		t.Fatalf("ApplySystemConfigs must NOT overlay task_recovery_interval_sec (startup-only); got %d, want 300",
			c.Gateway.TaskRecoveryIntervalSec)
	}
}

// The startup-only path applies the key exactly as the old integer() overlay did:
// present + non-empty + parseable overrides; anything else leaves it untouched.
func TestApplyStartupSystemConfigsAppliesRecoveryInterval(t *testing.T) {
	cases := []struct {
		name string
		in   map[string]string
		want int
	}{
		{"present valid overrides", map[string]string{"gateway.task_recovery_interval_sec": "120"}, 120},
		{"absent keeps default", map[string]string{}, 300},
		{"present empty keeps default", map[string]string{"gateway.task_recovery_interval_sec": ""}, 300},
		{"present non-numeric keeps default", map[string]string{"gateway.task_recovery_interval_sec": "abc"}, 300},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &Config{}
			c.Gateway.TaskRecoveryIntervalSec = 300
			c.ApplyStartupSystemConfigs(tc.in)
			if c.Gateway.TaskRecoveryIntervalSec != tc.want {
				t.Fatalf("ApplyStartupSystemConfigs(%v) = %d, want %d", tc.in, c.Gateway.TaskRecoveryIntervalSec, tc.want)
			}
		})
	}
}

// The dynamic path must still apply the OTHER gateway keys it is responsible for —
// proving the Decision 8 split removed only the startup-only key, not its
// neighbors. A regression that deleted too much would surface here.
func TestApplySystemConfigsStillAppliesDynamicKeys(t *testing.T) {
	c := &Config{}
	c.ApplySystemConfigs(map[string]string{
		"gateway.rate_limit_rpm":            "42",
		"gateway.max_message_chars":         "9999",
		"gateway.webhook_async_timeout_sec": "77",
	})
	if c.Gateway.RateLimitRPM != 42 {
		t.Fatalf("rate_limit_rpm = %d, want 42", c.Gateway.RateLimitRPM)
	}
	if c.Gateway.MaxMessageChars != 9999 {
		t.Fatalf("max_message_chars = %d, want 9999", c.Gateway.MaxMessageChars)
	}
	if c.Gateway.WebhookAsyncTimeoutSec != 77 {
		t.Fatalf("webhook_async_timeout_sec = %d, want 77", c.Gateway.WebhookAsyncTimeoutSec)
	}
}
