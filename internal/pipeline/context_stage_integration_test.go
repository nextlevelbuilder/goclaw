package pipeline

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/tokencount"
)

// buildRealisticToolDefinitions returns n ToolDefinitions with ~3KB JSON each,
// matching the trace-019dab16 scenario (agent with 10 realistic tools).
// Each tool has 8 uniquely-named parameters with long descriptions to reach ~3KB.
func buildRealisticToolDefinitions(n int) []providers.ToolDefinition {
	// Long description filler (~200 chars) repeated per property.
	descFiller := "This parameter controls an important aspect of the tool behaviour. " +
		"Provide a valid value according to the schema constraints documented above. "

	paramNames := []string{
		"source_file_path", "destination_path", "encoding_format",
		"compression_level", "output_template", "max_retry_count",
		"timeout_seconds", "verbose_logging",
	}

	tools := make([]providers.ToolDefinition, n)
	for i := range tools {
		properties := map[string]any{}
		required := make([]string, 0, 2)
		for j, name := range paramNames {
			properties[name] = map[string]any{
				"type":        "string",
				"description": strings.Repeat(descFiller, 2),
			}
			if j < 2 {
				required = append(required, name)
			}
		}
		tools[i] = providers.ToolDefinition{
			Type: "function",
			Function: providers.ToolFunctionSchema{
				Name: "realistic_tool",
				Description: strings.Repeat(
					"A realistic tool that performs complex file and system operations. "+
						"It accepts multiple parameters and returns structured JSON output. "+
						"Use this tool when you need to process, transform, or analyse data. ",
					4,
				),
				Parameters: map[string]any{
					"type":       "object",
					"properties": properties,
					"required":   required,
				},
			},
		}
	}
	return tools
}

// TestContextStage_Integration_ToolOverhead_RealCounter verifies the end-to-end
// composition of Phase 03 (CountToolSchemas) with the real FallbackCounter:
//  1. state.Think.Tools is populated (len == numTools).
//  2. OverheadTokens > system-prompt-only count (tools add non-zero overhead).
//  3. OverheadTokens > 5000 when 10 tools each ~3KB JSON are provided.
//
// Uses real tokencount.FallbackCounter (no spy) for deterministic non-zero counts.
func TestContextStage_Integration_ToolOverhead_RealCounter(t *testing.T) {
	t.Parallel()

	const numTools = 10
	counter := tokencount.NewFallbackCounter()

	// Build system prompt ~1500 chars.
	systemPrompt := strings.Repeat(
		"You are a capable AI assistant with access to many tools. "+
			"Use them wisely to help the user accomplish their goals. ",
		10,
	)

	fixture := buildRealisticToolDefinitions(numTools)

	// Sanity: verify fixtures are actually ~3KB each.
	toolJSON, _ := json.Marshal(fixture[0])
	if len(toolJSON) < 1000 {
		t.Logf("WARNING: single tool JSON only %d bytes; fixture may be smaller than expected", len(toolJSON))
	}

	deps := &PipelineDeps{
		TokenCounter: counter,
		BuildMessages: func(_ context.Context, _ *RunInput, _ []providers.Message, _ string) ([]providers.Message, error) {
			return []providers.Message{
				{Role: "system", Content: systemPrompt},
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

	// Assert 1: Tools populated.
	if len(state.Think.Tools) != numTools {
		t.Errorf("state.Think.Tools len = %d, want %d", len(state.Think.Tools), numTools)
	}

	// Compute system-only overhead for comparison.
	sysMsg := providers.Message{Role: "system", Content: systemPrompt}
	systemOnly := counter.CountMessages("claude-3", []providers.Message{sysMsg})

	// Assert 2: OverheadTokens strictly greater than system-only (tools counted).
	if state.Context.OverheadTokens <= systemOnly {
		t.Errorf("OverheadTokens = %d, want > %d (system-only=%d); tool schemas not contributing",
			state.Context.OverheadTokens, systemOnly, systemOnly)
	}

	// Assert 3: OverheadTokens > 5000 (system ~500 + 10 tools × 3KB JSON ÷ 3 ≈ 10000+).
	const wantMinOverhead = 5000
	if state.Context.OverheadTokens <= wantMinOverhead {
		t.Errorf("OverheadTokens = %d, want > %d; 10 tools with ~3KB JSON each should contribute significantly",
			state.Context.OverheadTokens, wantMinOverhead)
	}

	t.Logf("observed: systemOnly=%d, overhead=%d, tools=%d", systemOnly, state.Context.OverheadTokens, numTools)
}
