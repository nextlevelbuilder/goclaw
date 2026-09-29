package scheduler

import (
	"errors"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
)

func TestRunOutcomeFailure(t *testing.T) {
	runErr := errors.New("provider down")
	tests := []struct {
		name    string
		outcome RunOutcome
		want    string
	}{
		{name: "success", outcome: RunOutcome{Result: &agent.RunResult{Content: "answer"}}, want: ""},
		{name: "run error", outcome: RunOutcome{Err: runErr}, want: "provider down"},
		{name: "pipeline stop", outcome: RunOutcome{Result: &agent.RunResult{Content: "notice", StopReason: "final request context budget exceeded"}}, want: "final request context budget exceeded"},
		{name: "error wins over stop", outcome: RunOutcome{Err: runErr, Result: &agent.RunResult{StopReason: "stop"}}, want: "provider down"},
		{name: "nil result", outcome: RunOutcome{}, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.outcome.Failure()
			if tt.want == "" {
				if got != nil {
					t.Fatalf("Failure() = %v, want nil", got)
				}
				return
			}
			if got == nil || got.Error() != tt.want {
				t.Fatalf("Failure() = %v, want %q", got, tt.want)
			}
		})
	}
}
