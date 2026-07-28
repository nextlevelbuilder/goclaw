package agent

import (
	"context"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/eventbus"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
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
	deps := l.buildPipelineDeps(&req, bridgeRS)

	provider, model := l.resolveRunProviderModel(&req)

	p := pipeline.NewDefaultPipeline(deps)
	state := pipeline.NewRunState(input, nil, model, provider)

	pResult, err := p.Run(ctx, state)
	if err != nil {
		return nil, err
	}
	return convertRunResult(pResult), nil
}

// resolveRunProviderModel returns the provider/model to use for THIS run.
// In multi-tenant mode the global l.provider/l.model are nil/empty and the
// tenant-scoped provider arrives per-run via req.ProviderOverride/ModelOverride
// (see cmd/gateway_agents.go — "no global primary provider (multi-tenant mode)").
// Anything that makes its own LLM call for a run — the pipeline turn AND the
// empty-reply rescue — MUST resolve through here, or it silently no-ops on
// multi-tenant deploys.
func (l *Loop) resolveRunProviderModel(req *RunRequest) (providers.Provider, string) {
	provider, model := l.provider, l.model
	if req != nil {
		if req.ProviderOverride != nil {
			provider = req.ProviderOverride
		}
		if req.ModelOverride != "" {
			model = req.ModelOverride
		}
	}
	return provider, model
}

// buildPipelineDeps maps Loop fields + methods to PipelineDeps callbacks.
// browserMaxIterations is the think→act turn ceiling for browser-automation
// (extension) runs. Multi-page forms / wizards legitimately need far more turns
// than a chat reply; the loop detector + low per-turn thinking keep the extra
// turns cheap and runaway-safe, so a full job application can finish in one run
// instead of stopping at the default 30-turn cap.
const browserMaxIterations = 100

// delegateMaxIterations is the think→act ceiling for runs that can delegate to a
// connected coding agent + publish (the whole-repo port workflow). Such a run
// legitimately needs many turns — browse the repo, a SETUP delegation, several
// PARALLEL port workers, an INTEGRATE pass, then PUBLISH — and the default 30
// was being exhausted before the publish step ran, so the PR never opened. The
// loop detector + sandbox isolation keep the extra turns runaway-safe.
const delegateMaxIterations = 100

// unresolvedMaxOutputTokens is the per-call output budget used when the model's
// real ceiling can't be resolved. Generous on purpose so thinking models have
// room to reason AND emit text; auto-clamped to the model's real limit at the
// provider edge.
const unresolvedMaxOutputTokens = 32768

// resolveMaxOutputTokens returns the per-call output budget for a model. Uses
// the registry's real ceiling when known; otherwise a generous floor.
//
// The floor matters in prod: the catalog alias "default" → gemini-3.5-flash is
// registered from llm-service /v1/models, whose rows carry context_window but
// NO max_tokens, so the registered spec has MaxTokens=0. Returning 0 fell back
// to Config.MaxTokens (8192) — far too small for a THINKING model, which spends
// the whole budget on reasoning and emits zero visible text (verified in prod
// tracing: final turn finish_reason=length, output_tokens=0 → empty reply).
// The floor is auto-clamped to the model's real ceiling at the provider edge
// (openai_http.go clampMaxTokensFromError), so it's safe for any model.
func (l *Loop) resolveMaxOutputTokens(provider, model string) int {
	if l.modelRegistry != nil && model != "" {
		if spec := l.modelRegistry.Resolve(provider, model); spec != nil && spec.MaxTokens > 0 {
			return spec.MaxTokens
		}
	}
	return unresolvedMaxOutputTokens
}

// effectiveMaxIterations resolves the per-run turn ceiling. Extension runs get a
// raised floor so a full application doesn't stop mid-form. A per-request value
// may only LOWER the result (never raise it).
func (l *Loop) effectiveMaxIterations(req *RunRequest) int {
	maxIter := l.maxIterations
	if req != nil && req.ClientKind == "extension" && maxIter < browserMaxIterations {
		maxIter = browserMaxIterations
	}
	// Whole-repo port fan-out (delegate_external + github_publish_dir): raise the
	// ceiling so the run reaches PUBLISH instead of dying at the default 30.
	if maxIter < delegateMaxIterations && l.tools != nil {
		if _, ok := l.tools.Get("delegate_external"); ok {
			maxIter = delegateMaxIterations
		}
	}
	if req != nil && req.MaxIterations > 0 && req.MaxIterations < maxIter {
		maxIter = req.MaxIterations
	}
	return maxIter
}

