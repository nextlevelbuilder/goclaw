package agent

import (
	"context"
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/eventbus"
	"github.com/nextlevelbuilder/goclaw/internal/memory"
	"github.com/nextlevelbuilder/goclaw/internal/pipeline"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tokencount"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// runViaPipeline delegates a run to the v3 pipeline.
func (l *Loop) runViaPipeline(ctx context.Context, req RunRequest) (*RunResult, error) {
	input := convertRunInput(&req)
	// Bridge runState shares loop detection state between pipeline and agent.
	bridgeRS := &runState{}

	// Resolve the effective model + provider BEFORE building deps so the pre-call
	// budget estimate reserves reasoning output for the model that will actually
	// run (a ModelOverride can change the reasoning capability, hence the bump).
	model := l.model
	if req.ModelOverride != "" {
		model = req.ModelOverride
	}
	provider := l.provider
	if req.ProviderOverride != nil {
		provider = req.ProviderOverride
	} else if req.ModelOverride != "" {
		if fallback, ok := provider.(interface{ PrimaryProvider() providers.Provider }); ok {
			provider = fallback.PrimaryProvider()
		}
	}

	deps := l.buildPipelineDeps(&req, bridgeRS)

	p := pipeline.NewDefaultPipeline(deps)
	state := pipeline.NewRunState(input, nil, model, provider)

	pResult, err := p.Run(ctx, state)
	if err != nil {
		return nil, err
	}
	return redactDelegationRunResult(&req, convertRunResult(pResult)), nil
}

// buildPipelineDeps maps Loop fields + methods to PipelineDeps callbacks.
// effProvider/effModel are the resolved provider+model for THIS run (after any
// ModelOverride/ProviderOverride) so reasoning-effort resolution matches the
// request the pipeline will actually send.
func (l *Loop) buildPipelineDeps(req *RunRequest, bridgeRS *runState) pipeline.PipelineDeps {
	maxIter := l.maxIterations
	if req.MaxIterations > 0 && req.MaxIterations < maxIter {
		maxIter = req.MaxIterations
	}

	cb := l.pipelineCallbacks(req, bridgeRS)
	emitBlockReply := func(content, source string) {
		sanitized := SanitizeAssistantContent(content)
		if sanitized == "" || IsSilentReply(sanitized) {
			return
		}
		payload := map[string]string{"content": sanitized}
		if source != "" {
			payload["source"] = source
		}
		cb.emitRun(AgentEvent{
			Type:    protocol.AgentEventBlockReply,
			AgentID: l.id,
			RunID:   req.RunID,
			Payload: payload,
		})
	}

	return pipeline.PipelineDeps{
		TokenCounter:  tokencount.NewTiktokenCounter(),
		BudgetCounter: l.budgetCounter,
		EventBus:      l.domainBus,
		Hooks:         l.hookDispatcher,
		Config: pipeline.PipelineConfig{
			MaxIterations:      maxIter,
			MaxToolCalls:       l.maxToolCalls,
			CheckpointInterval: 5,
			ContextWindow:      l.contextWindow,
			MaxTokens:          l.effectiveMaxTokens(),
			ReserveTokens:      l.resolveReserveTokens(),
			Compaction:         l.compactionCfg,
			// V3 memory/retrieval flags removed — always true at runtime.
		},
		ResolveContextWindow: l.resolveEffectiveContextWindow,
		EmitEvent: func(event any) {
			if ae, ok := event.(AgentEvent); ok {
				l.emit(redactDelegationAgentEvent(req, ae))
			}
		},

		// V3 auto-inject: episodic memory L0 injection into system prompt.
		// Captures agent/tenant context via closure for store scoping.
		AutoInject: l.makeAutoInjectCallback(req),

		// Context injection + session history
		InjectContext:      cb.injectContext,
		LoadSessionHistory: cb.loadSessionHistory,

		// Context callbacks
		ResolveWorkspace: cb.resolveWorkspace,
		LoadContextFiles: cb.loadContextFiles,
		BuildMessages:    cb.buildMessages,
		EnrichMedia:      cb.enrichMedia,
		InjectReminders:  cb.injectReminders,

		// Think callbacks
		BuildFilteredTools: cb.buildFilteredTools,
		CallLLM:            cb.callLLM,
		UniqueToolCallIDs:  uniquifyToolCallIDs,
		EmitBlockReply: func(content string) {
			emitBlockReply(content, "")
		},
		EmitBlockReplyWithSource: emitBlockReply,

		// Prune callbacks
		PruneMessages:   cb.pruneMessages,
		SanitizeHistory: cb.sanitizeHistory,
		CompactMessages: cb.compactMessages,

		// Cache-TTL gate callbacks (Phase 06)
		GetProviderCaps: func() providers.ProviderCapabilities {
			if ca, ok := l.provider.(providers.CapabilitiesAware); ok {
				return ca.Capabilities()
			}
			return providers.ProviderCapabilities{}
		},
		GetPruningConfig: func() *config.ContextPruningConfig {
			return l.contextPruningCfg
		},
		GetCacheTouch:    l.cacheTouchAt,
		MarkCacheTouched: l.markCacheTouched,

		// Memory flush
		RunMemoryFlush: cb.runMemoryFlush,

		// Tool callbacks
		ExecuteToolCall:   cb.executeToolCall,
		ExecuteToolRaw:    cb.executeToolRaw,
		ProcessToolResult: cb.processToolResult,
		AuthorizeToolCall: cb.authorizeToolCall,
		SequentialToolCall: func(tc providers.ToolCall) bool {
			return l.resolveToolCallName(tc.Name) == "wait"
		},
		ParallelEligibleToolCall: l.parallelEligibleToolCall,
		CheckReadOnly:            cb.checkReadOnly,

		// Observe: drain InjectCh
		DrainInjectCh: func() []providers.Message {
			if req.InjectCh == nil {
				return nil
			}
			var msgs []providers.Message
			for {
				select {
				case injected := <-req.InjectCh:
					msgs = append(msgs, providers.Message{
						Role:    "user",
						Content: injected.Content,
					})
				default:
					return msgs
				}
			}
		},

		// Checkpoint + Finalize
		FlushMessages:          cb.flushMessages,
		PersistAssistantImages: persistAssistantImages,
		SkillPostscript:        l.makeSkillPostscript(),
		SanitizeContent:        cb.sanitizeContent,
		StripMessageDirectives: StripMessageDirectives,
		DeduplicateMediaSuffix: deduplicateMediaSuffix,
		IsSilentReply:          IsSilentReply,
		EmitSessionCompleted: func(ctx context.Context, sessionKey string, msgCount, tokensUsed, _ int) {
			// The per-run count (5th arg) is intentionally ignored — emitSessionCompleted
			// reads the CUMULATIVE session count itself (Bug A). See method doc.
			l.emitSessionCompleted(ctx, sessionKey, req.UserID, msgCount, tokensUsed)
		},
		UpdateMetadata:   cb.updateMetadata,
		BootstrapCleanup: cb.bootstrapCleanup,
		MaybeSummarize:   cb.maybeSummarize,
	}
}

// emitSessionCompleted publishes the session.completed domain event that drives
// the consolidation pipeline (episodic → semantic → dreaming). No-op when no bus.
//
// Bug A: it reads the CUMULATIVE session compaction count via GetCompactionCount
// (matching the legacy v2 emit path) rather than the per-run counter the pipeline
// tracks — the per-run counter resets to 0 each run, which pinned source_id at
// ":0" and made the episodic worker skip every cycle after the first.
//
// Bug C: SourceID embeds that count ("<sessionKey>:<count>") so the eventbus dedup
// key (Type+":"+SourceID, 5m TTL) advances per compaction cycle instead of
// swallowing every rapid same-session turn. Safe for the worker, which builds its
// own idempotency key from payload.SessionKey+payload.CompactionCount and never
// parses SourceID.
func (l *Loop) emitSessionCompleted(ctx context.Context, sessionKey, userID string, msgCount, tokensUsed int) {
	if l.domainBus == nil {
		return
	}
	count := l.sessions.GetCompactionCount(ctx, sessionKey)
	// Attach the existing session summary (from a PREVIOUS compaction cycle) when
	// one exists. The current cycle's summary is produced asynchronously and isn't
	// ready yet, but a prior summary spares the worker an LLM re-summarize call.
	var summary string
	if count > 0 {
		summary = l.sessions.GetSummary(ctx, sessionKey)
	}
	l.domainBus.Publish(eventbus.DomainEvent{
		Type:     eventbus.EventSessionCompleted,
		TenantID: l.tenantID.String(),
		AgentID:  l.agentUUID.String(),
		UserID:   userID,
		SourceID: fmt.Sprintf("%s:%d", sessionKey, count),
		Payload: &eventbus.SessionCompletedPayload{
			SessionKey:      sessionKey,
			MessageCount:    msgCount,
			TokensUsed:      tokensUsed,
			CompactionCount: count,
			Summary:         summary,
		},
	})
}

func (l *Loop) resolveEffectiveContextWindow() int {
	return l.contextWindow
}

// convertRunInput converts agent.RunRequest to pipeline.RunInput.
func convertRunInput(req *RunRequest) *pipeline.RunInput {
	return &pipeline.RunInput{
		SessionKey:                 req.SessionKey,
		Message:                    req.Message,
		Media:                      req.Media,
		ForwardMedia:               req.ForwardMedia,
		Channel:                    req.Channel,
		ChannelType:                req.ChannelType,
		BitrixPortalDomain:         req.BitrixPortalDomain,
		ChatTitle:                  req.ChatTitle,
		ChatID:                     req.ChatID,
		PeerKind:                   req.PeerKind,
		RunID:                      req.RunID,
		UserID:                     req.UserID,
		SenderID:                   req.SenderID,
		SenderName:                 req.SenderName,
		Stream:                     req.Stream,
		ExtraSystemPrompt:          req.ExtraSystemPrompt,
		SkillFilter:                req.SkillFilter,
		HistoryLimit:               req.HistoryLimit,
		ToolAllow:                  req.ToolAllow,
		TelegramManagerPermissions: req.TelegramManagerPermissions,
		LightContext:               req.LightContext,
		RunKind:                    req.RunKind,
		DelegationID:               req.DelegationID,
		TeamID:                     req.TeamID,
		TeamTaskID:                 req.TeamTaskID,
		ParentAgentID:              req.ParentAgentID,
		MaxIterations:              req.MaxIterations,
		ModelOverride:              req.ModelOverride,
		HideInput:                  req.HideInput,
		ContentSuffix:              req.ContentSuffix,
		LeaderAgentID:              req.LeaderAgentID,
		WorkspaceChannel:           req.WorkspaceChannel,
		WorkspaceChatID:            req.WorkspaceChatID,
		TeamWorkspace:              req.TeamWorkspace,
	}
}

// convertRunResult converts pipeline.RunResult to agent.RunResult.
func convertRunResult(pr *pipeline.RunResult) *RunResult {
	if pr == nil {
		return nil
	}
	var lastUsage *providers.Usage
	if pr.LastUsage.PromptTokens > 0 || pr.LastUsage.CompletionTokens > 0 || pr.LastUsage.TotalTokens > 0 {
		lu := pr.LastUsage
		lastUsage = &lu
	}
	media := make([]MediaResult, len(pr.MediaResults))
	for i, m := range pr.MediaResults {
		media[i] = MediaResult{
			Path:        m.Path,
			ContentType: m.ContentType,
			Size:        m.Size,
			AsVoice:     m.AsVoice,
			Prompt:      m.Prompt,
		}
	}
	return &RunResult{
		Content:        pr.Content,
		Thinking:       pr.Thinking,
		RunID:          pr.RunID,
		Iterations:     pr.Iterations,
		Usage:          &pr.TotalUsage,
		LastUsage:      lastUsage,
		Media:          media,
		Deliverables:   pr.Deliverables,
		BlockReplies:   pr.BlockReplies,
		LastBlockReply: pr.LastBlockReply,
		LoopKilled:     pr.LoopKilled,
		Calls:          pr.Calls,
	}
}

// makeAutoInjectCallback creates the AutoInject callback that captures agent/tenant context.
// Returns nil if autoInjector is not configured (v3 retrieval disabled or no episodic store).
// Phase 9: plumbs recentContext through to enrich vector search queries for
// context-aware recall.
func (l *Loop) makeAutoInjectCallback(req *RunRequest) func(ctx context.Context, userMessage, userID, recentContext string) (string, error) {
	if l.autoInjector == nil {
		return nil
	}
	return func(ctx context.Context, userMessage, userID, recentContext string) (string, error) {
		result, err := l.autoInjector.Inject(ctx, memory.InjectParams{
			AgentID:       l.agentUUID.String(),
			UserID:        store.MemoryUserID(ctx),
			TenantID:      store.TenantIDFromContext(ctx).String(),
			UserMessage:   userMessage,
			RecentContext: recentContext,
		})
		if err != nil || result == nil {
			return "", err
		}
		return result.Section, nil
	}
}
