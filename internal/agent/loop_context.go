package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/internal/workspace"
)

// contextSetupResult holds the outputs of injectContext that are needed by the main loop.
type contextSetupResult struct {
	ctx                  context.Context
	resolvedTeamSettings json.RawMessage
}

// injectContext enriches the context with agent, tenant, user, workspace, and tool-level
// values needed by the agent loop and tool execution. Also runs input guard and message
// truncation. Returns error only if input guard blocks the message.
func (l *Loop) injectContext(ctx context.Context, req *RunRequest) (contextSetupResult, error) {
	// Inject agent UUID + key into context for tool routing
	if l.agentUUID != uuid.Nil {
		ctx = store.WithAgentID(ctx, l.agentUUID)
	}
	if l.id != "" {
		ctx = store.WithAgentKey(ctx, l.id)
	}
	// Inject tenant into context for tool-level tenant scoping (spawn, MCP, etc.)
	if l.tenantID != uuid.Nil {
		ctx = store.WithTenantID(ctx, l.tenantID)
	}
	// Inject user ID into context for per-user scoping (memory, context files, etc.)
	if req.UserID != "" {
		ctx = store.WithUserID(ctx, req.UserID)
	}
	// Resolve merged tenant user identity for credential lookups.
	// Keeps UserID unchanged (session/workspace scoping) but sets a separate
	// CredentialUserID for SecureCLI, MCP, and other per-user features.
	if l.userResolver != nil && req.UserID != "" {
		credUserID := l.resolveCredentialUserID(ctx, *req)
		if credUserID != "" && credUserID != req.UserID {
			ctx = store.WithCredentialUserID(ctx, credUserID)
		}
	}
	// Inject agent type into context for interceptor routing
	if l.agentType != "" {
		ctx = store.WithAgentType(ctx, l.agentType)
	}
	// Inject self-evolve flag for predefined agents that can update SOUL.md
	if l.selfEvolve {
		ctx = store.WithSelfEvolve(ctx, true)
	}
	// Inject original sender ID for group file writer permission checks
	if req.SenderID != "" {
		ctx = store.WithSenderID(ctx, req.SenderID)
	}
	// Inject sender display name for bootstrap auto-contact
	if req.SenderName != "" {
		ctx = store.WithSenderName(ctx, req.SenderName)
	}
	// Inject caller role so RBAC-aware permission checks (CheckFileWriterPermission,
	// CheckCronPermission) can bypass per-user grants for authenticated admins
	// dispatched from dashboard or other trusted sources (#915).
	if req.Role != "" {
		ctx = store.WithRole(ctx, req.Role)
	}
	// Inject global + per-agent builtin tool settings (tier 1+3).
	// Media/provider-chain tools read the merged view via BuiltinToolSettingsFromCtx.
	if l.builtinToolSettings != nil {
		ctx = tools.WithBuiltinToolSettings(ctx, l.builtinToolSettings)
	}
	// Inject tenant-layer tool settings (tier 2). Merge with per-agent happens
	// at read time — per-agent still wins at tool-name level.
	if l.tenantToolSettings != nil {
		ctx = tools.WithTenantToolSettings(ctx, l.tenantToolSettings)
	}
	// Inject tenant-specific allowed paths for filesystem tools.
	if len(l.tenantAllowedPaths) > 0 {
		ctx = tools.WithTenantAllowedPaths(ctx, l.tenantAllowedPaths)
	}
	// Inject channel type into context for tools (e.g. message tool needs it for Zalo group routing)
	if req.ChannelType != "" {
		ctx = tools.WithToolChannelType(ctx, req.ChannelType)
	}
	// Inject per-agent overrides from DB so tools honor per-agent settings.
	if l.restrictToWs != nil {
		ctx = tools.WithRestrictToWorkspace(ctx, *l.restrictToWs)
	}
	if l.subagentsCfg != nil {
		ctx = tools.WithSubagentConfig(ctx, l.subagentsCfg)
	}
	// Pass the agent's model and provider so subagents inherit the correct combo.
	if l.model != "" {
		ctx = tools.WithParentModel(ctx, l.model)
	}
	if l.provider != nil {
		ctx = tools.WithParentProvider(ctx, l.provider.Name())
	}
	if l.memoryCfg != nil {
		ctx = tools.WithMemoryConfig(ctx, l.memoryCfg)
	}
	if l.sandboxCfg != nil {
		ctx = tools.WithSandboxConfig(ctx, l.sandboxCfg)
	}
	if l.shellDenyGroups != nil {
		ctx = store.WithShellDenyGroups(ctx, l.shellDenyGroups)
	}

	// Workspace scope propagation (delegation origin → workspace tools).
	if req.WorkspaceChannel != "" {
		ctx = tools.WithWorkspaceChannel(ctx, req.WorkspaceChannel)
	}
	// WorkspaceChatID drives vault chat_id isolation in isolated teams. Callers
	// that don't set it explicitly fall back to req.ChatID — the chat segment
	// used for workspace path layering — so the vault filter activates uniformly
	// across every RunRequest entry point (WS direct, HTTP, cron, subagent).
	effectiveWorkspaceChatID := req.WorkspaceChatID
	if effectiveWorkspaceChatID == "" {
		effectiveWorkspaceChatID = req.ChatID
	}
	if effectiveWorkspaceChatID != "" {
		ctx = tools.WithWorkspaceChatID(ctx, effectiveWorkspaceChatID)
	}
	if req.TeamTaskID != "" {
		ctx = tools.WithTeamTaskID(ctx, req.TeamTaskID)
	}
	if req.DelegationID != "" {
		ctx = tools.WithDelegationID(ctx, req.DelegationID)
	}

	// --- Per-user setup: file seeding + workspace resolution ---
	// Uses userSetups sync.Map to track both concerns atomically per user.
	// Seeding must run before buildMessages→resolveContextFiles reads context files.
	// Team sessions skip seeding: members process tasks from leader, not end-user onboarding.
	isTeamSession := bootstrap.IsTeamSession(req.SessionKey)
	channelMeta := l.buildChannelMeta(req)
	setup := l.getOrCreateUserSetup(ctx, req.UserID, req.Channel, isTeamSession, channelMeta)

	// Workspace resolution (layered pipeline).
	// Layer order: tenant → team → project (future) → user/chat
	// Two entry modes: solo agent (base = l.workspace) or team context (base = l.dataDir).
	// Result is always a single folder set via WithToolWorkspace.
	if l.workspace != "" && req.UserID != "" {
		ws := setup.workspace
		if ws == "" {
			ws = l.workspace
		}
		// Apply user isolation layer via pipeline.
		shared := l.shouldShareWorkspace(req.UserID, req.PeerKind)
		effectiveWorkspace := tools.ResolveWorkspace(ws,
			tools.UserChatLayer(tools.SanitizePathSegment(req.UserID), shared),
		)
		if l.shouldShareMemory() {
			ctx = store.WithSharedMemory(ctx)
		}
		if l.shouldShareKnowledgeGraph() {
			ctx = store.WithSharedKG(ctx)
		}
		if l.shouldShareSessions() {
			ctx = store.WithSharedSessions(ctx)
		}
		if err := os.MkdirAll(effectiveWorkspace, 0755); err != nil {
			slog.Warn("failed to create user workspace directory", "workspace", effectiveWorkspace, "user", req.UserID, "error", err)
		}
		ctx = tools.WithToolWorkspace(ctx, effectiveWorkspace)
	} else if l.workspace != "" {
		ctx = tools.WithToolWorkspace(ctx, l.workspace)
	}

	// Team workspace: dispatched task overrides default workspace.
	if req.TeamWorkspace != "" {
		if err := os.MkdirAll(req.TeamWorkspace, 0755); err != nil {
			slog.Warn("failed to create team workspace directory", "workspace", req.TeamWorkspace, "error", err)
		}
		ctx = tools.WithToolTeamWorkspace(ctx, req.TeamWorkspace)
		ctx = tools.WithToolWorkspace(ctx, req.TeamWorkspace)
	}
	if req.TeamID != "" {
		ctx = tools.WithToolTeamID(ctx, req.TeamID)
		// Team root for dispatched tasks: resolve the UserChatLayer-stripped root
		// so the dispatched agent can still read peer-scoped files in the same team.
		if teamUUID, err := uuid.Parse(req.TeamID); err == nil && l.dataDir != "" {
			teamRoot := tools.ResolveWorkspace(l.dataDir,
				tools.TenantLayer(store.TenantIDFromContext(ctx), store.TenantSlugFromContext(ctx)),
				tools.TeamLayer(teamUUID),
			)
			ctx = tools.WithToolTeamRoot(ctx, teamRoot)
		}
	}
	if req.LeaderAgentID != "" {
		ctx = tools.WithLeaderAgentID(ctx, req.LeaderAgentID)
	}

	// Team workspace: auto-resolve for agents with team membership (not dispatched).
	// Lead agents default to team workspace; non-lead members keep own workspace.
	var resolvedTeamSettings json.RawMessage
	// Dispatched tasks already have TeamWorkspace set but still need team settings
	// for TeamIsolated flag. Fetch by explicit TeamID in that branch.
	if req.TeamWorkspace != "" && req.TeamID != "" && l.teamStore != nil {
		if teamUUID, err := uuid.Parse(req.TeamID); err == nil {
			if team, _ := l.teamStore.GetTeam(ctx, teamUUID); team != nil {
				resolvedTeamSettings = team.Settings
			}
		}
	}
	if req.TeamWorkspace == "" && l.teamStore != nil && l.agentUUID != uuid.Nil {
		if team, _ := l.teamStore.GetTeamForAgent(ctx, l.agentUUID); team != nil {
			resolvedTeamSettings = team.Settings
			wsChat := req.ChatID
			if wsChat == "" {
				wsChat = req.UserID
			}
			shared := tools.IsSharedWorkspace(team.Settings)
			// Resolve team workspace via layered pipeline: tenant → team → user/chat.
			wsDir := tools.ResolveWorkspace(l.dataDir,
				tools.TenantLayer(store.TenantIDFromContext(ctx), store.TenantSlugFromContext(ctx)),
				tools.TeamLayer(team.ID),
				tools.UserChatLayer(wsChat, shared),
			)
			if err := os.MkdirAll(wsDir, 0750); err != nil {
				slog.Warn("failed to create team workspace directory", "workspace", wsDir, "error", err)
			}
			ctx = tools.WithToolTeamWorkspace(ctx, wsDir)
			// Team root (no UserChatLayer): lets any team agent — leader or member —
			// read files produced by peers under different chat/user scopes within
			// the same team. Writes still default to wsDir above; team root only
			// widens the allowed-prefix set for path boundary checks.
			teamRoot := tools.ResolveWorkspace(l.dataDir,
				tools.TenantLayer(store.TenantIDFromContext(ctx), store.TenantSlugFromContext(ctx)),
				tools.TeamLayer(team.ID),
			)
			ctx = tools.WithToolTeamRoot(ctx, teamRoot)
			// Leader keeps personal workspace (set at line 110-132) as default.
			// Team workspace accessible via ToolTeamWorkspaceFromCtx for delegation.
			if req.TeamID == "" {
				ctx = tools.WithToolTeamID(ctx, team.ID.String())
			}
		}
	}

	// V3 workspace: resolve once, set immutable context.
	{
		var teamIDPtr *string
		if req.TeamID != "" {
			teamIDPtr = &req.TeamID
		}
		var teamWSConfig *workspace.TeamWorkspaceConfig
		if resolvedTeamSettings != nil {
			var cfg workspace.TeamWorkspaceConfig
			if json.Unmarshal(resolvedTeamSettings, &cfg) == nil {
				teamWSConfig = &cfg
			}
		}
		resolver := workspace.NewResolver()
		wc, wsErr := resolver.Resolve(ctx, workspace.ResolveParams{
			// Filesystem path segment must use agent_key, not UUID — matches
			// the v2 path in loop_pipeline_callbacks.go and the session_key
			// anchor. See docs/agent-identity-conventions.md.
			AgentID:    l.id,
			AgentType:  l.agentType,
			UserID:     req.UserID,
			ChatID:     req.ChatID,
			TenantID:   store.TenantIDFromContext(ctx).String(),
			TenantSlug: store.TenantSlugFromContext(ctx),
			PeerKind:   req.PeerKind,
			TeamID:     teamIDPtr,
			TeamConfig: teamWSConfig,
			BaseDir:    l.dataDir,
		})
		if wsErr != nil {
			slog.Warn("workspace resolution failed", "err", wsErr)
		} else {
			ctx = workspace.WithContext(ctx, wc)
		}
	}

	// Persist agent UUID + user ID on the session (for querying/tracing)
	if l.agentUUID != uuid.Nil || req.UserID != "" {
		l.sessions.SetAgentInfo(ctx, req.SessionKey, l.agentUUID, req.UserID)
	}

	// Security: scan user message for injection patterns.
	// Action is configurable: "log" (info), "warn" (default), "block" (reject message).
	if l.inputGuard != nil {
		if matches := l.inputGuard.Scan(req.Message); len(matches) > 0 {
			matchStr := strings.Join(matches, ",")
			switch l.injectionAction {
			case "block":
				slog.Warn("security.injection_blocked",
					"agent", l.id, "user", req.UserID,
					"patterns", matchStr, "message_len", len(req.Message),
				)
				return contextSetupResult{}, fmt.Errorf("message blocked: potential prompt injection detected (%s)", matchStr)
			case "log":
				slog.Info("security.injection_detected",
					"agent", l.id, "user", req.UserID,
					"patterns", matchStr, "message_len", len(req.Message),
				)
			default: // "warn"
				slog.Warn("security.injection_detected",
					"agent", l.id, "user", req.UserID,
					"patterns", matchStr, "message_len", len(req.Message),
				)
			}
		}
	}

	// Inject agent key into context for tool-level resolution (multiple agents share tool registry)
	ctx = tools.WithToolAgentKey(ctx, l.id)

	// Inject delivered media tracker so write_file and message tool can coordinate:
	// write_file(deliver=true) marks paths, message self-send guard checks before allowing.
	ctx = tools.WithDeliveredMedia(ctx, tools.NewDeliveredMedia())

	// Security: truncate oversized user messages gracefully (feed truncation notice into LLM)
	maxChars := l.maxMessageChars
	if maxChars <= 0 {
		maxChars = config.DefaultMaxMessageChars
	}
	if len(req.Message) > maxChars {
		originalLen := len(req.Message)
		req.Message = req.Message[:maxChars] +
			fmt.Sprintf("\n\n[System: Message was truncated from %d to %d characters due to size limit. "+
				"Please ask the user to send shorter messages or use the read_file tool for large content.]",
				originalLen, maxChars)
		slog.Warn("security.message_truncated",
			"agent", l.id, "user", req.UserID,
			"original_len", originalLen, "truncated_to", maxChars,
		)
	}

	// Build RunContext from all resolved values and inject as single context key.
	// This provides a typed, inspectable snapshot of all loop-injected context.
	// Individual With* keys above remain for backward compat during transition.
	providerName := ""
	if l.provider != nil {
		providerName = l.provider.Name()
	}
	// Extract resolved credential user ID (set earlier via WithCredentialUserID, empty if not resolved).
	credUserID, _ := ctx.Value(store.CredentialUserIDKey).(string)
	rc := &store.RunContext{
		AgentID:             l.agentUUID,
		AgentKey:            l.id,
		TenantID:            l.tenantID,
		UserID:              req.UserID,
		CredentialUserID:    credUserID,
		AgentType:           l.agentType,
		SenderID:            req.SenderID,
		SelfEvolve:          l.selfEvolve,
		SharedMemory:        store.IsSharedMemory(ctx),
		SharedKG:            store.IsSharedKG(ctx),
		SharedSessions:      store.IsSharedSessions(ctx),
		RestrictToWorkspace: l.restrictToWs != nil && *l.restrictToWs,
		BuiltinToolSettings: l.builtinToolSettings,
		ChannelType:         req.ChannelType,
		SubagentsCfg:        l.subagentsCfg,
		ParentModel:         l.model,
		ParentProvider:      providerName,
		MemoryCfg:           l.memoryCfg,
		SandboxCfg:          l.sandboxCfg,
		ShellDenyGroups:     l.shellDenyGroups,
		Workspace:           tools.ToolWorkspaceFromCtx(ctx),
		TeamWorkspace:       tools.ToolTeamWorkspaceFromCtx(ctx),
		TeamID:              tools.ToolTeamIDFromCtx(ctx),
		WorkspaceChannel:    req.WorkspaceChannel,
		WorkspaceChatID:     effectiveWorkspaceChatID,
		TeamIsolated:        resolvedTeamSettings != nil && !tools.IsSharedWorkspace(resolvedTeamSettings),
		TeamTaskID:          req.TeamTaskID,
		LeaderAgentID:       tools.LeaderAgentIDFromCtx(ctx),
		AgentToolKey:        l.id,
		TenantAllowedPaths:  l.tenantAllowedPaths,
	}
	ctx = store.WithRunContext(ctx, rc)

	return contextSetupResult{
		ctx:                  ctx,
		resolvedTeamSettings: resolvedTeamSettings,
	}, nil
}
