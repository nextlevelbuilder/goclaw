package agent

import (
	"encoding/json"
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
			wantErr:    `parameter "source" is locked`,
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
				if got := err.Error(); !contains(got, tt.wantErr) {
					t.Errorf("error %q does not contain %q", got, tt.wantErr)
				}
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && searchStr(s, substr)))
}

func searchStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
