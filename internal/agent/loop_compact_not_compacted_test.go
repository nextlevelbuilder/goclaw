package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/pipeline"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// A run made only of tool call/result pairs has no clean split point, so
// compaction cannot summarize anything and must say so explicitly.
func TestMakeCompactMessages_NoCleanBoundary_ReturnsErrNotCompacted(t *testing.T) {
	t.Parallel()
	history := []providers.Message{{Role: "user", Content: "compare pages"}}
	for _, id := range []string{"c1", "c2", "c3"} {
		history = append(history,
			providers.Message{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: id, Name: "read_file"}}},
			providers.Message{Role: "tool", ToolCallID: id, Content: "result"},
		)
	}

	compact := (&Loop{}).makeCompactMessages(nil)
	got, err := compact(context.Background(), history, "test-model")

	if !errors.Is(err, pipeline.ErrNotCompacted) {
		t.Fatalf("err = %v, want pipeline.ErrNotCompacted", err)
	}
	if got != nil {
		t.Fatalf("got %d messages, want nil when nothing was compacted", len(got))
	}
}
