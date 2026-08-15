package cmd

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/teamworkclassify"
)

type teamWorkGateOutcome struct {
	Message   string
	Directive *agent.TeamWorkDirective
}

func applyTeamWorkGateForInbound(ctx context.Context, deps *ConsumerDeps, msg bus.InboundMessage, sessionKey, agentKey, peerKind string, agentUUID uuid.UUID, skillFilter []string, provider providers.Provider, model string) teamWorkGateOutcome {
	out := teamWorkGateOutcome{Message: msg.Content}
	if deps == nil || deps.Cfg == nil || deps.Cfg.Gateway.TeamWorkClassify == nil || !*deps.Cfg.Gateway.TeamWorkClassify {
		return out
	}
	if deps.TeamWorkEmbedder == nil {
		slog.Info("team_work_classify: skipped; embedding unavailable", "agent", agentKey, "session", sessionKey)
		return out
	}
	if agentUUID == uuid.Nil {
		return out
	}
	mode := agent.ResolveOrchestrationMode(ctx, agentUUID, deps.TeamStore, deps.AgentLinkStore)
	if mode == agent.ModeSpawn {
		slog.Info("team_work_classify: skipped; no team/delegate capability", "agent", agentKey, "session", sessionKey)
		return out
	}
	if msg.Metadata["run_kind"] != "" || msg.Metadata["delegation_id"] != "" || msg.Metadata["subagent_id"] != "" || bus.IsInternalSender(msg.SenderID) {
		return out
	}

	input := teamworkclassify.BuildInputFromStores(ctx, teamworkclassify.ProfileStores{
		Agents:     deps.AgentStore,
		Teams:      deps.TeamStore,
		AgentLinks: deps.AgentLinkStore,
	}, teamworkclassify.BuildInputOptions{
		Mode:        teamworkclassify.Mode(mode),
		Message:     msg.Content,
		AgentID:     agentUUID,
		ToolAllow:   msg.ToolAllow,
		SkillFilter: skillFilter,
		Embedder:    deps.TeamWorkEmbedder,
	})
	result := teamworkclassify.ClassifyWithLLM(ctx, input, provider, model, deps.UsageCaps)
	slog.Info("team_work_classify: decision", "agent", agentKey, "session", sessionKey, "mode", mode, "decision", result.Decision, "self_score", result.SelfScore, "collaboration_score", result.CollaborationScore, "reason", result.Reason)
	if result.Decision == teamworkclassify.DecisionTeam {
		out.Directive = &agent.TeamWorkDirective{
			Mode:            string(result.Mode),
			Source:          "llm",
			Reason:          result.Reason,
			OriginalMessage: msg.Content,
			RequiredTool:    result.RequiredTool,
			WorkflowHint:    result.WorkflowHint,
		}
	}
	return out
}
