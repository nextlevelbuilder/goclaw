package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/edition"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// buildMessages constructs the full message list for an LLM request.
// Returns the messages and whether BOOTSTRAP.md was present in context files
// (used by the caller for auto-cleanup without an extra DB roundtrip).
func (l *Loop) buildMessages(ctx context.Context, history []providers.Message, summary, userMessage, extraSystemPrompt, sessionKey, channel, channelType, chatTitle, chatID, peerKind, userID string, historyLimit int, skillFilter []string, lightContext bool) ([]providers.Message, bool) {
	var messages []providers.Message

	// Build system prompt — 3-layer mode resolution: runtime > auto-detect > config
	mode := resolvePromptMode("", sessionKey, l.promptMode)

	_, hasSpawn := l.tools.Get("spawn")
	_, hasTeamTools := l.tools.Get("team_tasks")
	_, hasSkillSearch := l.tools.Get("skill_search")
	_, hasSkillManage := l.tools.Get("skill_manage")
	_, hasMCPToolSearch := l.tools.Get("mcp_tool_search")
	_, hasKG := l.tools.Get("knowledge_graph_search")
	_, hasMemoryExpand := l.tools.Get("memory_expand")

	// Per-user workspace: show the user's subdirectory in the system prompt.
	// Uses cached workspace from userSetups (includes channel isolation).
	// When workspace sharing is enabled, show the base workspace without user subfolder.
	promptWorkspace := l.workspace
	if l.agentUUID != uuid.Nil && userID != "" && l.workspace != "" {
		shared := l.shouldShareWorkspace(userID, peerKind)
		baseWs := l.workspace
		if val, ok := l.userSetups.Load(userID); ok {
			if ws := val.(*userSetup).workspace; ws != "" {
				baseWs = ws
			}
		}
		promptWorkspace = tools.ResolveWorkspace(baseWs,
			tools.UserChatLayer(tools.SanitizePathSegment(userID), shared),
		)
	}

	// Resolve context files once — also detect BOOTSTRAP.md presence.
	// lightContext: skip loading context files, only inject ExtraSystemPrompt (heartbeat checklist).
	var contextFiles []bootstrap.ContextFile
	if !lightContext {
		contextFiles = l.resolveContextFiles(ctx, userID)

		// Fallback: if DB seeding failed (e.g. SQLITE_BUSY) but we have
		// in-memory embedded templates, merge them so the first turn still
		// gets bootstrap onboarding. Only applies when DB returned no user files.
		if val, ok := l.userSetups.Load(userID); ok {
			if fb := val.(*userSetup).fallbackBootstrap; len(fb) > 0 {
				contextFiles = l.mergeContextFallback(contextFiles, fb)
				// Clear after first use — next turn should read from DB.
				val.(*userSetup).fallbackBootstrap = nil
			}
		}
	}
	hadBootstrap := false
	for _, cf := range contextFiles {
		if cf.Path == bootstrap.BootstrapFile {
			hadBootstrap = true
			break
		}
	}

	// Bootstrap mode: only direct user DMs need onboarding.
	// System sessions (group, team, subagent, cron, heartbeat) skip bootstrap
	// to prevent the model from getting distracted by onboarding instructions.
	isSystemSession := peerKind == "group" ||
		bootstrap.IsTeamSession(sessionKey) ||
		bootstrap.IsSubagentSession(sessionKey) ||
		bootstrap.IsCronSession(sessionKey) ||
		bootstrap.IsHeartbeatSession(sessionKey)
	if hadBootstrap && isSystemSession {
		filtered := make([]bootstrap.ContextFile, 0, len(contextFiles))
		for _, cf := range contextFiles {
			if cf.Path != bootstrap.BootstrapFile {
				filtered = append(filtered, cf)
			}
		}
		contextFiles = filtered
		hadBootstrap = false
	}

	// Bootstrap auto-contact: inject known sender info from channel metadata.
	// DM only — group chats have permission checks and multiple senders.
	if hadBootstrap && peerKind == "direct" {
		if senderName := store.SenderNameFromContext(ctx); senderName != "" {
			hint := fmt.Sprintf("Known user info (from %s): Name=%q\nTimezone: not yet known. When the user mentions times, schedules, or reminders, ask for their timezone and update USER.md.", channelType, senderName)
			if extraSystemPrompt != "" {
				extraSystemPrompt += "\n\n"
			}
			extraSystemPrompt += hint
		}
	}

	// Group writer restrictions: filter context files + inject prompt
	if l.configPermStore != nil && (strings.HasPrefix(userID, "group:") || strings.HasPrefix(userID, "guild:")) {
		senderID := store.SenderIDFromContext(ctx)
		writerPrompt, filtered := l.buildGroupWriterPrompt(ctx, userID, senderID, contextFiles)
		contextFiles = filtered
		if writerPrompt != "" {
			if extraSystemPrompt != "" {
				extraSystemPrompt += "\n\n"
			}
			extraSystemPrompt += writerPrompt
		}
	}

	// Build tool list, filtering out skill_manage when skill_evolve is off.
	// Also applies ChannelAware filtering so channel-specific tools don't
	// appear in ## Tooling when the current channel doesn't support them.
	toolNames := l.filteredToolNamesForChannel(channelType)
	if !l.skillEvolve {
		filtered := toolNames[:0:0]
		for _, n := range toolNames {
			if n != "skill_manage" {
				filtered = append(filtered, n)
			}
		}
		toolNames = filtered
	}
	// Exclude tool aliases from the system prompt tool list.
	// Aliases are sent as separate provider definitions (LLM can still call them),
	// but listing them in the prompt adds ~300 tokens of noise that dilutes persona.
	if l.tools != nil {
		aliasSet := l.tools.Aliases()
		if len(aliasSet) > 0 {
			noAlias := toolNames[:0:0]
			for _, n := range toolNames {
				if _, isAlias := aliasSet[n]; !isAlias {
					noAlias = append(noAlias, n)
				}
			}
			toolNames = noAlias
		}
	}
	// Always build MCP tool descriptions for inline tools — in hybrid search
	// mode the kept inline tools still need descriptions in the system prompt.
	mcpToolDescs := l.buildMCPToolDescs(toolNames)

	// Bootstrap DM mode: only restrict tools for open agents (identity being created).
	// Predefined agents keep full capabilities — BOOTSTRAP.md guides behavior.
	if hadBootstrap && l.agentType != store.AgentTypePredefined {
		toolNames = filterBootstrapTools(toolNames)
		mcpToolDescs = nil
	}

	// Determine whether to inject team context into the system prompt.
	// Team context (TEAM.md, workspace section, members roster) is injected when:
	//   - This is a team-dispatched session (team: prefix), OR
	//   - Agent is the lead of a team AND this is an inbound (non-dispatch) session.
	// Member-only agents in inbound chat get spawn section instead of team context.
	isTeamDispatch := bootstrap.IsTeamSession(sessionKey)
	injectTeamContext := isTeamDispatch || (hasTeamTools && l.isTeamLead)

	// Filter TEAM.md from context files when team context should not be injected
	// (i.e. member-only agent in inbound chat — spawn section applies instead).
	if !injectTeamContext {
		filtered := make([]bootstrap.ContextFile, 0, len(contextFiles))
		for _, cf := range contextFiles {
			if cf.Path != bootstrap.TeamFile {
				filtered = append(filtered, cf)
			}
		}
		contextFiles = filtered
	}

	// Mode-aware context file filtering: each mode loads different files.
	if allowlist := bootstrap.ModeAllowlist(string(mode)); allowlist != nil {
		filtered := make([]bootstrap.ContextFile, 0, len(contextFiles))
		for _, cf := range contextFiles {
			if allowlist[cf.Path] {
				filtered = append(filtered, cf)
			}
		}
		contextFiles = filtered
	}

	// Resolve team members so agent knows who to assign tasks to.
	// Only resolve when team context is active — avoids unnecessary DB query for member-only inbound chats.
	var teamMembers []store.TeamMemberData
	if injectTeamContext && hasTeamTools && l.teamStore != nil && l.agentUUID != uuid.Nil {
		if team, _ := l.teamStore.GetTeamForAgent(ctx, l.agentUUID); team != nil {
			teamMembers, _ = l.teamStore.ListMembers(ctx, team.ID)
		}
	}

	systemPrompt := BuildSystemPrompt(SystemPromptConfig{
		AgentID:                l.id,
		AgentUUID:              l.agentUUID.String(),
		DisplayName:            l.displayName,
		Model:                  l.model,
		Workspace:              promptWorkspace,
		Channel:                channel,
		ChannelType:            channelType,
		ChatID:                 chatID,
		ChatTitle:              chatTitle,
		PeerKind:               peerKind,
		OwnerIDs:               l.ownerIDs,
		Mode:                   mode,
		ToolNames:              toolNames,
		SkillsSummary:          l.resolveSkillsSummary(ctx, skillFilter),
		PinnedSkillsSummary:    l.resolvePinnedSkillsSummary(ctx),
		HasMemory:              l.hasMemory,
		HasSpawn:               l.tools != nil && hasSpawn,
		IsTeamContext:          injectTeamContext,
		TeamWorkspace:          tools.ToolTeamWorkspaceFromCtx(ctx),
		TeamMembers:            teamMembers,
		TeamGuidance:           teamGuidance(edition.Current().TeamFullMode),
		HasSkillSearch:         hasSkillSearch,
		HasSkillManage:         l.skillEvolve && hasSkillManage,
		HasMCPToolSearch:       hasMCPToolSearch,
		HasKnowledgeGraph:      hasKG,
		HasMemoryExpand:        hasMemoryExpand,
		MCPToolDescs:           mcpToolDescs,
		ContextFiles:           contextFiles,
		AgentType:              l.agentType,
		ExtraPrompt:            extraSystemPrompt,
		SandboxEnabled:         l.sandboxEnabled,
		SandboxContainerDir:    l.sandboxContainerDir,
		SandboxWorkspaceAccess: l.sandboxWorkspaceAccess,
		ShellDenyGroups:        l.shellDenyGroups,
		SelfEvolve:             l.selfEvolve,
		TTSAutoMode:            l.ttsAutoMode,
		ProviderType:           providerTypeOf(l.provider),
		CredentialCLIContext:   l.buildCredentialCLIContext(ctx),
		IsBootstrap:            hadBootstrap && l.agentType != store.AgentTypePredefined,
		DelegateTargets:        l.delegateTargets,
		OrchMode:               l.orchMode,
		ProviderContribution:   l.providerContribution(),
	})

	messages = append(messages, providers.Message{
		Role:    "system",
		Content: systemPrompt,
	})

	// Summary context
	if summary != "" {
		messages = append(messages, providers.Message{
			Role:    "user",
			Content: fmt.Sprintf("[Previous conversation summary]\n%s", summary),
		})
		messages = append(messages, providers.Message{
			Role:    "assistant",
			Content: "I understand the context from our previous conversation. How can I help you?",
		})
	}

	// History pipeline: limitHistoryTurns → sanitizeHistory.
	// Pruning is owned by PruneStage in the pipeline (single entry point).
	trimmed := limitHistoryTurns(history, historyLimit)
	sanitized, droppedCount := sanitizeHistory(trimmed)
	messages = append(messages, sanitized...)

	// If orphaned messages were found and dropped, persist the cleaned history
	// back to the session store so the same orphans don't trigger on every request.
	if droppedCount > 0 {
		slog.Info("sanitizeHistory: cleaned session history",
			"session", sessionKey, "dropped", droppedCount)
		cleanedHistory, _ := sanitizeHistory(history)
		l.sessions.SetHistory(ctx, sessionKey, cleanedHistory)
		l.sessions.Save(ctx, sessionKey)
	}

	// Current user message
	messages = append(messages, providers.Message{
		Role:    "user",
		Content: userMessage,
	})

	return messages, hadBootstrap
}

