package pipeline

import (
	"fmt"
	"strings"
	"sync"
)

type ConstraintStore struct {
	mu                 sync.RWMutex
	entries            map[string]Constraint
	consecutiveBlocked int
	lastBlockedTool    string
}

func NewConstraintStore() *ConstraintStore {
	return &ConstraintStore{entries: make(map[string]Constraint)}
}

func (cs *ConstraintStore) Add(c Constraint) bool {
	if cs == nil {
		return false
	}
	normalized := c.Normalize(c.AddedAt)
	key := normalized.Key()

	cs.mu.Lock()
	defer cs.mu.Unlock()

	if existing, ok := cs.entries[key]; ok {
		if severityRank(normalized.Severity) < severityRank(existing.Severity) {
			return false
		}
		if existing == normalized {
			return false
		}
	}

	cs.entries[key] = normalized
	return true
}

func (cs *ConstraintStore) Active() []Constraint {
	if cs == nil {
		return nil
	}
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	out := make([]Constraint, 0, len(cs.entries))
	for _, entry := range cs.entries {
		out = append(out, entry)
	}
	sortConstraints(out)
	return out
}

func (cs *ConstraintStore) Check(toolName string, args map[string]any) (bool, *Constraint) {
	if cs == nil {
		return false, nil
	}

	cs.mu.RLock()
	defer cs.mu.RUnlock()

	for _, constraint := range cs.entries {
		if constraint.Severity != SeverityHard {
			continue
		}
		if matchesToolCall(constraint, toolName, args) {
			copy := constraint
			return true, &copy
		}
	}
	return false, nil
}

func (cs *ConstraintStore) RecordBlocked(toolName string) int {
	if cs == nil {
		return 0
	}
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if toolName != "" && toolName != cs.lastBlockedTool {
		cs.consecutiveBlocked++
		cs.lastBlockedTool = toolName
	}
	if toolName == "" {
		cs.consecutiveBlocked++
	}
	return cs.consecutiveBlocked
}

func (cs *ConstraintStore) RecordAllowed() {
	if cs == nil {
		return
	}
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.consecutiveBlocked = 0
	cs.lastBlockedTool = ""
}

func (cs *ConstraintStore) ConsecutiveBlocked() int {
	if cs == nil {
		return 0
	}
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.consecutiveBlocked
}

func (cs *ConstraintStore) ClearNonSticky() {
	if cs == nil {
		return
	}
	cs.mu.Lock()
	defer cs.mu.Unlock()
	for key, entry := range cs.entries {
		if !entry.Sticky {
			delete(cs.entries, key)
		}
	}
}

func (cs *ConstraintStore) ForSystemPrompt() string {
	active := cs.Active()
	if len(active) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("[Active runtime constraints]\n")
	sb.WriteString("Do not retry blocked paths. Choose an alternative approach or answer from gathered evidence.\n")
	for _, constraint := range active {
		icon := "WARN"
		if constraint.Severity == SeverityHard {
			icon = "BLOCK"
		}
		sb.WriteString(fmt.Sprintf("- %s %s on %s: %s\n", icon, constraint.Kind, constraint.Subject, constraint.Message))
		if constraint.Resolution == ResolutionHumanRequired {
			sb.WriteString("  Requires human action to resolve.\n")
		}
	}
	return strings.TrimSpace(sb.String())
}

func (cs *ConstraintStore) ForTrace() []map[string]any {
	active := cs.Active()
	if len(active) == 0 {
		return nil
	}

	out := make([]map[string]any, 0, len(active))
	for _, constraint := range active {
		out = append(out, map[string]any{
			"kind":       constraint.Kind,
			"subject":    constraint.Subject,
			"message":    constraint.Message,
			"severity":   constraint.Severity,
			"resolution": constraint.Resolution,
			"sticky":     constraint.Sticky,
			"added_at":   constraint.AddedAt,
		})
	}
	return out
}
