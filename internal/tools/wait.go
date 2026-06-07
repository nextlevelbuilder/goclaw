package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

const (
	defaultWaitMinMs = 100
	defaultWaitMaxMs = 300000
)

// WaitTool pauses the current agent tool sequence for a bounded duration.
type WaitTool struct{}

func NewWaitTool() *WaitTool { return &WaitTool{} }

func (t *WaitTool) Name() string { return "wait" }

func (t *WaitTool) Description() string {
	return "Pause execution before the next tool call. Use for rate-limit spacing or waiting for async work to complete."
}

func (t *WaitTool) Parameters() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"timeMs"},
		"properties": map[string]any{
			"timeMs": map[string]any{
				"type":        "integer",
				"description": "Duration to wait in milliseconds.",
				"minimum":     defaultWaitMinMs,
				"maximum":     defaultWaitMaxMs,
			},
			"reason": map[string]any{
				"type":        "string",
				"description": "Optional reason for logging and debugging.",
			},
		},
	}
}

func (t *WaitTool) Execute(ctx context.Context, args map[string]any) *Result {
	timeMs, err := parseWaitMillis(args["timeMs"])
	if err != nil {
		return ErrorResult(err.Error())
	}

	minMs, maxMs := waitLimits(ctx)
	if timeMs < minMs {
		return ErrorResult(fmt.Sprintf("timeMs must be at least %dms", minMs))
	}
	if timeMs > maxMs {
		return ErrorResult(fmt.Sprintf("timeMs must be at most %dms", maxMs))
	}

	timer := time.NewTimer(time.Duration(timeMs) * time.Millisecond)
	defer timer.Stop()

	select {
	case <-timer.C:
		reason, _ := args["reason"].(string)
		reason = strings.TrimSpace(reason)
		if reason != "" {
			return SilentResult(fmt.Sprintf("Waited %dms. Reason: %s", timeMs, reason))
		}
		return SilentResult(fmt.Sprintf("Waited %dms.", timeMs))
	case <-ctx.Done():
		return ErrorResult("wait cancelled: " + ctx.Err().Error())
	}
}

func parseWaitMillis(value any) (int, error) {
	if value == nil {
		return 0, fmt.Errorf("timeMs is required")
	}
	switch v := value.(type) {
	case int:
		return v, nil
	case int64:
		return int(v), nil
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) || math.Trunc(v) != v {
			return 0, fmt.Errorf("timeMs must be an integer number of milliseconds")
		}
		return int(v), nil
	case json.Number:
		i, err := strconv.Atoi(v.String())
		if err != nil {
			return 0, fmt.Errorf("timeMs must be an integer number of milliseconds")
		}
		return i, nil
	default:
		return 0, fmt.Errorf("timeMs must be an integer number of milliseconds")
	}
}

func waitLimits(ctx context.Context) (int, int) {
	minMs := defaultWaitMinMs
	maxMs := defaultWaitMaxMs
	if cfg := WaitToolConfigFromCtx(ctx); cfg != nil {
		if cfg.MinMs > 0 {
			minMs = clampWaitLimit(cfg.MinMs)
		}
		if cfg.MaxMs > 0 {
			maxMs = clampWaitLimit(cfg.MaxMs)
		}
	}
	if maxMs < minMs {
		maxMs = minMs
	}
	return minMs, maxMs
}

func clampWaitLimit(v int) int {
	if v < defaultWaitMinMs {
		return defaultWaitMinMs
	}
	if v > defaultWaitMaxMs {
		return defaultWaitMaxMs
	}
	return v
}
