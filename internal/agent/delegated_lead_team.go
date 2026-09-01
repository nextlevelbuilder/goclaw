package agent

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// delegatedLeadTeamRead is the read-only view of its own team granted to a team
// lead reached through delegate: workspace is the directory that team's tasks
// resolve to, root is the team directory above it — the one that makes files
// written by members under a different chat scope reachable.
//
// Both are allowances only. Neither may become the active workspace: in a
// delegation artifact run that stays the exchange outputs directory, and
// displacing it breaks the exchange.
type delegatedLeadTeamRead struct {
	workspace string
	root      string
}

func (r delegatedLeadTeamRead) ok() bool { return r.workspace != "" && r.root != "" }

// resolveDelegatedLeadTeamRead returns the team paths a delegated lead may read,
// or the zero value when no unambiguous team answers.
//
// A team member run carries its team workspace in dispatch metadata and a lead
// addressed directly resolves one below; a delegated lead had neither, so it
// could not read its own team's files nor stage them into the delegation outputs
// directory — the team's deliverables were unreachable to the only agent able to
// hand them back (#1535).
//
// The paths mirror what injectContext resolves for a directly addressed lead and
// what team_tasks_create stores on every task the lead creates; those must agree
// or the lead is handed a sibling directory. l.dataDir is already tenant-scoped
// (resolver.go: config.TenantDataDir), so TenantLayer must NOT be reapplied here.
//
// Ambiguity is deliberately not resolved: an agent leading several active teams
// gets nothing rather than a guess, since the delegation does not say which team
// it concerns. store.GetTeamForAgent would answer in one call, but it orders by
// lead-ness and takes LIMIT 1 — it would silently pick one team, which is exactly
// the choice this must not make on its own.
func (l *Loop) resolveDelegatedLeadTeamRead(ctx context.Context, req *RunRequest) delegatedLeadTeamRead {
	if l.teamStore == nil || l.agentUUID == uuid.Nil || l.dataDir == "" {
		return delegatedLeadTeamRead{}
	}
	teams, err := l.teamStore.ListTeams(ctx)
	if err != nil {
		slog.Warn("delegate: cannot resolve lead team read access",
			"agent_id", l.agentUUID, "error", err)
		return delegatedLeadTeamRead{}
	}
	var lead *store.TeamData
	for i := range teams {
		if teams[i].LeadAgentID != l.agentUUID || teams[i].Status != store.TeamStatusActive {
			continue
		}
		if lead != nil {
			slog.Debug("delegate: agent leads several active teams, team read access not granted",
				"agent_id", l.agentUUID)
			return delegatedLeadTeamRead{}
		}
		lead = &teams[i]
	}
	if lead == nil {
		return delegatedLeadTeamRead{}
	}

	// Origin chat, not the delivery channel: team_tasks_create derives the same
	// path from OriginChatIDFromCtx, which prefers the workspace chat ID.
	chatID := req.WorkspaceChatID
	if chatID == "" {
		chatID = req.ChatID
	}
	return delegatedLeadTeamRead{
		workspace: tools.ResolveWorkspace(l.dataDir,
			tools.TeamLayer(lead.ID),
			tools.UserChatLayer(chatID, tools.IsSharedWorkspace(lead.Settings)),
		),
		root: tools.ResolveWorkspace(l.dataDir, tools.TeamLayer(lead.ID)),
	}
}
