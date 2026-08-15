package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/config"
)

func TestWaitToolValidation(t *testing.T) {
	t.Parallel()
	tool := NewWaitTool()

	tests := []struct {
		name string
		args map[string]any
		want string
	}{
		{name: "missing", args: map[string]any{}, want: "timeMs is required"},
		{name: "below minimum", args: map[string]any{"timeMs": 99}, want: "at least 100ms"},
		{name: "above maximum", args: map[string]any{"timeMs": 300001}, want: "at most 300000ms"},
		{name: "fractional", args: map[string]any{"timeMs": 100.5}, want: "integer"},
		{name: "string", args: map[string]any{"timeMs": "100"}, want: "integer"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tool.Execute(context.Background(), tt.args)
			if got == nil || !got.IsError || !strings.Contains(got.ForLLM, tt.want) {
				t.Fatalf("Execute() = %#v, want error containing %q", got, tt.want)
			}
		})
	}
}

func TestWaitToolSuccess(t *testing.T) {
	t.Parallel()
	tool := NewWaitTool()

	start := time.Now()
	got := tool.Execute(context.Background(), map[string]any{
		"timeMs": json.Number("100"),
		"reason": "rate limit spacing",
	})
	if got == nil || got.IsError {
		t.Fatalf("Execute() error = %#v", got)
	}
	if elapsed := time.Since(start); elapsed < 90*time.Millisecond {
		t.Fatalf("wait returned too early after %s", elapsed)
	}
	if !strings.Contains(got.ForLLM, "Waited 100ms") || !strings.Contains(got.ForLLM, "rate limit spacing") {
		t.Fatalf("ForLLM = %q", got.ForLLM)
	}
}

func TestWaitToolContextCancellation(t *testing.T) {
	t.Parallel()
	tool := NewWaitTool()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	got := tool.Execute(ctx, map[string]any{"timeMs": 300000})
	if got == nil || !got.IsError || !strings.Contains(got.ForLLM, "wait cancelled") {
		t.Fatalf("Execute() = %#v, want cancellation error", got)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("cancelled wait took %s", elapsed)
	}
}

func TestWaitToolPerAgentBounds(t *testing.T) {
	t.Parallel()
	tool := NewWaitTool()
	ctx := WithWaitToolConfig(context.Background(), &config.WaitToolPolicy{MinMs: 250, MaxMs: 500})

	if got := tool.Execute(ctx, map[string]any{"timeMs": 200}); got == nil || !got.IsError || !strings.Contains(got.ForLLM, "at least 250ms") {
		t.Fatalf("below custom min = %#v", got)
	}
	if got := tool.Execute(ctx, map[string]any{"timeMs": 600}); got == nil || !got.IsError || !strings.Contains(got.ForLLM, "at most 500ms") {
		t.Fatalf("above custom max = %#v", got)
	}
}

func TestInferMetadataWaitReadOnly(t *testing.T) {
	t.Parallel()
	meta := inferMetadata("wait")
	if !meta.HasCapability(CapReadOnly) || meta.HasCapability(CapMutating) {
		t.Fatalf("wait metadata = %#v, want read-only and not mutating", meta)
	}
}
