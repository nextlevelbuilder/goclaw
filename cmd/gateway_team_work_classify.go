package cmd

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/providerresolve"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/teamworkclassify"
	"github.com/nextlevelbuilder/goclaw/internal/teamworkconfig"
)

type teamWorkGateOutcome struct {
	Message         string
	Directive       *agent.TeamWorkDirective
	DisableTeamWork bool
	BlockedTools    []string
	AuditID         uuid.UUID
}

// resolveInboundTeamWorkSettings returns the per-tenant Team Work classifier
// settings for the request tenant at the inbound ingress. When no resolver is
// wired (unit wiring), it falls back to deps.Cfg's file-config values so behavior
// matches the pre-isolation shared-cfg read.
func resolveInboundTeamWorkSettings(ctx context.Context, deps *ConsumerDeps) teamworkconfig.Settings {
	if deps.TeamWorkCfg != nil {
		return deps.TeamWorkCfg.Resolve(ctx)
	}
	if deps.Cfg == nil {
		return teamworkconfig.Settings{}
	}
	s := teamworkconfig.Settings{
		ClassifierProvider: deps.Cfg.Gateway.TeamWorkClassifyProvider,
		ClassifierModel:    deps.Cfg.Gateway.TeamWorkClassifyModel,
	}
	if deps.Cfg.Gateway.TeamWorkClassify != nil {
		s.Enabled = *deps.Cfg.Gateway.TeamWorkClassify
		s.EnabledSet = true
	}
	return s
}

func applyTeamWorkGateForInbound(ctx context.Context, deps *ConsumerDeps, msg bus.InboundMessage, sessionKey, agentKey, peerKind string, agentUUID uuid.UUID, skillFilter []string, provider providers.Provider, model, runID string) teamWorkGateOutcome {
	out := teamWorkGateOutcome{Message: msg.Content}
	if deps == nil {
		return out
	}
	twSettings := resolveInboundTeamWorkSettings(ctx, deps)
	if !twSettings.ClassifyEnabled() {
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
		Agents:            deps.AgentStore,
		Teams:             deps.TeamStore,
		AgentLinks:        deps.AgentLinkStore,
		PinnedSkills:      deps.SkillsLoader,
		MCP:               deps.MCPStore,
		BuiltinTools:      deps.BuiltinToolStore,
		TenantToolConfigs: deps.TenantToolStore,
		ToolPolicy:        deps.ToolPolicy,
		ToolRegistry:      deps.ToolRegistry,
	}, teamworkclassify.BuildInputOptions{
		Mode:           teamworkclassify.Mode(mode),
		Message:        msg.Content,
		RecentMessages: recentMessagesForTeamWorkGate(ctx, deps, sessionKey),
		AgentID:        agentUUID,
		ToolAllow:      msg.ToolAllow,
		SkillFilter:    skillFilter,
		Embedder:       deps.TeamWorkEmbedder,
		Timeout:        twSettings.ClassifyTimeout,
	})
	selection := providerresolve.ResolveTeamWorkClassifier(ctx, deps.ProviderReg, twSettings.ClassifierProvider, twSettings.ClassifierModel, provider, model)
	if selection.Warning != "" {
		slog.Warn("team_work_classify: classifier provider override fallback", "agent", agentKey, "session", sessionKey, "warning", selection.Warning, "source", selection.Source)
	} else if selection.Source != "agent_default" {
		slog.Info("team_work_classify: classifier provider selected", "agent", agentKey, "session", sessionKey, "classifier_provider", selection.ProviderName, "classifier_model", selection.Model, "source", selection.Source)
	}
	result := teamworkclassify.ClassifyWithLLM(ctx, input, selection.Provider, selection.Model, deps.UsageCaps)
	agentUUIDForAudit := agentUUID
	decision, auditID := agent.BuildAuditedTeamWorkGateDecision(ctx, deps.TeamStore, result, input, teamworkclassify.ClassificationAuditInput{
		Ingress:            store.TeamWorkIngressInbound,
		RunID:              runID,
		SessionKey:         sessionKey,
		AgentID:            &agentUUIDForAudit,
		OriginalMessage:    msg.Content,
		ClassifierProvider: selection.ProviderName,
		ClassifierModel:    selection.Model,
	})
	if decision.PlanFreezeError != nil {
		slog.Warn("team_work_classify: failed to freeze validated plan; failing closed to self", "agent", agentKey, "session", sessionKey, "error", decision.PlanFreezeError)
	}
	out.Directive = decision.Directive
	out.DisableTeamWork = decision.DisableTeamWork
	out.BlockedTools = decision.BlockedTools
	out.AuditID = auditID
	slog.Info("team_work_classify: decision",
		"agent", agentKey,
		"session", sessionKey,
		"mode", mode,
		"decision", result.Decision,
		"intent_relation", result.IntentRelation,
		"workflow_mode", result.WorkflowMode,
		"review_required", result.EffectiveReviewRequired,
		"owner", result.BestTeamOwner,
		"workflow_step_count", func() int {
			if result.Plan == nil {
				return 0
			}
			return len(result.Plan.Steps)
		}(),
		"revised", result.PlannerRepaired,
		"disable_team_work", decision.DisableTeamWork,
		"degraded_reason", result.DegradedReasonCode,
		"staffing_gaps", result.StaffingGaps,
		"validation_reason", result.PlannerValidationReason)
	return out
}

func recentMessagesForTeamWorkGate(ctx context.Context, deps *ConsumerDeps, sessionKey string) []providers.Message {
	if deps == nil || deps.SessStore == nil || sessionKey == "" {
		return nil
	}
	return deps.SessStore.GetHistory(ctx, sessionKey)
}
