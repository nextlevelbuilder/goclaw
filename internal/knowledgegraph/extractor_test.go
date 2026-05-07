package knowledgegraph

import (
	"encoding/json"
	"testing"
)

func TestSanitizeJSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "valid JSON unchanged",
			input: `{"confidence": 0.85}`,
			want:  `{"confidence": 0.85}`,
		},
		{
			name:  "fix spaced decimal",
			input: `{"confidence": 0. 85}`,
			want:  `{"confidence": 0.85}`,
		},
		{
			name:  "fix multiple spaced decimals",
			input: `{"a": 0. 9, "b": 1. 0}`,
			want:  `{"a": 0.9, "b": 1.0}`,
		},
		{
			name:  "fix spaced decimal with multiple spaces",
			input: `{"confidence": 0.   75}`,
			want:  `{"confidence": 0.75}`,
		},
		{
			name:  "preserve decimal-like pattern in strings",
			input: `{"description": "Founded in 2023. 15 employees"}`,
			want:  `{"description": "Founded in 2023. 15 employees"}`,
		},
		{
			name:  "preserve spaced period in strings (no digit before dot)",
			input: `{"description": "Mr. Smith leads the team"}`,
			want:  `{"description": "Mr. Smith leads the team"}`,
		},
		{
			name:  "mixed: fix value decimal, preserve string decimal",
			input: `{"description": "Version 2. 0 alpha", "confidence": 0. 9}`,
			want:  `{"description": "Version 2. 0 alpha", "confidence": 0.9}`,
		},
		{
			name:  "fix trailing comma in array",
			input: `{"items": [1, 2, 3,]}`,
			want:  `{"items": [1, 2, 3]}`,
		},
		{
			name:  "fix trailing comma in object",
			input: `{"a": 1, "b": 2,}`,
			want:  `{"a": 1, "b": 2}`,
		},
		{
			name:  "trailing comma with whitespace",
			input: `{"a": 1,  }`,
			want:  `{"a": 1  }`,
		},
		{
			name:  "trailing comma with newline",
			input: "{\"a\": 1,\n}",
			want:  "{\"a\": 1\n}",
		},
		{
			name:  "preserve comma in string value",
			input: `{"text": "hello, world,"}`,
			want:  `{"text": "hello, world,"}`,
		},
		{
			name:  "escaped quote in string",
			input: `{"text": "she said \"0. 5\" loudly", "val": 0. 5}`,
			want:  `{"text": "she said \"0. 5\" loudly", "val": 0.5}`,
		},
		{
			name:  "nested structure",
			input: `{"entities": [{"confidence": 0. 85,}]}`,
			want:  `{"entities": [{"confidence": 0.85}]}`,
		},
		{
			name:  "empty input",
			input: "",
			want:  "",
		},
		{
			name:  "no fixes needed",
			input: `{"entities": [], "relations": []}`,
			want:  `{"entities": [], "relations": []}`,
		},
		{
			name:  "truncated LLM response does not crash",
			input: `{"entities": [{"name": "Facebook Ads", "confidence": 0.9}, {"name": "TikT`,
			want:  `{"entities": [{"name": "Facebook Ads", "confidence": 0.9}, {"name": "TikT`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeJSON(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeJSON():\n  input: %s\n  got:   %s\n  want:  %s", tt.input, got, tt.want)
			}
		})
	}
}

func TestTruncatedJSONParseFails(t *testing.T) {
	truncated := `{"entities": [{"name": "Facebook Ads", "confidence": 0.9}, {"name": "TikT`
	sanitized := sanitizeJSON(truncated)

	var result ExtractionResult
	err := json.Unmarshal([]byte(sanitized), &result)
	if err == nil {
		t.Error("expected parse error for truncated JSON, got nil")
	}
}

func TestRepairTruncatedJSON(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantEnts   int
		wantRels   int
		wantEmpty  bool // expect empty result ("" or empty arrays)
	}{
		{
			name:     "already valid JSON",
			input:    `{"entities":[{"external_id":"a","name":"A","entity_type":"person","confidence":0.9}],"relations":[]}`,
			wantEnts: 1,
			wantRels: 0,
		},
		{
			name:     "truncated mid-entity",
			input:    `{"entities": [{"external_id": "a", "name": "A", "confidence": 0.9}, {"external_id": "b", "nam`,
			wantEnts: 1,
			wantRels: 0,
		},
		{
			name:     "truncated after entities before relations",
			input:    `{"entities": [{"external_id": "a", "name": "A", "confidence": 0.9}], "relations": [{"so`,
			wantEnts: 1,
			wantRels: 0,
		},
		{
			name:     "truncated mid-relation",
			input:    `{"entities": [{"external_id": "a", "name": "A", "confidence": 0.9}], "relations": [{"source_entity_id": "a", "relation_type": "knows", "target_entity_id": "b", "confidence": 0.8}, {"so`,
			wantEnts: 1,
			wantRels: 1,
		},
		{
			name:      "truncated at opening brace",
			input:     `{"ent`,
			wantEmpty: true,
		},
		{
			name:      "empty string",
			input:     "",
			wantEmpty: true,
		},
		{
			name:     "complete entities empty relations",
			input:    `{"entities": [{"external_id": "a", "name": "A", "confidence": 0.9}, {"external_id": "b", "name": "B", "confidence": 0.8}], "relations": []}`,
			wantEnts: 2,
			wantRels: 0,
		},
		{
			name:     "truncated with relations key started but no bracket",
			input:    `{"entities": [{"external_id": "a", "name": "A", "confidence": 0.9}], "relations": `,
			wantEnts: 1,
			wantRels: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := repairTruncatedJSON(tt.input)

			if tt.wantEmpty {
				if got == "" {
					return // expected empty
				}
				// Check it's valid but empty
				var result ExtractionResult
				if err := json.Unmarshal([]byte(got), &result); err != nil {
					t.Fatalf("expected valid JSON, got parse error: %v", err)
				}
				if len(result.Entities) != 0 || len(result.Relations) != 0 {
					t.Errorf("expected empty result, got %d entities, %d relations", len(result.Entities), len(result.Relations))
				}
				return
			}

			if got == "" {
				t.Fatalf("expected non-empty repaired JSON, got empty string")
			}

			var result ExtractionResult
			if err := json.Unmarshal([]byte(got), &result); err != nil {
				t.Fatalf("repaired JSON is not valid: %v\ninput: %s\nrepaired: %s", err, tt.input, got)
			}
			if len(result.Entities) != tt.wantEnts {
				t.Errorf("expected %d entities, got %d", tt.wantEnts, len(result.Entities))
			}
			if len(result.Relations) != tt.wantRels {
				t.Errorf("expected %d relations, got %d", tt.wantRels, len(result.Relations))
			}
		})
	}
}

func TestStripCodeBlock(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no code block",
			input: `{"entities": []}`,
			want:  `{"entities": []}`,
		},
		{
			name:  "json code block",
			input: "```json\n{\"entities\": []}\n```",
			want:  `{"entities": []}`,
		},
		{
			name:  "plain code block",
			input: "```\n{\"entities\": []}\n```",
			want:  `{"entities": []}`,
		},
		{
			name:  "code block with surrounding whitespace",
			input: "  ```json\n{\"entities\": []}\n```  ",
			want:  `{"entities": []}`,
		},
		{
			name:  "empty input",
			input: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripCodeBlock(tt.input)
			if got != tt.want {
				t.Errorf("stripCodeBlock():\n  got:  %s\n  want: %s", got, tt.want)
			}
		})
	}
}
