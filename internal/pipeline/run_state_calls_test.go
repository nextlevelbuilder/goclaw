package pipeline

import (
	"sync"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

func TestRunStateAppendCallAndBuildResult(t *testing.T) {
	rs := &RunState{RunID: "r1"}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ { // race-safety: concurrent appends (parallel tools)
		wg.Add(1)
		go func() {
			defer wg.Done()
			rs.AppendCall(providers.CallUsage{Type: "tool_call", Provider: "p", Model: "m",
				Usage: providers.Usage{PromptTokens: 1, TotalTokens: 1}})
		}()
	}
	wg.Wait()
	if len(rs.Calls) != 10 {
		t.Fatalf("Calls len = %d, want 10", len(rs.Calls))
	}
	res := rs.BuildResult()
	if len(res.Calls) != 10 {
		t.Fatalf("BuildResult Calls len = %d, want 10", len(res.Calls))
	}
}
