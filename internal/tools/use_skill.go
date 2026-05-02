package tools

import (
	"context"
	"fmt"
	"log/slog"
)

type skillLoader interface {
	LoadSkill(context.Context, string) (string, bool)
}

// UseSkillTool activates a skill and returns its instructions directly.
// Returning the body matters for sandboxed wake sessions: read_file may be
// restricted to a per-session workspace while skills live in shared roots.
type UseSkillTool struct {
	loader skillLoader
}

func NewUseSkillTool(loader ...skillLoader) *UseSkillTool {
	t := &UseSkillTool{}
	if len(loader) > 0 {
		t.loader = loader[0]
	}
	return t
}

func (t *UseSkillTool) Name() string { return "use_skill" }

func (t *UseSkillTool) Description() string {
	return "Activate a skill and return its instructions."
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

func (t *UseSkillTool) Execute(ctx context.Context, args map[string]any) *Result {
	name, _ := args["name"].(string)
	if name == "" {
		return ErrorResult("name parameter is required")
	}

	slog.Info("skill.activated", "skill", name)

	if t.loader == nil {
		return NewResult(fmt.Sprintf("Skill %q activated. Proceed to read the skill's SKILL.md with read_file.", name))
	}
	content, ok := t.loader.LoadSkill(ctx, name)
	if !ok {
		return ErrorResult(fmt.Sprintf("skill %q not found", name))
	}
	return NewResult(fmt.Sprintf("Skill %q activated. Follow these instructions:\n\n%s", name, content))
}
