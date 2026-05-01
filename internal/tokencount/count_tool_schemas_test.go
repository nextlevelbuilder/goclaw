package tokencount_test

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/tokencount"
)

const testModel = "claude-sonnet-4-5-20250929"

// smallTool returns a minimal ToolDefinition with a short description.
func smallTool() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: &providers.ToolFunctionSchema{
			Name:        "get_time",
			Description: "Returns the current UTC time.",
			Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
		},
	}
}

// largeTool returns a ToolDefinition with a longer description and parameters.
func largeTool(name string) providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: &providers.ToolFunctionSchema{
			Name: name,
			Description: "Reads, writes, and appends content to files in the workspace. " +
				"Supports binary and text modes. Path must be relative to the active workspace root. " +
				"Returns byte count on success. Errors on path traversal attempts.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":    map[string]any{"type": "string", "description": "Relative file path"},
					"content": map[string]any{"type": "string", "description": "Content to write"},
					"mode":    map[string]any{"type": "string", "enum": []string{"read", "write", "append"}},
				},
				"required": []string{"path", "mode"},
			},
		},
	}
}

// fiveLargeTools returns 5 distinct large tool definitions.
func fiveLargeTools() []providers.ToolDefinition {
	names := []string{"write_file", "read_file", "exec_command", "web_search", "create_image"}
	tools := make([]providers.ToolDefinition, len(names))
	for i, n := range names {
		tools[i] = largeTool(n)
	}
	return tools
}

func TestCountToolSchemas_NilSlice_ReturnsZero(t *testing.T) {
	t.Parallel()
	tc := tokencount.NewTiktokenCounter()
	fc := tokencount.NewFallbackCounter()

	if got := tc.CountToolSchemas(testModel, nil); got != 0 {
		t.Errorf("tiktokenCounter.CountToolSchemas(nil) = %d, want 0", got)
	}
	if got := fc.CountToolSchemas(testModel, nil); got != 0 {
		t.Errorf("FallbackCounter.CountToolSchemas(nil) = %d, want 0", got)
	}
}

func TestCountToolSchemas_EmptySlice_ReturnsZero(t *testing.T) {
	t.Parallel()
	tc := tokencount.NewTiktokenCounter()
	fc := tokencount.NewFallbackCounter()

	if got := tc.CountToolSchemas(testModel, []providers.ToolDefinition{}); got != 0 {
		t.Errorf("tiktokenCounter.CountToolSchemas([]) = %d, want 0", got)
	}
	if got := fc.CountToolSchemas(testModel, []providers.ToolDefinition{}); got != 0 {
		t.Errorf("FallbackCounter.CountToolSchemas([]) = %d, want 0", got)
	}
}

func TestCountToolSchemas_OneSmallTool_PositiveCount(t *testing.T) {
	t.Parallel()
	tools := []providers.ToolDefinition{smallTool()}
	tc := tokencount.NewTiktokenCounter()
	fc := tokencount.NewFallbackCounter()

	if got := tc.CountToolSchemas(testModel, tools); got <= 0 {
		t.Errorf("tiktokenCounter.CountToolSchemas(1 small tool) = %d, want > 0", got)
	}
	if got := fc.CountToolSchemas(testModel, tools); got <= 0 {
		t.Errorf("FallbackCounter.CountToolSchemas(1 small tool) = %d, want > 0", got)
	}
}

func TestCountToolSchemas_FiveLargeToolsGtOneSmall(t *testing.T) {
	t.Parallel()
	one := []providers.ToolDefinition{smallTool()}
	five := fiveLargeTools()

	tc := tokencount.NewTiktokenCounter()
	fc := tokencount.NewFallbackCounter()

	tcOne := tc.CountToolSchemas(testModel, one)
	tcFive := tc.CountToolSchemas(testModel, five)
	if tcFive <= tcOne {
		t.Errorf("tiktokenCounter: 5 large tools (%d) should produce more tokens than 1 small tool (%d)", tcFive, tcOne)
	}

	fcOne := fc.CountToolSchemas(testModel, one)
	fcFive := fc.CountToolSchemas(testModel, five)
	if fcFive <= fcOne {
		t.Errorf("FallbackCounter: 5 large tools (%d) should produce more tokens than 1 small tool (%d)", fcFive, fcOne)
	}
}

func TestCountToolSchemas_FallbackModel_UsesRuneHeuristic(t *testing.T) {
	t.Parallel()
	// Unknown model forces tiktoken to use FallbackCounter path.
	const unknownModel = "unknown-model-xyz"
	tools := fiveLargeTools()

	tc := tokencount.NewTiktokenCounter()
	fc := tokencount.NewFallbackCounter()

	tcCount := tc.CountToolSchemas(unknownModel, tools)
	fcCount := fc.CountToolSchemas(unknownModel, tools)

	// Both should return same value since tiktoken falls back to FallbackCounter.
	if tcCount != fcCount {
		t.Errorf("unknown model: tiktokenCounter(%d) != FallbackCounter(%d), expected same fallback path", tcCount, fcCount)
	}
	if tcCount <= 0 {
		t.Errorf("unknown model: CountToolSchemas = %d, want > 0", tcCount)
	}
}
