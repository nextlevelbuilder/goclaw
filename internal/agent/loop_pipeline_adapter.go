package agent

import (
	"context"

	"github.com/nextlevelbuilder/goclaw/internal/pipeline"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/tokencount"
)

// runViaPipeline delegates a run to the v3 pipeline.
// Called when l.v3PipelineEnabled is true.
func (l *Loop) runViaPipeline(ctx context.Context, req RunRequest) (*RunResult, error) {
	input := convertRunInput(&req)
	// Bridge runState shares loop detection state between pipeline and agent.
	bridgeRS := &runState{}
	deps := l.buildPipelineDeps(&req, bridgeRS)

	model := l.model
	if req.ModelOverride != "" {
		model = req.ModelOverride
	}
	provider := l.provider
	if req.ProviderOverride != nil {
		provider = req.ProviderOverride
	}

	p := pipeline.NewDefaultPipeline(deps)
	state := pipeline.NewRunState(input, nil, model, provider)

	pResult, err := p.Run(ctx, state)
	if err != nil {
		return nil, err
	}
	return convertRunResult(pResult), nil
}

// buildPipelineDeps maps Loop fields + methods to PipelineDeps callbacks.
func (l *Loop) buildPipelineDeps(req *RunRequest, bridgeRS *runState) pipeline.PipelineDeps {
	maxIter := l.maxIterations
	if req.MaxIterations > 0 && req.MaxIterations < maxIter {
		maxIter = req.MaxIterations
	}

	cb := l.pipelineCallbacks(req, bridgeRS)

	return pipeline.PipelineDeps{
		TokenCounter: tokencount.NewFallbackCounter(),
		Config: pipeline.PipelineConfig{
			MaxIterations:      maxIter,
			MaxToolCalls:       l.maxToolCalls,
			CheckpointInterval: 5,
			ContextWindow:      l.contextWindow,
			MaxTokens:          l.maxTokens,
			Compaction:         l.compactionCfg,
		},
		EmitEvent: func(event any) {
			if ae, ok := event.(AgentEvent); ok {
				l.emit(ae)
			}
		},

		// Context callbacks
		ResolveWorkspace: cb.resolveWorkspace,
		LoadContextFiles: cb.loadContextFiles,
		BuildMessages:    cb.buildMessages,
		EnrichMedia:      cb.enrichMedia,
		InjectReminders:  cb.injectReminders,

		// Think callbacks
		BuildFilteredTools: cb.buildFilteredTools,
		CallLLM:            cb.callLLM,

		// Prune callbacks
		PruneMessages:   cb.pruneMessages,
		CompactMessages: cb.compactMessages,

		// Memory flush
		RunMemoryFlush: cb.runMemoryFlush,

		// Tool callbacks
		ExecuteToolCall: cb.executeToolCall,
		CheckReadOnly:   cb.checkReadOnly,

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
		FlushMessages:    cb.flushMessages,
		SanitizeContent:  cb.sanitizeContent,
		UpdateMetadata:   cb.updateMetadata,
		BootstrapCleanup: cb.bootstrapCleanup,
		MaybeSummarize:   cb.maybeSummarize,
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
		}
	}
	return &RunResult{
		Content:        pr.Content,
		RunID:          pr.RunID,
		Iterations:     pr.Iterations,
		Usage:          &pr.TotalUsage,
		Media:          media,
		Deliverables:   pr.Deliverables,
		BlockReplies:   pr.BlockReplies,
		LastBlockReply: pr.LastBlockReply,
		LoopKilled:     pr.LoopKilled,
	}
}
