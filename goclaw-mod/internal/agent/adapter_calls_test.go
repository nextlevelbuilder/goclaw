package agent

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/pipeline"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

func TestConvertRunResultMapsCalls(t *testing.T) {
	pr := &pipeline.RunResult{
		RunID: "r1",
		Calls: []providers.CallUsage{
			{Type: "llm_call", Provider: "p", Model: "m", Usage: providers.Usage{TotalTokens: 5}},
		},
	}
	got := convertRunResult(pr)
	if len(got.Calls) != 1 || got.Calls[0].Type != "llm_call" {
		t.Fatalf("Calls not mapped: %+v", got.Calls)
	}
}
