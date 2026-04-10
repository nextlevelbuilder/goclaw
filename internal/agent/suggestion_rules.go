package agent

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Minimum data points required before a rule triggers.
const (
	minRetrievalQueries = 50
	minToolCalls        = 20
	highToolCallsWeek   = 100
)

// LowRetrievalUsageRule suggests raising retrieval threshold when usage rate is low.
// Triggers: usage_rate < 0.2 over 50+ queries for any source.
type LowRetrievalUsageRule struct{}

func (r *LowRetrievalUsageRule) Name() string { return "low_retrieval_usage" }

func (r *LowRetrievalUsageRule) Evaluate(_ context.Context, _ uuid.UUID, input AnalysisInput) (*store.EvolutionSuggestion, error) {
	for _, agg := range input.RetrievalAggs {
		if agg.QueryCount < minRetrievalQueries {
			continue
		}
		if agg.UsageRate < 0.2 {
			return &store.EvolutionSuggestion{
				SuggestionType: store.SuggestThreshold,
				Suggestion:     fmt.Sprintf("Raise retrieval threshold for source %q — only %.0f%% of results used in replies", agg.Source, agg.UsageRate*100),
				Rationale:      fmt.Sprintf("%d queries, %.1f%% usage rate, avg score %.2f", agg.QueryCount, agg.UsageRate*100, agg.AvgScore),
				Parameters:     marshalParams(map[string]any{"source": agg.Source, "current_usage_rate": agg.UsageRate, "query_count": agg.QueryCount}),
			}, nil
		}
	}
	return nil, nil
}

// ToolFailureRule suggests removing or fixing tools with high failure rates.
// Triggers: success_rate < 0.1 over 20+ calls.
type ToolFailureRule struct{}

func (r *ToolFailureRule) Name() string { return "tool_failure" }

func (r *ToolFailureRule) Evaluate(_ context.Context, _ uuid.UUID, input AnalysisInput) (*store.EvolutionSuggestion, error) {
	for _, agg := range input.ToolAggs {
		if agg.CallCount < minToolCalls {
			continue
		}
		if agg.SuccessRate < 0.1 {
			return &store.EvolutionSuggestion{
				SuggestionType: store.SuggestToolOrder,
				Suggestion:     fmt.Sprintf("Tool %q has %.0f%% failure rate — consider disabling or fixing", agg.ToolName, (1-agg.SuccessRate)*100),
				Rationale:      fmt.Sprintf("%d calls, %.1f%% success rate, avg %.0fms", agg.CallCount, agg.SuccessRate*100, agg.AvgDurationMs),
				Parameters:     marshalParams(map[string]any{"tool": agg.ToolName, "success_rate": agg.SuccessRate, "call_count": agg.CallCount}),
			}, nil
		}
	}
	return nil, nil
}

// RepeatedToolRule suggests creating a skill when a tool is called excessively.
// Triggers: call_count > 100/week for a single tool.
type RepeatedToolRule struct{}

func (r *RepeatedToolRule) Name() string { return "repeated_tool" }

func (r *RepeatedToolRule) Evaluate(_ context.Context, _ uuid.UUID, input AnalysisInput) (*store.EvolutionSuggestion, error) {
	for _, agg := range input.ToolAggs {
		if agg.CallCount > highToolCallsWeek && agg.SuccessRate > 0.5 {
			return &store.EvolutionSuggestion{
				SuggestionType: store.SuggestSkillAdd,
				Suggestion:     fmt.Sprintf("Tool %q called %d times this week — consider creating a skill to encapsulate this pattern", agg.ToolName, agg.CallCount),
				Rationale:      fmt.Sprintf("High-frequency successful tool (%d calls, %.0f%% success)", agg.CallCount, agg.SuccessRate*100),
				Parameters: marshalParams(map[string]any{
					"tool":        agg.ToolName,
					"call_count":  agg.CallCount,
					"skill_draft": GenerateSkillDraft(agg.ToolName, agg.CallCount, agg.SuccessRate),
				}),
			}, nil
		}
	}
	return nil, nil
}
