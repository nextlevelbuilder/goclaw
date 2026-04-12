package pipeline

import (
	"context"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

func TestStreamingToolExecutorDoneRejectsLateTools(t *testing.T) {
	t.Parallel()

	started := make(chan string, 1)
	release := make(chan struct{})
	executed := make(chan string, 2)

	executor := NewStreamingToolExecutor(
		func(providers.ToolCall) bool { return true },
		func(_ context.Context, tc providers.ToolCall) (providers.Message, any, error) {
			started <- tc.ID
			<-release
			executed <- tc.ID
			return providers.Message{Role: "tool", Content: tc.Name}, nil, nil
		},
		context.Background(),
	)

	if ok := executor.AddTool(providers.ToolCall{ID: "tc1", Name: "read_file"}); !ok {
		t.Fatal("expected first tool to be accepted")
	}
	select {
	case got := <-started:
		if got != "tc1" {
			t.Fatalf("started tool = %q, want tc1", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first tool to start")
	}

	results := executor.Done()
	if ok := executor.AddTool(providers.ToolCall{ID: "tc2", Name: "write_file"}); ok {
		t.Fatal("late tool should be rejected after Done()")
	}

	close(release)

	var updates []StreamToolUpdate
	for update := range results {
		updates = append(updates, update)
	}
	if len(updates) != 1 {
		t.Fatalf("received %d updates, want 1", len(updates))
	}
	if updates[0].Call.ID != "tc1" {
		t.Fatalf("update tool id = %q, want tc1", updates[0].Call.ID)
	}

	select {
	case got := <-executed:
		if got != "tc1" {
			t.Fatalf("executed tool = %q, want tc1", got)
		}
	default:
		t.Fatal("expected first tool execution to complete")
	}
	select {
	case got := <-executed:
		t.Fatalf("unexpected extra tool execution: %q", got)
	default:
	}
}
