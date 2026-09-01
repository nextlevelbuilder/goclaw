package tools

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// UseSkillTool is a marker tool for observability.
// It generates tool.call / tool.result events in spans and realtime
// so skill activation is visible in tracing. For agents that have read_file,
// the actual skill content is still loaded via read_file — this tool stays a
// no-op for them. Agents without read_file in their resolved tool set (see
// store.AvailableToolNamesFromContext) have no way to follow up, so this tool
// inlines the skill content directly instead.
type UseSkillTool struct {
	loader *skills.Loader
}

func NewUseSkillTool(loader *skills.Loader) *UseSkillTool { return &UseSkillTool{loader: loader} }

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

func (t *UseSkillTool) Execute(ctx context.Context, args map[string]any) *Result {
	name, _ := args["name"].(string)
	if name == "" {
		return ErrorResult("name parameter is required")
	}

	slog.Info("skill.activated", "skill", name)

	// nil AvailableToolNames means "no restriction known" (see
	// store.AvailableToolNamesFromContext) — only inline when we can positively
	// confirm read_file is missing from this agent's resolved tool set.
	if available := store.AvailableToolNamesFromContext(ctx); available != nil && !available["read_file"] {
		content, ok := t.loader.LoadSkill(ctx, name)
		if !ok {
			return ErrorResult(fmt.Sprintf("skill %q not found", name))
		}
		return NewResult(content)
	}

	return NewResult(fmt.Sprintf("Skill %q activated. Proceed to read the skill's SKILL.md with read_file.", name))
}
