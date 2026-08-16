package providers

import (
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/reliability"
)

// TestRemoteHealthSnapshotEmpty verifies that a fresh, never-observed key
// yields a default healthy entry (score 1.0, healthy circuit, zero counters)
// rather than a panic or noise.
func TestRemoteHealthSnapshotEmpty(t *testing.T) {
	e := RemoteHealthSnapshot("never-used.provider", "never-used-model")
	if e.Key != "never-used.provider:never-used-model" {
		t.Errorf("Key = %q", e.Key)
	}
	if e.CircuitState != "healthy" {
		t.Errorf("CircuitState = %q, want healthy", e.CircuitState)
	}
	if e.Score != 1.0 {
		t.Errorf("Score = %f, want 1.0", e.Score)
	}
	if e.Attempts != 0 || e.Successes != 0 || e.ConsecutiveFailures != 0 {
		t.Errorf("fresh entry counters not zero: %+v", e)
	}
	if e.CooldownRemaining != 0 {
		t.Errorf("fresh entry cooldown = %s, want zero", e.CooldownRemaining)
	}
}

// TestRemoteHealthSnapshotAfterObserve verifies the snapshot reflects failures
// observed through the live registry singleton.
func TestRemoteHealthSnapshotAfterObserve(t *testing.T) {
	reg := reliability.Default()
	reg.Health.ObserveFailure("w3-observe", "model-a", reliability.ErrProviderRateLimited)
	reg.Health.ObserveFailure("w3-observe", "model-a", reliability.ErrProviderTimeout)

	e := RemoteHealthSnapshot("w3-observe", "model-a")
	if e.Attempts != 2 {
		t.Errorf("Attempts = %d, want 2", e.Attempts)
	}
	if e.Successes != 0 {
		t.Errorf("Successes = %d, want 0", e.Successes)
	}
	if e.ConsecutiveFailures != 2 {
		t.Errorf("ConsecutiveFailures = %d, want 2", e.ConsecutiveFailures)
	}
	if e.TimeoutCount != 1 {
		t.Errorf("TimeoutCount = %d, want 1", e.TimeoutCount)
	}
	if !e.RateLimitedUntil.After(time.Now().Add(20 * time.Second)) {
		t.Errorf("RateLimitedUntil = %v, want ~30s in the future", e.RateLimitedUntil)
	}
	if e.Score >= 1.0 {
		t.Errorf("Score = %f, want < 1.0 after failures", e.Score)
	}
}

// TestRemoteHealthCooldownReported verifies the shared rate-limit coordinator
// cooldown shows up in the snapshot when one is active.
func TestRemoteHealthCooldownReported(t *testing.T) {
	reg := reliability.Default()
	reg.RateLimit.Record429("w3-cooldown", "model-b", 10*time.Second)

	e := RemoteHealthSnapshot("w3-cooldown", "model-b")
	if e.CooldownRemaining <= 0 || e.CooldownRemaining > 11*time.Second {
		t.Errorf("CooldownRemaining = %s, want ~10s", e.CooldownRemaining)
	}
}

// TestRemoteHealthSnapshotAllSorted verifies the all-keys snapshot is sorted
// and contains entries observed across keys.
func TestRemoteHealthSnapshotAllSorted(t *testing.T) {
	reg := reliability.Default()
	reg.Health.ObserveSuccess("w3-sort-b", "model-x")
	reg.Health.ObserveSuccess("w3-sort-a", "model-y")

	entries := RemoteHealthSnapshotAll()
	if len(entries) < 2 {
		t.Fatalf("len(entries) = %d, want >= 2", len(entries))
	}
	for i := 1; i < len(entries); i++ {
		if entries[i-1].Key > entries[i].Key {
			t.Errorf("snapshot not sorted at %d: %q > %q", i, entries[i-1].Key, entries[i].Key)
		}
	}
	found := false
	for _, e := range entries {
		if e.Key == "w3-sort-b:model-x" {
			found = true
			if e.Successes != 1 {
				t.Errorf("Successes for w3-sort-b:model-x = %d, want 1", e.Successes)
			}
		}
	}
	if !found {
		t.Error("w3-sort-b:model-x missing from snapshot")
	}
}

// TestRemoteHealthMetricsAll verifies the metrics snapshot reflects recorded
// counters. The singleton metrics are process-global and shared with other
// tests in the package, so assertions are delta-based: the counters must
// increase by exactly the amount recorded here.
func TestRemoteHealthMetricsAll(t *testing.T) {
	reg := reliability.Default()
	before := reg.Metrics.Take()

	reg.Metrics.RecordLLMRequest()
	reg.Metrics.RecordLLMRequest()
	reg.Metrics.RecordLLMSuccess()
	reg.Metrics.RecordLLMRateLimited()

	m := RemoteHealthMetricsAll()
	if got := m.Requests - before.LLMRequests; got != 2 {
		t.Errorf("Requests delta = %d, want 2", got)
	}
	if got := m.Successes - before.LLMSuccesses; got != 1 {
		t.Errorf("Successes delta = %d, want 1", got)
	}
	if got := m.RateLimited - before.LLMRateLimited; got != 1 {
		t.Errorf("RateLimited delta = %d, want 1", got)
	}
}