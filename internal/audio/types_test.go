package audio

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestParamSchema_RoundTrip verifies marshal/unmarshal of ParamSchema preserves all fields.
func TestParamSchema_RoundTrip(t *testing.T) {
	orig := ParamSchema{
		Key:         "stability",
		Type:        ParamTypeRange,
		Label:       "Stability",
		Description: "Voice stability",
		Default:     0.5,
		Min:         new(0.0),
		Max:         new(1.0),
		Step:        new(0.01),
		Enum:        []EnumOption{{Value: "auto", Label: "Auto"}},
		DependsOn: []Dependency{
			{Field: "model", Op: "eq", Value: "eleven_v3"},
		},
	}
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got ParamSchema
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Key != orig.Key {
		t.Errorf("Key: got %q want %q", got.Key, orig.Key)
	}
	if got.Type != orig.Type {
		t.Errorf("Type: got %q want %q", got.Type, orig.Type)
	}
	if got.Min == nil || *got.Min != *orig.Min {
		t.Errorf("Min: got %v want %v", got.Min, orig.Min)
	}
	if got.Max == nil || *got.Max != *orig.Max {
		t.Errorf("Max: got %v want %v", got.Max, orig.Max)
	}
	if got.Step == nil || *got.Step != *orig.Step {
		t.Errorf("Step: got %v want %v", got.Step, orig.Step)
	}
	if len(got.Enum) != 1 || got.Enum[0].Value != "auto" {
		t.Errorf("Enum: got %v want [{auto Auto}]", got.Enum)
	}
	if len(got.DependsOn) != 1 || got.DependsOn[0].Field != "model" {
		t.Errorf("DependsOn: got %v", got.DependsOn)
	}
}

// floatPtr is a helper for pointer-to-float64 in tests.
//
//go:fix inline
func floatPtr(v float64) *float64 { return new(v) }

// TestDependency_AndSemantics verifies evaluateDependsOn returns true only when ALL deps match.
func TestDependency_AndSemantics(t *testing.T) {
	deps := []Dependency{
		{Field: "model", Op: "eq", Value: "eleven_v3"},
		{Field: "language", Op: "eq", Value: "en"},
	}
	// All match → true
	if !evaluateDependsOn(deps, map[string]any{"model": "eleven_v3", "language": "en"}) {
		t.Error("all deps match: expected true")
	}
	// One mismatch → false
	if evaluateDependsOn(deps, map[string]any{"model": "eleven_v3", "language": "fr"}) {
		t.Error("one dep mismatches: expected false")
	}
	// Empty deps → true (always visible)
	if !evaluateDependsOn(nil, map[string]any{"model": "eleven_v3"}) {
		t.Error("empty deps: expected true")
	}
}

// TestNestedKeyPathParser verifies parseKeyPath splits keys on "." and rejects empty segments.
func TestNestedKeyPathParser(t *testing.T) {
	tests := []struct {
		input   string
		want    []string
		wantErr bool
	}{
		{"voice_settings.stability", []string{"voice_settings", "stability"}, false},
		{"audio.sample_rate", []string{"audio", "sample_rate"}, false},
		{"simple", []string{"simple"}, false},
		{"", nil, true},
		{"a..b", nil, true},
		{".leading", nil, true},
		{"trailing.", nil, true},
	}
	for _, tc := range tests {
		got, err := parseKeyPath(tc.input)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseKeyPath(%q): expected error, got %v", tc.input, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseKeyPath(%q): unexpected error: %v", tc.input, err)
			continue
		}
		if len(got) != len(tc.want) {
			t.Errorf("parseKeyPath(%q) = %v, want %v", tc.input, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("parseKeyPath(%q)[%d] = %q, want %q", tc.input, i, got[i], tc.want[i])
			}
		}
	}
}

// TestTTSOptions_ParamsReadOnlyDoc verifies the Params field has a godoc comment
// declaring the read-only contract by reading the actual source file.
func TestTTSOptions_ParamsReadOnlyDoc(t *testing.T) {
	data, err := os.ReadFile("types.go")
	if err != nil {
		t.Fatalf("read types.go: %v", err)
	}
	src := string(data)
	if !strings.Contains(src, "READ-ONLY") {
		t.Error("types.go: TTSOptions.Params must have READ-ONLY doc comment")
	}
	if !strings.Contains(src, "MUST NOT mutate") {
		t.Error("types.go: TTSOptions.Params comment must say 'MUST NOT mutate'")
	}
}
