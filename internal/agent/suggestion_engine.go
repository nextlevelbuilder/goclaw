package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// AnalysisInput bundles aggregated metrics for rule evaluation.
type AnalysisInput struct {
	ToolAggs      []store.ToolAggregate
	RetrievalAggs []store.RetrievalAggregate
	Since         time.Time
}

// AnalysisRule evaluates aggregated metrics and optionally returns a suggestion.
// Returns nil when no suggestion is warranted.
type AnalysisRule interface {
	Name() string
	Evaluate(ctx context.Context, agentID uuid.UUID, input AnalysisInput) (*store.EvolutionSuggestion, error)
}

// SuggestionEngine analyzes agent metrics and generates actionable suggestions.
// Runs as a periodic cron job. Suggestions require admin review before application.
type SuggestionEngine struct {
	metrics     store.EvolutionMetricsStore
	suggestions store.EvolutionSuggestionStore
	rules       []AnalysisRule
}

// NewSuggestionEngine creates a suggestion engine with default rules.
func NewSuggestionEngine(metrics store.EvolutionMetricsStore, suggestions store.EvolutionSuggestionStore) *SuggestionEngine {
	return &SuggestionEngine{
		metrics:     metrics,
		suggestions: suggestions,
		rules: []AnalysisRule{
			&LowRetrievalUsageRule{},
			&ToolFailureRule{},
			&RepeatedToolRule{},
		},
	}
}

// dedupKey uniquely identifies a suggestion by type + metric key (e.g., tool name or source).
type dedupKey struct {
	Type store.SuggestionType
	Key  string
}

// extractMetricKey returns the distinguishing key from suggestion parameters.
func extractMetricKey(params json.RawMessage, st store.SuggestionType) string {
	var p map[string]any
	if json.Unmarshal(params, &p) != nil {
		return ""
	}
	switch st {
	case store.SuggestThreshold:
		s, _ := p["source"].(string)
		return s
	case store.SuggestToolOrder, store.SuggestSkillAdd:
		s, _ := p["tool"].(string)
		return s
	default:
		return ""
	}
}

// Analyze runs all rules against a single agent's metrics (7-day window).
// Returns newly created suggestions. Skips rules that produce duplicates.
func (e *SuggestionEngine) Analyze(ctx context.Context, agentID uuid.UUID) ([]store.EvolutionSuggestion, error) {
	since := time.Now().Add(-7 * 24 * time.Hour)

	toolAggs, err := e.metrics.AggregateToolMetrics(ctx, agentID, since)
	if err != nil {
		return nil, err
	}
	retrievalAggs, err := e.metrics.AggregateRetrievalMetrics(ctx, agentID, since)
	if err != nil {
		return nil, err
	}

	input := AnalysisInput{
		ToolAggs:      toolAggs,
		RetrievalAggs: retrievalAggs,
		Since:         since,
	}

	// Load existing pending suggestions to avoid duplicates (composite key: type + metric key).
	existing, _ := e.suggestions.ListSuggestions(ctx, agentID, "pending", 100)
	existingKeys := make(map[dedupKey]bool, len(existing))
	for _, sg := range existing {
		mk := extractMetricKey(sg.Parameters, sg.SuggestionType)
		existingKeys[dedupKey{sg.SuggestionType, mk}] = true
	}

	var created []store.EvolutionSuggestion
	for _, rule := range e.rules {
		sg, err := rule.Evaluate(ctx, agentID, input)
		if err != nil {
			slog.Debug("evolution.rule.error", "rule", rule.Name(), "agent", agentID, "error", err)
			continue
		}
		if sg == nil {
			continue
		}
		// Skip if pending suggestion with same type + metric key already exists.
		mk := extractMetricKey(sg.Parameters, sg.SuggestionType)
		if existingKeys[dedupKey{sg.SuggestionType, mk}] {
			continue
		}

		sg.ID = uuid.New()
		sg.AgentID = agentID
		sg.Status = "pending"
		if err := e.suggestions.CreateSuggestion(ctx, *sg); err != nil {
			slog.Warn("evolution.suggestion.create_failed", "rule", rule.Name(), "agent", agentID, "error", err)
			continue
		}
		created = append(created, *sg)
		existingKeys[dedupKey{sg.SuggestionType, mk}] = true
	}

	return created, nil
}

// AnalyzeAll runs analysis for all agents with evolution metrics in a tenant.
// Intended to be called from a daily cron job.
func (e *SuggestionEngine) AnalyzeAll(ctx context.Context, agentIDs []uuid.UUID) error {
	for _, agentID := range agentIDs {
		if _, err := e.Analyze(ctx, agentID); err != nil {
			slog.Warn("evolution.analyze_failed", "agent", agentID, "error", err)
		}
	}
	return nil
}

// marshalParams marshals suggestion parameters to JSON, returning nil on error.
func marshalParams(params map[string]any) json.RawMessage {
	data, _ := json.Marshal(params)
	return data
}
