package pipeline

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/tokencount"
)

// TestContextStage_ToolOverhead_ThinkToolsPopulated verifies that:
// 1. state.Think.Tools is populated by BuildFilteredTools called in ContextStage.
// 2. state.Context.OverheadTokens > CountMessages(system) when tools are present.
func TestContextStage_ToolOverhead_ThinkToolsPopulated(t *testing.T) {
	t.Parallel()

	const numTools = 5

	// Use real FallbackCounter so we get deterministic non-zero tool counts.
	counter := tokencount.NewFallbackCounter()

	fixture := fixtureTools(numTools)

	deps := &PipelineDeps{
		TokenCounter: counter,
		BuildMessages: func(_ context.Context, _ *RunInput, _ []providers.Message, _ string) ([]providers.Message, error) {
			return []providers.Message{
				{Role: "system", Content: "You are a capable AI assistant."},
			}, nil
		},
		BuildFilteredTools: func(_ *RunState) ([]providers.ToolDefinition, error) {
			return fixture, nil
		},
	}

	stage := NewContextStage(deps)
	state := defaultState()

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	// Assert: state.Think.Tools populated.
	if len(state.Think.Tools) != numTools {
		t.Errorf("state.Think.Tools len = %d, want %d", len(state.Think.Tools), numTools)
	}

	// Compute expected system-only overhead to compare.
	sysMsg := providers.Message{Role: "system", Content: "You are a capable AI assistant."}
	systemOnly := counter.CountMessages("claude-3", []providers.Message{sysMsg})

	// Assert: overhead includes tool tokens — strictly greater than system-only.
	if state.Context.OverheadTokens <= systemOnly {
		t.Errorf("OverheadTokens = %d, want > %d (system-only); tool schemas not counted",
			state.Context.OverheadTokens, systemOnly)
	}
}

// TestContextStage_ToolOverhead_BuildFilteredToolsError_FallsBackToSystemOnly verifies
// that a BuildFilteredTools error is silently swallowed and overhead = system only.
func TestContextStage_ToolOverhead_BuildFilteredToolsError_FallsBackToSystemOnly(t *testing.T) {
	t.Parallel()

	counter := tokencount.NewFallbackCounter()

	deps := &PipelineDeps{
		TokenCounter: counter,
		BuildMessages: func(_ context.Context, _ *RunInput, _ []providers.Message, _ string) ([]providers.Message, error) {
			return []providers.Message{
				{Role: "system", Content: "You are a capable AI assistant."},
			}, nil
		},
		BuildFilteredTools: func(_ *RunState) ([]providers.ToolDefinition, error) {
			return nil, context.DeadlineExceeded // simulate error
		},
	}

	stage := NewContextStage(deps)
	state := defaultState()

	// Should not return an error even though BuildFilteredTools failed.
	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	// state.Think.Tools should remain nil/empty.
	if len(state.Think.Tools) != 0 {
		t.Errorf("state.Think.Tools len = %d, want 0 on BuildFilteredTools error", len(state.Think.Tools))
	}

	// Overhead = system only (no tool penalty).
	sysMsg := providers.Message{Role: "system", Content: "You are a capable AI assistant."}
	wantOverhead := counter.CountMessages("claude-3", []providers.Message{sysMsg})
	if state.Context.OverheadTokens != wantOverhead {
		t.Errorf("OverheadTokens = %d, want %d (system-only on tool-build error)",
			state.Context.OverheadTokens, wantOverhead)
	}
}
