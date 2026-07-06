package agent

import (
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

func TestTeamWorkDirectivePromptRequiresWorkflowTool(t *testing.T) {
	prompt := buildTeamWorkDirectivePrompt(&TeamWorkDirective{
		Mode:            "team",
		Source:          "llm",
		Reason:          "requires strategy and content members",
		OriginalMessage: "lập kế hoạch chiến dịch",
		RequiredTool:    "team_tasks",
		WorkflowHint:    `Use team_tasks(action="create", task_type="request") because this agent is a member requesting help.`,
	})
	for _, want := range []string{"TEAM WORK ROUTING LOCK", "team_tasks", "must not complete", "requires strategy and content members", `task_type="request"`} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestTeamWorkDirectiveRetriesWhenRequiredToolMissing(t *testing.T) {
	directive := &TeamWorkDirective{Mode: "team", RequiredTool: "team_tasks"}
	if !teamWorkDirectiveNeedsRetry(directive, 0, &providers.ChatResponse{Content: "em tự làm xong rồi"}) {
		t.Fatal("text-only first response should require retry")
	}
	if teamWorkDirectiveNeedsRetry(directive, 1, &providers.ChatResponse{Content: "em tự làm xong rồi"}) {
		t.Fatal("second iteration should not retry workflow directive")
	}
	if teamWorkDirectiveNeedsRetry(directive, 0, &providers.ChatResponse{ToolCalls: []providers.ToolCall{{Name: "team_tasks"}}}) {
		t.Fatal("response with required tool should not retry")
	}
}

func TestTeamWorkDirectiveRetryRequestRequiresToolChoice(t *testing.T) {
	directive := &TeamWorkDirective{Mode: "delegate", RequiredTool: "delegate"}
	req := providers.ChatRequest{
		Messages: []providers.Message{{Role: "user", Content: "làm việc này theo link"}},
		Options:  map[string]any{"existing": true},
	}
	retry := buildTeamWorkDirectiveRetryRequest(req, directive)
	if retry.Options[providers.OptToolChoice] != "required" {
		t.Fatalf("tool_choice = %v, want required", retry.Options[providers.OptToolChoice])
	}
	if retry.Options["existing"] != true {
		t.Fatalf("existing option was not preserved: %+v", retry.Options)
	}
	if len(retry.Messages) != 2 || !strings.Contains(retry.Messages[1].Content, "delegate") {
		t.Fatalf("retry messages not reinforced: %+v", retry.Messages)
	}
}
