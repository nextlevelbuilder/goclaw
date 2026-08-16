package providers

// remote_health.go exposes the process-wide reliability state to operator
// tooling (goclaw health). It reads through reliability.Default() and stays
// nil-safe: if the singleton has not been initialized yet, every accessor
// returns zero values instead of panicking.

import (
	"sort"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/reliability"
)

// RemoteHealthEntry is a point-in-time view of one provider:model health entry.
type RemoteHealthEntry struct {
	Key                 string
	CircuitState        string
	Score               float64
	Attempts            int
	Successes           int
	ConsecutiveFailures int
	CooldownRemaining   time.Duration
	RateLimitedUntil    time.Time
	TimeoutCount        int
	StreamStallCount    int
}

// RemoteHealthMetrics is a point-in-time metrics snapshot (all counters).
type RemoteHealthMetrics struct {
	Requests            uint64
	Successes           uint64
	Retries             uint64
	RateLimited         uint64
	ServerErrors        uint64
	Timeouts            uint64
	StreamStalls        uint64
	AgentRecovered      uint64
	AgentContinued      uint64
	PrematureCompletes  uint64
	LoopDetected        uint64
}

// RemoteHealthSnapshotAll returns the sorted snapshot of all health entries in
// the process-wide registry. When the registry has no entries (fresh process),
// the result is an empty slice — never nil.
func RemoteHealthSnapshotAll() []RemoteHealthEntry {
	reg := reliability.Default()
	if reg == nil || reg.Health == nil {
		return []RemoteHealthEntry{}
	}

	keys := reg.Health.Keys()
	sort.Strings(keys)
	entries := make([]RemoteHealthEntry, 0, len(keys))
	for _, k := range keys {
		provider, model := splitRemoteKey(k)
		entries = append(entries, remoteEntry(reg, provider, model))
	}
	return entries
}

// RemoteHealthSnapshot returns the snapshot of one provider:model key, with
// zero/default values when it has never been observed.
func RemoteHealthSnapshot(provider, model string) RemoteHealthEntry {
	return remoteEntry(reliability.Default(), provider, model)
}

func remoteEntry(reg *reliability.Runtime, provider, model string) RemoteHealthEntry {
	e := RemoteHealthEntry{
		Key:          provider + ":" + model,
		CircuitState: reliability.CircuitHealthy.String(),
		Score:        1.0,
	}
	if reg == nil || reg.Health == nil {
		return e
	}
	st := reg.Health.Status(provider, model)
	e.CircuitState = st.CircuitState.String()
	e.Score = reg.Health.Score(provider, model)
	e.Attempts = st.Attempts
	e.Successes = st.Successes
	e.ConsecutiveFailures = st.ConsecutiveFailures
	e.RateLimitedUntil = st.RateLimitUntil
	e.TimeoutCount = st.TimeoutCount
	e.StreamStallCount = st.StreamStallCount
	if reg.RateLimit != nil {
		e.CooldownRemaining, _ = reg.RateLimit.CooldownFor(provider, model)
	}
	return e
}

// RemoteHealthMetricsAll returns the current metrics snapshot. When the
// singleton is not initialized the snapshot is all zeroes.
func RemoteHealthMetricsAll() RemoteHealthMetrics {
	reg := reliability.Default()
	if reg == nil || reg.Metrics == nil {
		return RemoteHealthMetrics{}
	}
	s := reg.Metrics.Take()
	return RemoteHealthMetrics{
		Requests:           s.LLMRequests,
		Successes:          s.LLMSuccesses,
		Retries:            s.LLMRetries,
		RateLimited:        s.LLMRateLimited,
		ServerErrors:       s.LLMServerErrors,
		Timeouts:           s.LLMTimeouts,
		StreamStalls:       s.LLMStreamStalls,
		AgentRecovered:     s.AgentRecovered,
		AgentContinued:     s.AgentContinued,
		PrematureCompletes: s.PrematureCompleted,
		LoopDetected:       s.LoopDetected,
	}
}

// splitRemoteKey splits a provider:model key at the first colon.
func splitRemoteKey(k string) (provider, model string) {
	for i := 0; i < len(k); i++ {
		if k[i] == ':' {
			return k[:i], k[i+1:]
		}
	}
	return k, ""
}