package agent

import (
	"encoding/json"
	"log/slog"
	"time"

	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// defaultSlowToolThreshold is used when no historical data is available for a tool.
const defaultSlowToolThreshold = 120 * time.Second

// toolTimingMultiplier determines how much slower than the historical max
// a tool call must be before it's considered abnormally slow.
const toolTimingMultiplier = 2.0

// minTimingSamples is the minimum number of samples needed before using
// adaptive thresholds instead of the default.
const minTimingSamples = 3

// ToolTimingStat tracks execution time statistics for a single tool.
type ToolTimingStat struct {
	Min   int64 `json:"min"`   // minimum duration in ms
	Max   int64 `json:"max"`   // maximum duration in ms
	Sum   int64 `json:"sum"`   // total duration in ms (for avg calculation)
	Count int   `json:"n"`     // number of samples
}

// ToolTimingMap maps tool names to their timing statistics.
// Concurrency contract: SlowThreshold (read) may be called from goroutines,
// but Record (write) must only be called sequentially after parallel tools complete.
type ToolTimingMap map[string]*ToolTimingStat

// ParseToolTiming reads tool timing data from session metadata.
// Returns an empty map if the key is missing or malformed.
func ParseToolTiming(metadata map[string]string) ToolTimingMap {
	raw, ok := metadata["tool_timing"]
	if !ok || raw == "" {
		return make(ToolTimingMap)
	}
	var m ToolTimingMap
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return make(ToolTimingMap)
	}
	return m
}

// Serialize returns the JSON string for storage in session metadata.
func (m ToolTimingMap) Serialize() string {
	if len(m) == 0 {
		return ""
	}
	data, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(data)
}

// Record adds a new timing sample for the given tool.
func (m ToolTimingMap) Record(toolName string, durationMs int64) {
	stat, ok := m[toolName]
	if !ok {
		m[toolName] = &ToolTimingStat{
			Min:   durationMs,
			Max:   durationMs,
			Sum:   durationMs,
			Count: 1,
		}
		return
	}
	if durationMs < stat.Min {
		stat.Min = durationMs
	}
	if durationMs > stat.Max {
		stat.Max = durationMs
	}
	stat.Sum += durationMs
	stat.Count++
}

// SlowThreshold returns the duration after which a tool call is considered
// abnormally slow. Uses adaptive threshold if enough samples exist,
// otherwise falls back to the default.
func (m ToolTimingMap) SlowThreshold(toolName string) time.Duration {
	stat, ok := m[toolName]
	if !ok || stat.Count < minTimingSamples {
		return defaultSlowToolThreshold
	}
	threshold := time.Duration(float64(stat.Max)*toolTimingMultiplier) * time.Millisecond
	// Never go below the default — short tools shouldn't trigger on tiny spikes.
	if threshold < defaultSlowToolThreshold {
		return defaultSlowToolThreshold
	}
	return threshold
}

// StartSlowTimer starts a timer that emits a tool_slow activity event if the
// tool call exceeds the adaptive threshold. Returns a stop function that MUST
// be called after tool execution to cancel the timer.
// If enabled is false, returns a no-op stop function (no timer started).
func (m ToolTimingMap) StartSlowTimer(toolName, agentID, runID string, enabled bool, emitRun func(AgentEvent)) func() {
	if !enabled {
		return func() {}
	}
	threshold := m.SlowThreshold(toolName)
	timer := time.AfterFunc(threshold, func() {
		slog.Warn("tool.slow", "agent", agentID, "tool", toolName, "threshold_ms", threshold.Milliseconds())
		emitRun(AgentEvent{
			Type:    protocol.AgentEventActivity,
			AgentID: agentID,
			RunID:   runID,
			Payload: map[string]any{
				"phase":        "tool_slow",
				"tool":         toolName,
				"threshold_ms": threshold.Milliseconds(),
			},
		})
	})
	return func() { timer.Stop() }
}

