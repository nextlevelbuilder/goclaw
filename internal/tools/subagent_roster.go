package tools

import (
	"cmp"
	"slices"
)

// SubagentRosterEntry summarizes one subagent task for the roster.
type SubagentRosterEntry struct {
	Label  string
	Status string // "running", "completed", "failed", "cancelled"
}

// SubagentRoster holds the full roster of subagent tasks for a parent,
// including per-agent config limits for deterministic LLM context.
type SubagentRoster struct {
	Entries     []SubagentRosterEntry
	Total       int // total tasks for this parent
	MaxPerAgent int // from spawnConfig.MaxChildrenPerAgent
}

// RosterForParent returns the full roster of tasks for a parent.
// Sorted: completed/failed/cancelled first, then running (deterministic output).
func (sm *SubagentManager) RosterForParent(parentID string) SubagentRoster {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var entries []SubagentRosterEntry
	maxPerAgent := 0
	for _, t := range sm.tasks {
		if t.ParentID != parentID {
			continue
		}
		entries = append(entries, SubagentRosterEntry{
			Label:  t.Label,
			Status: t.Status,
		})
		if maxPerAgent == 0 {
			maxPerAgent = t.spawnConfig.MaxChildrenPerAgent
		}
	}

	// Sort: non-running first (completed/failed/cancelled), then running.
	// Within same group, sort alphabetically by label for determinism.
	slices.SortFunc(entries, func(a, b SubagentRosterEntry) int {
		aRunning := a.Status == TaskStatusRunning
		bRunning := b.Status == TaskStatusRunning
		if aRunning != bRunning {
			if aRunning {
				return 1 // running goes last
			}
			return -1
		}
		return cmp.Compare(a.Label, b.Label)
	})

	return SubagentRoster{
		Entries:     entries,
		Total:       len(entries),
		MaxPerAgent: maxPerAgent,
	}
}
