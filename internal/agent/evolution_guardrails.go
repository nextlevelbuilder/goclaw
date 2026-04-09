package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"math"
	"slices"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// AdaptationGuardrails controls auto-adaptation safety limits.
// Stored in agent other_config JSONB under "evolution_guardrails".
type AdaptationGuardrails struct {
	MaxDeltaPerCycle float64  `json:"max_delta_per_cycle"`     // max parameter change per cycle (default 0.1)
	MinDataPoints    int      `json:"min_data_points"`         // min metrics before applying (default 100)
	RollbackOnDrop   float64  `json:"rollback_on_drop_pct"`    // quality drop % triggering rollback (default 20.0)
	LockedParams     []string `json:"locked_params,omitempty"` // params that cannot be auto-changed
}

// DefaultGuardrails returns conservative defaults.
func DefaultGuardrails() AdaptationGuardrails {
	return AdaptationGuardrails{
		MaxDeltaPerCycle: 0.1,
		MinDataPoints:    100,
		RollbackOnDrop:   20.0,
	}
}

// CheckGuardrails validates a suggestion against guardrail constraints.
// Returns an error describing the violation, or nil if safe to apply.
func CheckGuardrails(g AdaptationGuardrails, sg store.EvolutionSuggestion, dataPoints int) error {
	// Check minimum data points.
	minDP := g.MinDataPoints
	if minDP <= 0 {
		minDP = 100
	}
	if dataPoints < minDP {
		return fmt.Errorf("insufficient data: %d points, need %d", dataPoints, minDP)
	}

	// Check locked parameters.
	if len(g.LockedParams) > 0 {
		var params map[string]any
		if err := json.Unmarshal(sg.Parameters, &params); err == nil {
			for key := range params {
				if slices.Contains(g.LockedParams, key) {
					return fmt.Errorf("parameter %q is locked", key)
				}
			}
		}
	}

	// Check delta constraint for threshold-type suggestions.
	if sg.SuggestionType == store.SuggestThreshold {
		maxDelta := g.MaxDeltaPerCycle
		if maxDelta <= 0 {
			maxDelta = 0.1
		}
		var params map[string]any
		if err := json.Unmarshal(sg.Parameters, &params); err == nil {
			if currentRate, ok := params["current_usage_rate"].(float64); ok {
				// Proposed change is bounded by max delta.
				if math.Abs(currentRate) > maxDelta {
					return fmt.Errorf("delta %.2f exceeds max %.2f per cycle", currentRate, maxDelta)
				}
			}
		}
	}

	return nil
}

// ApplySuggestion applies an approved suggestion's parameters to the agent's other_config JSONB.
// Stores the previous values in the suggestion's parameters for rollback.
// Scope: only retrieval-related parameters. Never security or core settings.
func ApplySuggestion(ctx context.Context, agentStore store.AgentStore, sugStore store.EvolutionSuggestionStore, sg store.EvolutionSuggestion) error {
	// Load current agent config.
	agent, err := agentStore.GetByID(ctx, sg.AgentID)
	if err != nil {
		return fmt.Errorf("load agent: %w", err)
	}

	var otherConfig map[string]any
	if len(agent.OtherConfig) > 0 {
		_ = json.Unmarshal(agent.OtherConfig, &otherConfig)
	}
	if otherConfig == nil {
		otherConfig = make(map[string]any)
	}

	// Store baseline for rollback: snapshot current retrieval params.
	baseline := make(map[string]any)
	if v, ok := otherConfig["retrieval_threshold"]; ok {
		baseline["retrieval_threshold"] = v
	}

	// Apply suggestion-specific changes based on type.
	switch sg.SuggestionType {
	case store.SuggestThreshold:
		// Raise retrieval threshold slightly (bounded by guardrails).
		current, _ := otherConfig["retrieval_threshold"].(float64)
		if current == 0 {
			current = 0.3 // default threshold
		}
		otherConfig["retrieval_threshold"] = current + 0.05 // conservative bump
	default:
		// Non-threshold suggestions are informational only, no auto-apply.
		return fmt.Errorf("suggestion type %q does not support auto-apply", sg.SuggestionType)
	}

	// Save updated config.
	configJSON, _ := json.Marshal(otherConfig)
	if err := agentStore.Update(ctx, sg.AgentID, map[string]any{
		"other_config": json.RawMessage(configJSON),
	}); err != nil {
		return fmt.Errorf("update agent config: %w", err)
	}

	// Persist baseline into suggestion parameters for future rollback.
	var sgParams map[string]any
	_ = json.Unmarshal(sg.Parameters, &sgParams)
	if sgParams == nil {
		sgParams = make(map[string]any)
	}
	sgParams["_baseline"] = baseline
	updatedParams, _ := json.Marshal(sgParams)
	if err := sugStore.UpdateSuggestionParameters(ctx, sg.ID, updatedParams); err != nil {
		return fmt.Errorf("save baseline: %w", err)
	}
	if err := sugStore.UpdateSuggestionStatus(ctx, sg.ID, "applied", "auto-adapt"); err != nil {
		return fmt.Errorf("update suggestion status: %w", err)
	}

	slog.Info("evolution.auto_adapt.applied", "agent", sg.AgentID, "type", sg.SuggestionType)
	return nil
}

