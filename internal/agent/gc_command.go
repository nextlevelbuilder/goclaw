package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/commands/gc"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// applyGCCommand intercepts /gc: slash commands in the agent loop and transforms
// them into skill-guided runs. It mirrors applySkillSlashCommand: when the
// dispatcher resolves a /gc:<command>, the remaining message becomes the skill
// input, the skill content + executor guidance is appended to the system prompt,
// and the skill filter is narrowed to the resolved skill slug.
//
// Passthrough is critical: when gcDispatcher is nil or the message is not a
// /gc: command, the inputs are returned untouched.
func (l *Loop) applyGCCommand(ctx context.Context, req *RunRequest, message, extraPrompt string, skillFilter []string) (string, string, []string) {
	if l.gcDispatcher == nil {
		return message, extraPrompt, skillFilter
	}
	d, ok := l.gcDispatcher.Resolve(ctx, message)
	if !ok || d == nil {
		return message, extraPrompt, skillFilter
	}
	extraPrompt = appendExtraPrompt(extraPrompt, gcSystemPromptSection(d))
	if strings.TrimSpace(d.Remaining) == "" {
		message = fmt.Sprintf("Execute the activated %s skill per its workflow.", d.Skill)
	} else {
		message = d.Remaining
	}
	skillFilter = []string{d.Skill}
	l.recordSkillSlashUsageEvent(ctx, d.Skill)
	l.recordSkillUsage(ctx, req, d.Skill, "", "slash", store.SkillUsageStatusStarted, "", 0)
	return message, extraPrompt, skillFilter
}

// gcSystemPromptSection builds the explicit activation directive for a resolved
// /gc: command, mirroring the skill slash command's activation section.
func gcSystemPromptSection(d *gc.Dispatch) string {
	var b strings.Builder
	b.WriteString("## Explicit Command Activation\n\n")
	fmt.Fprintf(&b, "The user invoked a `/gc:` command. Execute the `%s` skill per its workflow for the current request and treat the remaining user message as the skill input.\n\n", d.Skill)
	if content := strings.TrimSpace(d.Content); content != "" {
		b.WriteString(content)
		b.WriteString("\n")
	}
	if len(d.Flags) > 0 {
		b.WriteString("\nExecution flags: " + strings.Join(d.Flags, " "))
	}
	return b.String()
}