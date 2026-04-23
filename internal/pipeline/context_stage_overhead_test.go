package pipeline

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// spyTokenCounter records all CountMessages invocations for assertion.
type spyTokenCounter struct {
	calls      [][]providers.Message // each element is the msgs slice from one CountMessages call
	toolCounts int                   // number of CountToolSchemas calls
	fixed      int                   // tokens returned per CountMessages call
	toolFixed  int                   // tokens returned per CountToolSchemas call
}

func (s *spyTokenCounter) Count(_ string, _ string) int { return s.fixed }
func (s *spyTokenCounter) CountMessages(_ string, msgs []providers.Message) int {
	// Deep-copy the slice so later mutations don't affect recorded state.
	cp := make([]providers.Message, len(msgs))
	copy(cp, msgs)
	s.calls = append(s.calls, cp)
	return len(msgs) * s.fixed
}
func (s *spyTokenCounter) CountToolSchemas(_ string, tools []providers.ToolDefinition) int {
	s.toolCounts++
	return len(tools) * s.toolFixed
}
func (s *spyTokenCounter) ModelContextWindow(_ string) int { return 200_000 }

// fixtureTools returns a slice of n minimal ToolDefinitions for testing.
func fixtureTools(n int) []providers.ToolDefinition {
	tools := make([]providers.ToolDefinition, n)
	for i := range tools {
		tools[i] = providers.ToolDefinition{
			Type: "function",
			Function: &providers.ToolFunctionSchema{
				Name:        "tool_fixture",
				Description: "A fixture tool for testing overhead calculation.",
				Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
			},
		}
	}
	return tools
}

// TestContextStage_OverheadSystemPlusTools_PostFix verifies the POST-fix overhead
// calculation: OverheadTokens = system-message tokens + tool-schema tokens.
// Both CountMessages and CountToolSchemas are called exactly once.
func TestContextStage_OverheadSystemPlusTools_PostFix(t *testing.T) {
	t.Parallel()

	const systemFixed = 100
	const toolFixed = 50
	const numTools = 5

	spy := &spyTokenCounter{fixed: systemFixed, toolFixed: toolFixed}
	fixture := fixtureTools(numTools)

	deps := &PipelineDeps{
		TokenCounter: spy,
		// BuildMessages seeds a system message so the counter has content.
		BuildMessages: func(_ context.Context, _ *RunInput, _ []providers.Message, _ string) ([]providers.Message, error) {
			return []providers.Message{
				{Role: "system", Content: "You are a helpful assistant with many capabilities."},
			}, nil
		},
		// BuildFilteredTools returns fixture tools so ContextStage can count them.
		BuildFilteredTools: func(_ *RunState) ([]providers.ToolDefinition, error) {
			return fixture, nil
		},
	}

	stage := NewContextStage(deps)
	state := defaultState()

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	// POST-fix: OverheadTokens = system (1 msg × 100) + tools (5 × 50) = 350.
	wantOverhead := systemFixed + numTools*toolFixed
	if state.Context.OverheadTokens != wantOverhead {
		t.Errorf("OverheadTokens = %d, want %d (system=%d + tools=%d)",
			state.Context.OverheadTokens, wantOverhead, systemFixed, numTools*toolFixed)
	}

	// Assert: exactly 1 call to CountMessages (system msg).
	if len(spy.calls) != 1 {
		t.Errorf("CountMessages called %d time(s), want exactly 1", len(spy.calls))
	}

	// Assert: CountToolSchemas called exactly once.
	if spy.toolCounts != 1 {
		t.Errorf("CountToolSchemas called %d time(s), want exactly 1", spy.toolCounts)
	}

	// Assert: state.Think.Tools populated by ContextStage.
	if len(state.Think.Tools) != numTools {
		t.Errorf("state.Think.Tools len = %d, want %d", len(state.Think.Tools), numTools)
	}
}

// TestContextStage_OverheadSystemOnly_NoToolsCallback verifies that when
// BuildFilteredTools is nil, OverheadTokens = system tokens only (no panic).
// CountToolSchemas IS called with a nil slice (returns 0) — that's correct behavior.
func TestContextStage_OverheadSystemOnly_NoToolsCallback(t *testing.T) {
	t.Parallel()

	spy := &spyTokenCounter{fixed: 100, toolFixed: 0}

	deps := &PipelineDeps{
		TokenCounter: spy,
		BuildMessages: func(_ context.Context, _ *RunInput, _ []providers.Message, _ string) ([]providers.Message, error) {
			return []providers.Message{
				{Role: "system", Content: "You are a helpful assistant with many capabilities."},
			}, nil
		},
		// BuildFilteredTools intentionally nil.
	}

	stage := NewContextStage(deps)
	state := defaultState()

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	// No tools → overhead = system only (1 msg × 100 = 100).
	// CountToolSchemas(nil) = 0, so overhead is unchanged.
	wantOverhead := 100
	if state.Context.OverheadTokens != wantOverhead {
		t.Errorf("OverheadTokens = %d, want %d", state.Context.OverheadTokens, wantOverhead)
	}
}

// roleList returns a slice of role strings for error messages.
func roleList(msgs []providers.Message) []string {
	roles := make([]string, len(msgs))
	for i, m := range msgs {
		roles[i] = m.Role
	}
	return roles
}
