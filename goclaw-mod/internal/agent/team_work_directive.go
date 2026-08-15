package agent

import (
	"fmt"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

type TeamWorkDirective struct {
	Mode            string
	Source          string
	Reason          string
	OriginalMessage string
	RequiredTool    string
	WorkflowHint    string
}

func (d *TeamWorkDirective) normalizedRequiredTool() string {
	if d == nil {
		return ""
	}
	if strings.TrimSpace(d.RequiredTool) != "" {
		return strings.TrimSpace(d.RequiredTool)
	}
	switch strings.TrimSpace(d.Mode) {
	case "team":
		return "team_tasks"
	case "delegate":
		return "delegate"
	default:
		return ""
	}
}

func buildTeamWorkDirectivePrompt(d *TeamWorkDirective) string {
	tool := d.normalizedRequiredTool()
	if tool == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("## TEAM WORK ROUTING LOCK\n")
	b.WriteString("This turn has been classified by the system as requiring the team/delegate workflow.\n")
	b.WriteString("You must not complete the requested work by yourself before using the required workflow tool.\n")
	b.WriteString("Required tool: `")
	b.WriteString(tool)
	b.WriteString("`.\n")
	if d.Mode != "" {
		b.WriteString("Workflow mode: ")
		b.WriteString(d.Mode)
		b.WriteString(".\n")
	}
	if d.Reason != "" {
		b.WriteString("Routing reason: ")
		b.WriteString(d.Reason)
		b.WriteString(".\n")
	}
	if d.WorkflowHint != "" {
		b.WriteString("Workflow hint: ")
		b.WriteString(d.WorkflowHint)
		b.WriteString("\n")
	}
	b.WriteString("If the required workflow tool is unavailable or cannot be used, explain that blocker instead of presenting the work as complete.")
	return b.String()
}

func teamWorkDirectiveNeedsRetry(d *TeamWorkDirective, iteration int, resp *providers.ChatResponse) bool {
	tool := d.normalizedRequiredTool()
	if tool == "" || iteration != 0 || resp == nil {
		return false
	}
	for _, tc := range resp.ToolCalls {
		if tc.Name == tool {
			return false
		}
	}
	return true
}

func buildTeamWorkDirectiveRetryRequest(req providers.ChatRequest, d *TeamWorkDirective) providers.ChatRequest {
	retry := req
	retry.Options = make(map[string]any, len(req.Options)+1)
	for k, v := range req.Options {
		retry.Options[k] = v
	}
	retry.Options[providers.OptToolChoice] = "required"
	retry.Messages = append(append([]providers.Message{}, req.Messages...), providers.Message{
		Role:    "system",
		Content: fmt.Sprintf("The system routing lock requires the `%s` workflow tool in this turn. Call that tool now before giving any final answer. If it is impossible, explain the blocker.", d.normalizedRequiredTool()),
	})
	return retry
}

func teamWorkDirectiveBlocker(d *TeamWorkDirective) string {
	tool := d.normalizedRequiredTool()
	if tool == "" {
		return "Hệ thống đã yêu cầu quy trình team/delegate nhưng không xác định được công cụ cần dùng, nên lượt này chưa thể coi là hoàn tất."
	}
	return fmt.Sprintf("Hệ thống đã yêu cầu quy trình team/delegate cho lượt này, nhưng model không gọi công cụ `%s` như bắt buộc. Vì vậy hệ thống chưa thể coi nhiệm vụ đã được chuyển đúng workflow.", tool)
}
