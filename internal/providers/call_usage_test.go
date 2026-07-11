package providers

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSumCallUsage(t *testing.T) {
	calls := []CallUsage{
		{Type: "llm_call", Provider: "p", Model: "m",
			Usage: Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120,
				CacheReadTokens: 80, PromptTokensIncludeCachedSegments: true}, CostUSD: 0.01},
		{Type: "tool_call", Provider: "q", Model: "n",
			Usage: Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}, CostUSD: 0.002},
	}
	got := SumCallUsage(calls)
	if got.PromptTokens != 110 || got.CompletionTokens != 25 || got.TotalTokens != 135 {
		t.Errorf("base tokens = %+v, want 110/25/135", got)
	}
	if got.CacheReadTokens != 80 {
		t.Errorf("CacheReadTokens = %d, want 80", got.CacheReadTokens)
	}
	if !got.PromptTokensIncludeCachedSegments {
		t.Error("flag not OR-ed")
	}
	if cost := SumCallCost(calls); cost < 0.0119 || cost > 0.0121 {
		t.Errorf("SumCallCost = %f, want ~0.012", cost)
	}
	if SumCallUsage(nil) != (Usage{}) {
		t.Error("nil should sum to zero Usage")
	}
}

func TestCallUsageFlatJSON(t *testing.T) {
	b, _ := json.Marshal(CallUsage{Type: "llm_call", Name: "x", Provider: "p", Model: "m",
		Usage: Usage{PromptTokens: 5, CompletionTokens: 1, TotalTokens: 6, CacheReadTokens: 3}, CostUSD: 0.5})
	s := string(b)
	for _, want := range []string{`"type":"llm_call"`, `"provider":"p"`, `"model":"m"`,
		`"prompt_tokens":5`, `"cache_read_input_tokens":3`, `"cost_usd":0.5`} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s in %s", want, s)
		}
	}
	if strings.Contains(s, `"usage"`) {
		t.Errorf("usage must be flattened, not nested: %s", s)
	}
}
