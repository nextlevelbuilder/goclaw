package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestLowRetrievalUsageRule(t *testing.T) {
	rule := &LowRetrievalUsageRule{}
	tests := []struct {
		name    string
		aggs    []store.RetrievalAggregate
		wantNil bool
	}{
		{"below min queries -> skip", []store.RetrievalAggregate{{QueryCount: 49, UsageRate: 0.1}}, true},
		{"low usage triggers", []store.RetrievalAggregate{{Source: "mem", QueryCount: 60, UsageRate: 0.15}}, false},
		{"normal usage no trigger", []store.RetrievalAggregate{{QueryCount: 60, UsageRate: 0.5}}, true},
		{"empty aggs", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sg, err := rule.Evaluate(context.Background(), uuid.New(), AnalysisInput{RetrievalAggs: tt.aggs})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantNil && sg != nil {
				t.Errorf("expected nil suggestion, got %+v", sg)
			}
			if !tt.wantNil && sg == nil {
				t.Error("expected suggestion, got nil")
			}
			if !tt.wantNil && sg != nil && sg.SuggestionType != store.SuggestThreshold {
				t.Errorf("expected SuggestThreshold, got %q", sg.SuggestionType)
			}
		})
	}
}

func TestToolFailureRule(t *testing.T) {
	rule := &ToolFailureRule{}
	tests := []struct {
		name    string
		aggs    []store.ToolAggregate
		wantNil bool
	}{
		{"below min calls -> skip", []store.ToolAggregate{{CallCount: 19, SuccessRate: 0.05}}, true},
		{"high failure triggers", []store.ToolAggregate{{ToolName: "broken", CallCount: 30, SuccessRate: 0.05, AvgDurationMs: 1000}}, false},
		{"normal success no trigger", []store.ToolAggregate{{CallCount: 30, SuccessRate: 0.8}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sg, err := rule.Evaluate(context.Background(), uuid.New(), AnalysisInput{ToolAggs: tt.aggs})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantNil && sg != nil {
				t.Errorf("expected nil, got %+v", sg)
			}
			if !tt.wantNil && sg == nil {
				t.Error("expected suggestion, got nil")
			}
		})
	}
}

func TestRepeatedToolRule(t *testing.T) {
	rule := &RepeatedToolRule{}
	tests := []struct {
		name    string
		aggs    []store.ToolAggregate
		wantNil bool
	}{
		{"low call count -> skip", []store.ToolAggregate{{CallCount: 50, SuccessRate: 0.9}}, true},
		{"high calls + high success", []store.ToolAggregate{{ToolName: "exec", CallCount: 150, SuccessRate: 0.8}}, false},
		{"high calls + low success -> skip", []store.ToolAggregate{{CallCount: 150, SuccessRate: 0.3}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sg, err := rule.Evaluate(context.Background(), uuid.New(), AnalysisInput{ToolAggs: tt.aggs})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantNil && sg != nil {
				t.Errorf("expected nil, got %+v", sg)
			}
			if !tt.wantNil && sg == nil {
				t.Error("expected suggestion, got nil")
			}
			if !tt.wantNil && sg != nil && sg.SuggestionType != store.SuggestSkillAdd {
				t.Errorf("expected SuggestSkillAdd, got %q", sg.SuggestionType)
			}
		})
	}
}

func TestRepeatedToolRule_IncludesSkillDraft(t *testing.T) {
	rule := &RepeatedToolRule{}
	sg, err := rule.Evaluate(context.Background(), uuid.New(), AnalysisInput{
		ToolAggs: []store.ToolAggregate{{ToolName: "web_search", CallCount: 200, SuccessRate: 0.9}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sg == nil {
		t.Fatal("expected suggestion, got nil")
	}

	var params map[string]any
	if err := json.Unmarshal(sg.Parameters, &params); err != nil {
		t.Fatalf("failed to parse parameters: %v", err)
	}

	draft, ok := params["skill_draft"].(string)
	if !ok || draft == "" {
		t.Fatal("parameters missing skill_draft field")
	}

	// Verify draft is parseable SKILL.md
	name, _, _, _ := skills.ParseSkillFrontmatter(draft)
	if name == "" {
		t.Error("skill_draft has invalid frontmatter (missing name)")
	}
	if !strings.Contains(draft, "web_search") {
		t.Error("skill_draft missing tool name")
	}
}
