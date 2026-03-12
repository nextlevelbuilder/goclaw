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
