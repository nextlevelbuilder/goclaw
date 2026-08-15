package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestDefaultGuardrails(t *testing.T) {
	g := DefaultGuardrails()
	if g.MaxDeltaPerCycle != 0.1 {
		t.Errorf("MaxDeltaPerCycle = %v, want 0.1", g.MaxDeltaPerCycle)
	}
	if g.MinDataPoints != 100 {
		t.Errorf("MinDataPoints = %d, want 100", g.MinDataPoints)
	}
	if g.RollbackOnDrop != 20.0 {
		t.Errorf("RollbackOnDrop = %v, want 20.0", g.RollbackOnDrop)
	}
	if len(g.LockedParams) != 0 {
		t.Errorf("LockedParams should be empty, got %v", g.LockedParams)
	}
}

func TestCheckGuardrails(t *testing.T) {
	tests := []struct {
		name       string
		guardrails AdaptationGuardrails
		dataPoints int
		params     map[string]any
		sgType     store.SuggestionType
		wantErr    string // substring, empty = no error
	}{
		{
			name:       "insufficient data",
			guardrails: DefaultGuardrails(),
			dataPoints: 50,
			sgType:     store.SuggestThreshold,
			wantErr:    "insufficient data",
		},
		{
			name:       "sufficient data passes",
			guardrails: DefaultGuardrails(),
			dataPoints: 200,
			sgType:     store.SuggestThreshold,
		},
		{
			name:       "locked param hit",
			guardrails: AdaptationGuardrails{MinDataPoints: 10, LockedParams: []string{"source"}},
			dataPoints: 200,
			params:     map[string]any{"source": "mem"},
			sgType:     store.SuggestThreshold,
			wantErr:    "parameter \"source\" is locked",
		},
		{
			name:       "no locked params passes",
			guardrails: DefaultGuardrails(),
			dataPoints: 200,
			params:     map[string]any{"source": "mem"},
			sgType:     store.SuggestThreshold,
		},
		{
			name:       "zero min defaults to 100",
			guardrails: AdaptationGuardrails{MinDataPoints: 0},
			dataPoints: 50,
			sgType:     store.SuggestThreshold,
			wantErr:    "insufficient data",
		},
		{
			name:       "threshold with usage rate passes (no broken delta check)",
			guardrails: DefaultGuardrails(),
			dataPoints: 200,
			params:     map[string]any{"current_usage_rate": 0.15, "source": "vault"},
			sgType:     store.SuggestThreshold,
			// Previously this would fail with "delta 0.15 exceeds max 0.10" — now passes.
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params, _ := json.Marshal(tt.params)
			sg := store.EvolutionSuggestion{
				SuggestionType: tt.sgType,
				Parameters:     params,
			}
			err := CheckGuardrails(tt.guardrails, sg, tt.dataPoints)
			if tt.wantErr == "" && err != nil {
				t.Errorf("expected no error, got: %v", err)
			}
			if tt.wantErr != "" && err == nil {
				t.Errorf("expected error containing %q, got nil", tt.wantErr)
			}
			if tt.wantErr != "" && err != nil {
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}
