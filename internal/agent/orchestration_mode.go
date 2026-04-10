package agent

import (
	"context"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// OrchestrationMode controls which inter-agent tools are available.
type OrchestrationMode string

const (
	// ModeSpawn: self-clone only. spawn tool available.
	ModeSpawn OrchestrationMode = "spawn"

	// ModeDelegate: agent links + spawn. delegate tool available.
	ModeDelegate OrchestrationMode = "delegate"

	// ModeTeam: full team tasks + delegate + spawn.
	ModeTeam OrchestrationMode = "team"
)

// ResolveOrchestrationMode determines the orchestration mode for an agent
// based on team membership and delegate links.
// Priority: team > delegate > spawn.
func ResolveOrchestrationMode(ctx context.Context, agentID uuid.UUID, teamStore store.TeamStore, linkStore store.AgentLinkStore) OrchestrationMode {
	// Check team membership first (highest priority)
	if teamStore != nil {
		if team, err := teamStore.GetTeamForAgent(ctx, agentID); err == nil && team != nil {
			return ModeTeam
		}
	}

	// Check delegate links
	if linkStore != nil {
		if targets, err := linkStore.DelegateTargets(ctx, agentID); err == nil && len(targets) > 0 {
			return ModeDelegate
		}
	}

	return ModeSpawn
}

// orchModeDenyTools returns tool names to hide for a given orchestration mode.
// spawn: hide delegate + team_tasks. delegate: hide team_tasks. team: hide nothing.
func orchModeDenyTools(mode OrchestrationMode) map[string]bool {
	switch mode {
	case ModeSpawn:
		return map[string]bool{"delegate": true, "team_tasks": true}
	case ModeDelegate:
		return map[string]bool{"team_tasks": true}
	default:
		return nil
	}
}

// OrchestrationSectionData for system prompt template.
type OrchestrationSectionData struct {
	Mode            OrchestrationMode
	DelegateTargets []DelegateTargetEntry
	TeamContext     *TeamSectionData // only if ModeTeam
}

// DelegateTargetEntry is a single delegate target for prompt injection.
type DelegateTargetEntry struct {
	AgentKey    string
	DisplayName string
	Description string
}