// resolveContextFiles merges base context files (from resolver, e.g. auto-generated
// delegation targets) with per-user files. Per-user files override same-name base files,
// but base-only files (like auto-injected delegation info) are preserved.
func (l *Loop) resolveContextFiles(ctx context.Context, userID string) []bootstrap.ContextFile {
	if l.contextFileLoader == nil || userID == "" {
		return l.contextFiles
	}
	userFiles := l.contextFileLoader(ctx, l.agentUUID, userID, l.agentType)
	if len(userFiles) == 0 {
		return l.contextFiles
	}
	if len(l.contextFiles) == 0 {
		return userFiles
	}

	// Merge: start with per-user files, then append base-only files
	userSet := make(map[string]struct{}, len(userFiles))
	for _, f := range userFiles {
		userSet[f.Path] = struct{}{}
	}
	merged := make([]bootstrap.ContextFile, len(userFiles))
	copy(merged, userFiles)
	for _, base := range l.contextFiles {
		if _, exists := userSet[base.Path]; !exists {
			merged = append(merged, base)
		}
	}
	return merged
}

// mergeContextFallback adds fallback (in-memory) files into contextFiles,
// skipping any that already exist. Used when DB seeding failed.
func (l *Loop) mergeContextFallback(contextFiles, fallback []bootstrap.ContextFile) []bootstrap.ContextFile {
	existing := make(map[string]struct{}, len(contextFiles))
	for _, f := range contextFiles {
		existing[f.Path] = struct{}{}
	}
	for _, fb := range fallback {
		if _, ok := existing[fb.Path]; !ok {
			contextFiles = append(contextFiles, fb)
		}
	}
	return contextFiles
}
