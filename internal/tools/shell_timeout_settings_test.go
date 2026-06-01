package tools

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestExecToolEffectiveTimeoutFromSettings(t *testing.T) {
	tool := NewExecTool(t.TempDir(), false)

	cases := []struct {
		name string
		ctx  context.Context
		want time.Duration
	}{
		{
			name: "global exec timeout",
			ctx: WithBuiltinToolSettings(context.Background(), BuiltinToolSettings{
				"exec": []byte(`{"timeout_seconds":120}`),
			}),
			want: 120 * time.Second,
		},
		{
			name: "tenant overrides global",
			ctx: WithTenantToolSettings(
				WithBuiltinToolSettings(context.Background(), BuiltinToolSettings{
					"exec": []byte(`{"timeout_seconds":120}`),
				}),
				BuiltinToolSettings{"exec": []byte(`{"timeout_seconds":30}`)},
			),
			want: 30 * time.Second,
		},
		{
			name: "excessive timeout clamps",
			ctx: WithBuiltinToolSettings(context.Background(), BuiltinToolSettings{
				"exec": []byte(`{"timeout_seconds":999999}`),
			}),
			want: 3600 * time.Second,
		},
		{
			name: "invalid json falls back",
			ctx: WithBuiltinToolSettings(context.Background(), BuiltinToolSettings{
				"exec": []byte(`{`),
			}),
			want: 60 * time.Second,
		},
		{
			name: "missing timeout falls back",
			ctx: WithBuiltinToolSettings(context.Background(), BuiltinToolSettings{
				"exec": []byte(`{"other":true}`),
			}),
			want: 60 * time.Second,
		},
		{
			name: "zero timeout falls back",
			ctx: WithBuiltinToolSettings(context.Background(), BuiltinToolSettings{
				"exec": []byte(`{"timeout_seconds":0}`),
			}),
			want: 60 * time.Second,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tool.effectiveTimeout(tc.ctx); got != tc.want {
				t.Fatalf("effectiveTimeout() = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestExecToolExecuteUsesConfiguredHostTimeout(t *testing.T) {
	tool := NewExecTool(t.TempDir(), false)
	ctx := WithBuiltinToolSettings(context.Background(), BuiltinToolSettings{
		"exec": []byte(`{"timeout_seconds":1}`),
	})

	result := tool.Execute(ctx, map[string]any{
		"command": "sleep 2",
	})

	if !result.IsError {
		t.Fatalf("expected timeout error, got ForLLM=%q", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "command timed out after 1s") {
		t.Fatalf("expected configured timeout in error, got %q", result.ForLLM)
	}
}
