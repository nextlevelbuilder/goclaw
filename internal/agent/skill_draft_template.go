package agent

import "fmt"

// GenerateSkillDraft creates a SKILL.md template from repeated tool usage data.
// Produces a skeleton that admin can edit before activation via evolution approval.
func GenerateSkillDraft(toolName string, callCount int, successRate float64) string {
	return fmt.Sprintf(`---
name: %s-patterns
description: Skill auto-generated from repeated %s tool usage (%d calls/week, %.0f%% success)
---

# %s Usage Patterns

Auto-generated from tool metrics. Edit before activating.

## When to Use

Describe scenarios where this tool pattern should be applied automatically.

## Instructions

Provide specific instructions for using %s effectively based on observed patterns.

## Constraints

List any constraints or guardrails for this tool usage.
`, toolName, toolName, callCount, successRate*100, toolName, toolName)
}