func (l *Loop) buildPipelineDeps(req *RunRequest, bridgeRS *runState) pipeline.PipelineDeps {
	maxIter := l.effectiveMaxIterations(req)

	cb := l.pipelineCallbacks(req, bridgeRS)

	return pipeline.PipelineDeps{
		TokenCounter: tokencount.NewTiktokenCounter(),
		EventBus:     l.domainBus,
		Hooks:        l.hookDispatcher,
		Config: pipeline.PipelineConfig{
			MaxIterations:      maxIter,
			MaxToolCalls:       l.maxToolCalls,
			CheckpointInterval: 5,
			ContextWindow:      l.contextWindow,
			MaxTokens:          l.effectiveMaxTokens(),
			Compaction:         l.compactionCfg,
			// V3 memory/retrieval flags removed — always true at runtime.
		},
		// Resolve per-model context window once per run. Falls back to
		// Config.ContextWindow when registry/model is unknown (existing
		// behaviour unchanged for tests and lite edition).
		ResolveContextWindow: func(provider, model string) int {
			if l.modelRegistry == nil || model == "" {
				return 0
			}
			spec := l.modelRegistry.Resolve(provider, model)
			if spec == nil {
				return 0
			}
			return spec.ContextWindow
		},
		// Resolve the model's real output ceiling (MaxTokens) the same way, so
		// ThinkStage can request the model's full output capacity instead of the
		// small pipeline default. Forward-compat resolver maps new versions
		// (e.g. claude-opus-4-8 → claude-opus-4-6's 32k) so current models work.
		ResolveMaxOutputTokens: l.resolveMaxOutputTokens,
		EmitEvent: func(event any) {
			if ae, ok := event.(AgentEvent); ok {
				l.emit(ae)
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
			sanitized := SanitizeAssistantContent(content)
			if sanitized != "" && !IsSilentReply(sanitized) {
				cb.emitRun(AgentEvent{
					Type:    protocol.AgentEventBlockReply,
					AgentID: l.id,
					RunID:   req.RunID,
					Payload: map[string]string{"content": sanitized},
				})
			}
		},

		// Prune callbacks
		PruneMessages:     cb.pruneMessages,
		CollapseSnapshots: cb.collapseSnapshots,
		SanitizeHistory:   cb.sanitizeHistory,
		CompactMessages:   cb.compactMessages,

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
		CheckReadOnly:     cb.checkReadOnly,

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
		SkillPostscript:        l.makeSkillPostscript(),
		SanitizeContent:        cb.sanitizeContent,
		StripMessageDirectives: StripMessageDirectives,
		DeduplicateMediaSuffix: deduplicateMediaSuffix,
		IsSilentReply:          IsSilentReply,
		EmitSessionCompleted: func(ctx context.Context, sessionKey string, msgCount, tokensUsed, compactionCount int) {
			if l.domainBus != nil {
				// Include existing session summary (from previous compaction cycles).
				// Current cycle's compaction runs async so its summary isn't ready yet,
				// but previous summaries are available and useful for episodic creation.
				var summary string
				if compactionCount > 0 {
					summary = l.sessions.GetSummary(ctx, sessionKey)
				}
				// See loop_finalize.go — unify user_id across channels via the
				// existing contact-merge resolver so memory writers see one
				// canonical identity per human.
				memUserID := l.resolveCredentialUserID(ctx, *req)
				l.domainBus.Publish(eventbus.DomainEvent{
					Type:     eventbus.EventSessionCompleted,
					TenantID: l.tenantID.String(),
					AgentID:  l.agentUUID.String(),
					UserID:   memUserID,
					SourceID: sessionKey,
					Payload: &eventbus.SessionCompletedPayload{
						SessionKey:      sessionKey,
						MessageCount:    msgCount,
						TokensUsed:      tokensUsed,
						CompactionCount: compactionCount,
						Summary:         summary,
					},
				})
			}
		},
		UpdateMetadata:   cb.updateMetadata,
		BootstrapCleanup: cb.bootstrapCleanup,
		MaybeSummarize:   cb.maybeSummarize,
		HandleEmptyReply: func(ctx context.Context, history []providers.Message) string {
			// One tools-disabled retry that streams text back over the SAME
			// WS subscription as the primary turn, so the assistant bubble
			// fills live instead of staying empty until the user reloads.
			emitChunk := l.makeRescueChunkEmitter(req)
			if rescued := l.rescueEmptyReply(ctx, req, history, emitChunk); rescued != "" {
				return SanitizeAssistantContent(rescued)
			}
			// Rescue also empty — return a localised sentence so the user
			// sees something actionable instead of a "..." placeholder.
			locale := store.LocaleFromContext(ctx)
			return i18n.T(locale, i18n.MsgEmptyReplyFallback)
		},

		// In-pipeline barrier: keeps the entire user turn inside a single
		// pipeline run, so FinalizeStage only flushes ONCE and the parent
		// emits exactly one assistant row to history. Without this the
		// older two-call barrier (run pipeline → drain → run pipeline
		// again) produces two assistant rows, and a mid-stream page
		// reload sees an "I'll spawn…" bubble PLUS a separate synthesis
		// bubble (split UI).
		WaitForChildren: l.makePipelineBarrier(req),
	}
}

// makePipelineBarrier returns the pipeline.WaitForChildren callback. It
// is nil-safe and returns false when the loop has no SubagentManager
// attached or no children to drain — letting the pipeline exit normally.
func (l *Loop) makePipelineBarrier(req *RunRequest) func(ctx context.Context, state *pipeline.RunState) bool {
	consumedIDs := map[string]struct{}{}
	passes := 0
	return func(ctx context.Context, state *pipeline.RunState) bool {
		if passes >= barrierMaxPasses {
			return false
		}
		systemMsg, newConsumed, drained := l.drainSpawnedChildren(ctx, req.RunID, consumedIDs)
		if !drained {
			return false
		}
		for _, id := range newConsumed {
			consumedIDs[id] = struct{}{}
		}
		passes++
		logBarrierPass(req.RunID, passes, len(newConsumed), len(consumedIDs))
		// Append the synthetic [System Message] to the EPHEMERAL buffer:
		// visible to the LLM via state.Messages.All() on the synthesis
		// iteration, but excluded from FlushPending → never reaches the
		// session store. Persisting it would leave a stray `role:user`
		// row in DB between the assistant's spawn turn and its synthesis
		// turn, which breaks UI grouping on page reload (history loader
		// renders user-role rows as separate user bubbles, so the model's
		// reply gets split into two assistant bubbles around the fake
		// user turn).
		state.Messages.AppendEphemeral(providers.Message{
			Role:    "user",
			Content: systemMsg,
		})
		return true
	}
}

// convertRunInput converts agent.RunRequest to pipeline.RunInput.
func convertRunInput(req *RunRequest) *pipeline.RunInput {
	return &pipeline.RunInput{
		SessionKey:        req.SessionKey,
		Message:           req.Message,
		Media:             req.Media,
		ForwardMedia:      req.ForwardMedia,
		Channel:           req.Channel,
		ChannelType:       req.ChannelType,
		ChatTitle:         req.ChatTitle,
		ChatID:            req.ChatID,
		PeerKind:          req.PeerKind,
		RunID:             req.RunID,
		UserID:            req.UserID,
		SenderID:          req.SenderID,
		Stream:            req.Stream,
		ExtraSystemPrompt: req.ExtraSystemPrompt,
		SkillFilter:       req.SkillFilter,
		HistoryLimit:      req.HistoryLimit,
		ToolAllow:         req.ToolAllow,
		LightContext:      req.LightContext,
		RunKind:           req.RunKind,
		DelegationID:      req.DelegationID,
		TeamID:            req.TeamID,
		TeamTaskID:        req.TeamTaskID,
		ParentAgentID:     req.ParentAgentID,
		MaxIterations:     req.MaxIterations,
		ModelOverride:     req.ModelOverride,
		HideInput:         req.HideInput,
		ContentSuffix:     req.ContentSuffix,
		LeaderAgentID:     req.LeaderAgentID,
		WorkspaceChannel:  req.WorkspaceChannel,
		WorkspaceChatID:   req.WorkspaceChatID,
		TeamWorkspace:     req.TeamWorkspace,
	}
}

// convertRunResult converts pipeline.RunResult to agent.RunResult.
func convertRunResult(pr *pipeline.RunResult) *RunResult {
	if pr == nil {
		return nil
	}
	media := make([]MediaResult, len(pr.MediaResults))
	for i, m := range pr.MediaResults {
		media[i] = MediaResult{
			Path:        m.Path,
			ContentType: m.ContentType,
			Size:        m.Size,
			AsVoice:     m.AsVoice,
			Filename:    m.Filename,
		}
	}
	stopReason := ""
	if pr.MaxIterationsReached {
		stopReason = "max_steps"
	}
	return &RunResult{
		Content:        pr.Content,
		Thinking:       pr.Thinking,
		RunID:          pr.RunID,
		Iterations:     pr.Iterations,
		Usage:          &pr.TotalUsage,
		Media:          media,
		Deliverables:   pr.Deliverables,
		BlockReplies:   pr.BlockReplies,
		LastBlockReply: pr.LastBlockReply,
		LoopKilled:     pr.LoopKilled,
		StopReason:     stopReason,
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
