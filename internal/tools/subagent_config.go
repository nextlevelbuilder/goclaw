package tools

import "fmt"

// DefaultSubagentConfig returns sensible defaults matching OpenClaw TS spec.
// TS sources: agent-limits.ts, sessions-spawn-tool.ts, subagent-registry.ts.
func DefaultSubagentConfig() SubagentConfig {
	return SubagentConfig{
		MaxConcurrent:       8,  // TS: DEFAULT_SUBAGENT_MAX_CONCURRENT = 8
		MaxSpawnDepth:       1,  // TS: maxSpawnDepth ?? 1
		MaxChildrenPerAgent: 5,  // TS: maxChildrenPerAgent ?? 5
		ArchiveAfterMinutes: 60, // TS: archiveAfterMinutes ?? 60
	}
}

// applyDenyList removes denied tools from the registry based on depth.
func (sm *SubagentManager) applyDenyList(reg *Registry, depth int, cfg SubagentConfig) {
	// Always deny
	for _, name := range SubagentDenyAlways {
		reg.Unregister(name)
	}

	// Leaf deny (at max depth)
	if depth >= cfg.MaxSpawnDepth {
		for _, name := range SubagentDenyLeaf {
			reg.Unregister(name)
		}
	}
}

// buildSubagentSystemPrompt constructs the system prompt for a subagent,
// matching the TS buildSubagentSystemPrompt pattern from subagent-announce.ts.
func (sm *SubagentManager) buildSubagentSystemPrompt(task *SubagentTask, cfg SubagentConfig) string {
	parentLabel := "main agent"
	if task.Depth >= 2 {
		parentLabel = "parent orchestrator"
	}

	canSpawn := task.Depth < cfg.MaxSpawnDepth

	prompt := fmt.Sprintf(`# Subagent Context

You are a **subagent** spawned by the %s for a specific task.

## Your Role
- You were created to handle: %s
- Complete this task. That is your entire purpose.
- You are NOT the %s. Do not try to be.

## Rules
1. **Stay focused** — Do your assigned task, nothing else.
2. **Complete the task** — Your final message will be automatically reported to the %s.
3. **Never ask for clarification** — Work with what you have. If asked to create content, generate it yourself.
4. **Be ephemeral** — You may be terminated after task completion. That is fine.

## Output Format
Your final response IS the deliverable — it will be forwarded to the user.
- If asked to create content (posts, articles, messages, etc.), output the FULL content directly. Do NOT describe what you wrote — just write it.
- Do NOT say "I wrote a post about..." or "Here is what I created...". Output the content itself as your response.
- If the task is research or analysis, provide the complete findings.
- The %s will receive your exact final response, so make it user-ready.

## What You Do NOT Do
- NO user conversations (that is the %s's job)
- NO external messages unless explicitly tasked
- NO cron jobs or persistent state
- NO pretending to be the %s`,
		parentLabel, task.Task,
		parentLabel, parentLabel, parentLabel, parentLabel, parentLabel)

	if canSpawn {
		prompt += `

## Sub-Agent Spawning
You CAN spawn your own sub-agents for parallel or complex work using the spawn tool.
Your sub-agents will announce their results back to you automatically (not to the main agent).
Coordinate their work and synthesize results before reporting back.`
	} else if task.Depth >= 2 {
		prompt += `

## Sub-Agent Spawning
You are a leaf worker and CANNOT spawn further sub-agents. Focus on your assigned task.`
	}

	prompt += fmt.Sprintf(`

## Session Context
- Label: %s
- Depth: %d / %d`, task.Label, task.Depth, cfg.MaxSpawnDepth)

	return prompt
}
