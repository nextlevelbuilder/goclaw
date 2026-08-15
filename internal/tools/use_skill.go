package tools

import (
	"context"
	"fmt"
	"log/slog"
)

// UseSkillTool is a marker tool for observability.
// It generates tool.call / tool.result events in spans and realtime
// so skill activation is visible in tracing. The actual skill content
// is still loaded via read_file — this tool is a deliberate no-op.
type UseSkillTool struct{}

func NewUseSkillTool() *UseSkillTool { return &UseSkillTool{} }

func (t *UseSkillTool) Name() string { return "use_skill" }

func (t *UseSkillTool) Description() string {
	return "Activate a skill. Call this before read_file to signal skill usage for tracing and observability."
}

func (t *UseSkillTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "Skill name or slug to activate",
			},
			"params": map[string]any{
				"type":        "object",
				"description": "Optional skill-specific parameters",
			},
		},
		"required": []string{"name"},
	}
}

func (t *UseSkillTool) Execute(_ context.Context, args map[string]any) *Result {
	name, _ := args["name"].(string)
	if name == "" {
		return ErrorResult("name parameter is required")
	}

	slog.Info("skill.activated", "skill", name)

	return NewResult(fmt.Sprintf("Skill %q activated. Proceed to read the skill's SKILL.md with read_file.", name))
}
