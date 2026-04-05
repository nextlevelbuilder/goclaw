package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// buildMergedSubagentAnnounce creates the announce message for one or more subagent results.
func buildMergedSubagentAnnounce(entries []subagentAnnounceEntry, roster tools.SubagentRoster) string {
	var sb strings.Builder

	if len(entries) == 1 {
		e := entries[0]
		statusLabel := "completed successfully"
		if e.Status == "failed" {
			statusLabel = "failed"
		} else if e.Status == "cancelled" {
			statusLabel = "was cancelled"
		}
		fmt.Fprintf(&sb, "[System Message] A subagent task %q just %s.\n\nResult:\n%s\n\nStats: runtime %s, iterations %d, tokens %d in / %d out\n",
			e.Label, statusLabel, e.Content,
			e.Runtime.Round(time.Millisecond), e.Iterations,
			e.InputTokens, e.OutputTokens)
	} else {
		completed, failed := 0, 0
		for _, e := range entries {
			switch e.Status {
			case "completed":
				completed++
			case "failed":
				failed++
			}
		}
		if failed > 0 && completed > 0 {
			fmt.Fprintf(&sb, "[System Message] %d subagent task(s) completed, %d failed.\n", completed, failed)
		} else if failed > 0 {
			fmt.Fprintf(&sb, "[System Message] %d subagent task(s) failed.\n", failed)
		} else {
			fmt.Fprintf(&sb, "[System Message] %d subagent tasks completed.\n", completed)
		}
		for i, e := range entries {
			fmt.Fprintf(&sb, "\n--- Task #%d: %q (%s, %s, tokens %d/%d) ---\nResult: %s\n",
				i+1, e.Label, e.Status,
				e.Runtime.Round(time.Millisecond),
				e.InputTokens, e.OutputTokens, e.Content)
		}
	}

	// Append roster for context.
	sb.WriteString("\n")
	sb.WriteString(tools.BuildReplyInstruction(roster))

	// Smart prompting based on remaining tasks.
	running := 0
	for _, e := range roster.Entries {
		if e.Status == tools.TaskStatusRunning {
			running++
		}
	}
	if running == 0 {
		sb.WriteString("\n\nAll subagent tasks finished. Present a comprehensive summary of ALL results to the user.")
	} else {
		sb.WriteString("\n\nSome subagents are still running. Briefly acknowledge this result (1-2 sentences). A full summary will come when all tasks complete.")
	}

	return sb.String()
}