// RollbackSuggestion reverts an applied suggestion by restoring baseline values.
func RollbackSuggestion(ctx context.Context, agentStore store.AgentStore, sugStore store.EvolutionSuggestionStore, sg store.EvolutionSuggestion) error {
	// Extract baseline from suggestion params.
	var sgParams map[string]any
	if err := json.Unmarshal(sg.Parameters, &sgParams); err != nil {
		return fmt.Errorf("parse suggestion params: %w", err)
	}
	baseline, _ := sgParams["_baseline"].(map[string]any)
	if baseline == nil {
		return fmt.Errorf("no baseline data for rollback")
	}

	// Load current config and restore baseline values.
	agent, err := agentStore.GetByID(ctx, sg.AgentID)
	if err != nil {
		return fmt.Errorf("load agent: %w", err)
	}
	var otherConfig map[string]any
	if len(agent.OtherConfig) > 0 {
		_ = json.Unmarshal(agent.OtherConfig, &otherConfig)
	}
	if otherConfig == nil {
		otherConfig = make(map[string]any)
	}

	// Restore each baseline parameter.
	maps.Copy(otherConfig, baseline)

	configJSON, _ := json.Marshal(otherConfig)
	if err := agentStore.Update(ctx, sg.AgentID, map[string]any{
		"other_config": json.RawMessage(configJSON),
	}); err != nil {
		return fmt.Errorf("rollback agent config: %w", err)
	}

	if err := sugStore.UpdateSuggestionStatus(ctx, sg.ID, "rolled_back", "auto-adapt"); err != nil {
		return fmt.Errorf("update suggestion status: %w", err)
	}

	slog.Info("evolution.auto_adapt.rolled_back", "agent", sg.AgentID, "type", sg.SuggestionType)
	return nil
}

// EvaluateApplied checks if applied suggestions improved or degraded quality.
// Rolls back suggestions where quality dropped more than the guardrail threshold.
func EvaluateApplied(ctx context.Context, agentID uuid.UUID, guardrails AdaptationGuardrails,
	metricsStore store.EvolutionMetricsStore, sugStore store.EvolutionSuggestionStore,
	agentStore store.AgentStore) error {

	// Find applied suggestions for this agent.
	applied, err := sugStore.ListSuggestions(ctx, agentID, "applied", 20)
	if err != nil {
		return err
	}

	rollbackPct := guardrails.RollbackOnDrop
	if rollbackPct <= 0 {
		rollbackPct = 20.0
	}

	for _, sg := range applied {
		// Only evaluate threshold suggestions (the only auto-apply type).
		if sg.SuggestionType != store.SuggestThreshold {
			continue
		}

		// Compare current retrieval usage to baseline.
		var sgParams map[string]any
		_ = json.Unmarshal(sg.Parameters, &sgParams)
		baselineRate, _ := sgParams["current_usage_rate"].(float64)
		if baselineRate == 0 {
			continue
		}

		// Get current retrieval metrics.
		currentAggs, err := metricsStore.AggregateRetrievalMetrics(ctx, agentID, sg.CreatedAt)
		if err != nil || len(currentAggs) == 0 {
			continue
		}

		// Check if usage rate dropped significantly.
		for _, agg := range currentAggs {
			drop := (baselineRate - agg.UsageRate) / baselineRate * 100
			if drop > rollbackPct {
				slog.Warn("evolution.auto_adapt.quality_drop",
					"agent", agentID, "source", agg.Source,
					"baseline", baselineRate, "current", agg.UsageRate, "drop_pct", drop)
				_ = RollbackSuggestion(ctx, agentStore, sugStore, sg)
				break
			}
		}
	}

	return nil
}
