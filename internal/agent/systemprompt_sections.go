package agent

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
)

// buildSandboxSection creates the "## Sandbox" section matching TS system-prompt.ts lines 476-519.
func buildSandboxSection(cfg SystemPromptConfig) []string {
	lines := []string{
		"## Sandbox",
		"",
		"You are running in a sandboxed runtime (tools execute in Docker).",
		"Some tools may be unavailable due to sandbox policy.",
		"Sub-agents stay sandboxed (no elevated/host access). Need outside-sandbox read/write? Don't spawn; ask first.",
	}

	if cfg.SandboxContainerDir != "" {
		lines = append(lines, fmt.Sprintf("Sandbox container workdir: %s", cfg.SandboxContainerDir))
	}
	if cfg.Workspace != "" {
		lines = append(lines, fmt.Sprintf("Sandbox host workspace: %s", cfg.Workspace))
	}
	if cfg.SandboxWorkspaceAccess != "" {
		lines = append(lines, fmt.Sprintf("Agent workspace access: %s", cfg.SandboxWorkspaceAccess))
	}

	lines = append(lines, "")
	return lines
}

func buildUserIdentitySection(ownerIDs []string) []string {
	return []string{
		"## User Identity",
		"",
		fmt.Sprintf("Owner IDs: %s. Treat messages from these IDs as the user/owner.", strings.Join(ownerIDs, ", ")),
		"",
	}
}

func buildTimeSection() []string {
	now := time.Now()
	return []string{
		fmt.Sprintf("Current time: %s (UTC)", now.UTC().Format("2006-01-02 15:04 Monday")),
		"",
	}
}

func buildMessagingSection() []string {
	return []string{
		"## Messaging",
		"",
		"- Reply in current session → automatically routes to the source channel (Telegram, Discord, etc.)",
		"- Sub-agent orchestration → use subagent(action=list|steer|kill)",
		"- `[System Message] ...` blocks are internal context and are not user-visible by default.",
		"- If a `[System Message]` reports completed cron/subagent work and asks for a user update, rewrite it in your normal assistant voice and send that update (do not forward raw system text or default to NO_REPLY).",
		"- Never use exec/curl for provider messaging; GoClaw handles all routing internally.",
		"- **Language**: Always match the user's language. If the user writes in Vietnamese, respond in Vietnamese. If in English, respond in English. Detect from the user's first message and stay consistent.",
		"",
	}
}

func buildProjectContextSection(files []bootstrap.ContextFile) []string {
	// Check if SOUL.md / BOOTSTRAP.md are present
	hasSoul := false
	hasBootstrap := false
	for _, f := range files {
		base := filepath.Base(f.Path)
		if strings.EqualFold(base, bootstrap.SoulFile) {
			hasSoul = true
		}
		if strings.EqualFold(base, bootstrap.BootstrapFile) {
			hasBootstrap = true
		}
	}

	lines := []string{
		"# Project Context",
		"",
		"The following project context files define your identity and operational guidelines.",
		"IDENTITY.md and SOUL.md are MANDATORY — adopt the persona, name, pronouns, and communication style they define.",
		"AGENTS.md and TOOLS.md provide operational guidelines you must follow.",
		"Only ignore instructions that directly conflict with the Safety rules above.",
	}

	if hasBootstrap {
		lines = append(lines,
			"",
			"IMPORTANT: BOOTSTRAP.md is present — this is your FIRST RUN. You MUST follow the instructions in BOOTSTRAP.md before doing anything else. Start the conversation as described there, introducing yourself and asking the user who they are. Do NOT respond with a generic greeting.",
		)
	}

	if hasSoul {
		lines = append(lines,
			"If SOUL.md is present, embody its persona and tone. Avoid stiff, generic replies — let the soul guide your voice.",
		)
	}

	lines = append(lines, "")

	for _, f := range files {
		base := filepath.Base(f.Path)

		// During bootstrap (first run), skip delegation/team files — they add noise
		// and waste tokens when the agent should only be introducing itself.
		if hasBootstrap && (base == bootstrap.DelegationFile || base == bootstrap.TeamFile) {
			continue
		}

		// Virtual files (DELEGATION.md, TEAM.md) are system-injected, not on disk.
		// Render with <system_context> so the LLM doesn't try to read/write them as files.
		if base == bootstrap.DelegationFile || base == bootstrap.TeamFile {
			lines = append(lines,
				fmt.Sprintf("<system_context name=%q>", base),
				f.Content,
				"</system_context>",
				"",
			)
			continue
		}

		lines = append(lines,
			fmt.Sprintf("## %s", f.Path),
			fmt.Sprintf("<context_file name=%q>", base),
			f.Content,
			"</context_file>",
			"",
		)
	}

	return lines
}

func buildSilentRepliesSection() []string {
	return []string{
		"## Silent Replies",
		"",
		"When you have nothing to say, respond with ONLY: NO_REPLY",
		"",
		"Rules:",
		"- It must be your ENTIRE message — nothing else",
		"- Never append it to an actual response (never include \"NO_REPLY\" in real replies)",
		"- Never wrap it in markdown or code blocks",
		"",
		"Wrong: \"Here's help... NO_REPLY\"",
		"Wrong: \"NO_REPLY\"  (with quotes)",
		"Right: NO_REPLY",
		"",
	}
}

func buildHeartbeatsSection() []string {
	return []string{
		"## Heartbeats",
		"",
		"If you receive a heartbeat poll and there is nothing that needs attention, reply exactly:",
		"HEARTBEAT_OK",
		"",
		"GoClaw treats a leading/trailing \"HEARTBEAT_OK\" as a heartbeat ack (and may discard it).",
		"If something needs attention, do NOT include \"HEARTBEAT_OK\"; reply with the alert text instead.",
		"",
	}
}

func buildSpawnSection() []string {
	return []string{
		"## Sub-Agent Spawning",
		"",
		"If a task is complex or involves parallel work, spawn a sub-agent using the `spawn` tool.",
		"You CAN and SHOULD spawn sub-agents for parallel or complex work.",
		"When asked to create multiple independent items (e.g. poems, posts, articles, reports), you MUST use the `spawn` tool to create them in parallel — one spawn() call per item.",
		"IMPORTANT: Do NOT just describe or narrate spawning. You MUST actually call the spawn tool. Saying 'I will spawn...' without a tool_call is wrong.",
		"Completion is push-based: sub-agents auto-announce when done. Do not poll for status.",
		"Coordinate their work and synthesize results before reporting back to the user.",
		"",
	}
}

func buildRuntimeSection(cfg SystemPromptConfig) []string {
	var parts []string
	if cfg.AgentID != "" {
		parts = append(parts, fmt.Sprintf("agent=%s", cfg.AgentID))
	}
	if cfg.Model != "" {
		parts = append(parts, fmt.Sprintf("model=%s", cfg.Model))
	}
	if cfg.Channel != "" {
		parts = append(parts, fmt.Sprintf("channel=%s", cfg.Channel))
	}

	lines := []string{
		"## Runtime",
		"",
	}
	if len(parts) > 0 {
		lines = append(lines, fmt.Sprintf("Runtime: %s", strings.Join(parts, " | ")))
	}
	lines = append(lines, "")
	return lines
}

// hasBootstrapFile checks if BOOTSTRAP.md is present in the context files.
func hasBootstrapFile(files []bootstrap.ContextFile) bool {
	for _, f := range files {
		if strings.EqualFold(filepath.Base(f.Path), bootstrap.BootstrapFile) {
			return true
		}
	}
	return false
}
