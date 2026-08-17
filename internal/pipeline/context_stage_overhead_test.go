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

// contentCounter counts tokens from content length (fallback-style chars/4), so
// appending the memory section to the system prompt changes the count — unlike
// spyTokenCounter, which is per-message and would mask the reorder regression.
// It also records the exact system content it counted so the test can prove the
// counter observed the post-mutation system prompt.
type contentCounter struct {
	systemSeen string // system content observed at count time
	toolFixed  int    // tokens per tool schema
	toolsSeen  int    // number of tool schemas observed
}

func (c *contentCounter) Count(_ string, text string) int { return len(text)/4 + 1 }
func (c *contentCounter) CountMessages(_ string, msgs []providers.Message) int {
	total := 0
	for _, m := range msgs {
		total += len(m.Content)/4 + 1
		if m.Role == "system" {
			c.systemSeen = m.Content
		}
	}
	return total
}
func (c *contentCounter) CountToolSchemas(_ string, tools []providers.ToolDefinition) int {
	c.toolsSeen = len(tools)
	return len(tools) * c.toolFixed
}
func (c *contentCounter) ModelContextWindow(_ string) int { return 200_000 }

// contentTokens approximates what contentCounter.CountMessages would return for
// the given content string (the /4+1 heuristic used above).
func contentTokens(s string) int { return len(s)/4 + 1 }

// TestContextStage_OverheadIncludesMemoryAndReminders verifies the reordered
// overhead computation (Gap A): OverheadTokens is counted AFTER InjectReminders
// and AutoInject, so the L0 memory section appended to the system prompt is
// included in the fixed (overhead) pool rather than leaking into the history
// budget, and reminders are present in the history PruneStage counts.
func TestContextStage_OverheadIncludesMemoryAndReminders(t *testing.T) {
	t.Parallel()

	cc := &contentCounter{}
	const bareSystem = "You are a helpful assistant with many capabilities."
	const memorySection = "## Memory Context\n\nRelevant memories:\n- user prefers Go\n- user lives in Hanoi\n"

	deps := &PipelineDeps{
		TokenCounter: cc,
		BuildMessages: func(_ context.Context, _ *RunInput, _ []providers.Message, _ string) ([]providers.Message, error) {
			return []providers.Message{
				{Role: "system", Content: bareSystem},
				{Role: "user", Content: "Hello"},
			}, nil
		},
		InjectReminders: func(_ context.Context, _ *RunInput, msgs []providers.Message) []providers.Message {
			// Prepend a reminder as a user message so its tokens land in history.
			reminder := providers.Message{Role: "user", Content: "Reminder: team task T-1 is due today."}
			return append([]providers.Message{reminder}, msgs...)
		},
		AutoInject: func(_ context.Context, _, _, _ string) (string, error) {
			return memorySection, nil
		},
	}

	stage := NewContextStage(deps)
	state := defaultState()
	state.Input.SessionKey = "sess-a"
	state.Input.Message = "What should I build next?"

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	if state.Context.MemorySection == "" {
		t.Fatalf("MemorySection empty after Execute with AutoInject configured")
	}

	// OverheadTokens must reflect the FINAL system prompt: bare system content
	// plus the appended memory section. Counted after AutoInject, the overhead
	// equals contentTokens(bareSystem + "\n\n" + memorySection) + tool schemas (0).
	expectSystem := bareSystem + "\n\n" + memorySection
	want := contentTokens(expectSystem)
	if state.Context.OverheadTokens != want {
		t.Errorf("OverheadTokens = %d, want %d (final system prompt incl. memory section)",
			state.Context.OverheadTokens, want)
	}

	// The bare system content alone must be strictly smaller — proving the memory
	// section is included in the count.
	if want <= contentTokens(bareSystem) {
		t.Fatalf("test fixture invalid: memory section must add tokens (bare=%d, with-memory=%d)",
			contentTokens(bareSystem), want)
	}

	// Reminder must be present in history after injection.
	hist := state.Messages.History()
	if len(hist) < 2 || hist[0].Role != "user" || hist[0].Content != "Reminder: team task T-1 is due today." {
		t.Errorf("reminder not injected correctly: history[0] = %#v", hist[0])
	}
}
