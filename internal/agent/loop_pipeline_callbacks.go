package agent

import (
	"context"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/pipeline"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/workspace"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// pipelineCallbacks creates all callback closures that capture *Loop.
// Each callback bridges a pipeline.PipelineDeps function to an existing Loop method.
func (l *Loop) pipelineCallbacks(req *RunRequest, bridgeRS *runState) pipelineCallbackSet {
	return pipelineCallbackSet{
		injectContext:      l.makeInjectContext(req),
		loadSessionHistory: l.makeLoadSessionHistory(),
		resolveWorkspace:   l.makeResolveWorkspace(req),
		loadContextFiles:   l.makeLoadContextFiles(),
		buildMessages:      l.makeBuildMessages(),
		enrichMedia:        l.makeEnrichMedia(req),
		injectReminders:    l.makeInjectReminders(req),
		buildFilteredTools: l.makeBuildFilteredTools(req),
		callLLM:            l.makeCallLLM(req),
		pruneMessages:      l.makePruneMessages(),
		compactMessages:    l.makeCompactMessages(),
		runMemoryFlush:     l.makeRunMemoryFlush(),
		executeToolCall:    l.makeExecuteToolCall(req, bridgeRS),
		executeToolRaw:     l.makeExecuteToolRaw(req),
		processToolResult:  l.makeProcessToolResult(req, bridgeRS),
		checkReadOnly:      l.makeCheckReadOnly(req, bridgeRS),
		sanitizeContent:    SanitizeAssistantContent,
		flushMessages:      l.makeFlushMessages(req),
		updateMetadata:     l.makeUpdateMetadata(req),
		bootstrapCleanup:   l.makeBootstrapCleanup(),
		maybeSummarize:     l.maybeSummarize,
	}
}

// pipelineCallbackSet groups all typed callbacks for PipelineDeps.
type pipelineCallbackSet struct {
	injectContext      func(ctx context.Context, input *pipeline.RunInput) (context.Context, error)
	loadSessionHistory func(ctx context.Context, sessionKey string) ([]providers.Message, string)
	resolveWorkspace   func(ctx context.Context, input *pipeline.RunInput) (*workspace.WorkspaceContext, error)
	loadContextFiles   func(ctx context.Context, userID string) ([]bootstrap.ContextFile, bool)
	buildMessages      func(ctx context.Context, input *pipeline.RunInput, history []providers.Message, summary string) ([]providers.Message, error)
	enrichMedia        func(ctx context.Context, state *pipeline.RunState) error
	injectReminders    func(ctx context.Context, input *pipeline.RunInput, msgs []providers.Message) []providers.Message
	buildFilteredTools func(state *pipeline.RunState) ([]providers.ToolDefinition, error)
	callLLM            func(ctx context.Context, state *pipeline.RunState, req providers.ChatRequest) (*providers.ChatResponse, error)
	pruneMessages      func(msgs []providers.Message, budget int) []providers.Message
	compactMessages    func(ctx context.Context, msgs []providers.Message, model string) ([]providers.Message, error)
	runMemoryFlush     func(ctx context.Context, state *pipeline.RunState) error
	executeToolCall    func(ctx context.Context, state *pipeline.RunState, tc providers.ToolCall) ([]providers.Message, error)
	executeToolRaw     func(ctx context.Context, tc providers.ToolCall) (providers.Message, any, error)
	processToolResult  func(ctx context.Context, state *pipeline.RunState, tc providers.ToolCall, rawMsg providers.Message, rawData any) []providers.Message
	checkReadOnly      func(state *pipeline.RunState) (*providers.Message, bool)
	sanitizeContent    func(string) string
	flushMessages      func(ctx context.Context, sessionKey string, msgs []providers.Message) error
	updateMetadata     func(ctx context.Context, sessionKey string, usage providers.Usage) error
	bootstrapCleanup   func(ctx context.Context, state *pipeline.RunState) error
	maybeSummarize     func(ctx context.Context, sessionKey string)
}

func (l *Loop) makeResolveWorkspace(req *RunRequest) func(ctx context.Context, input *pipeline.RunInput) (*workspace.WorkspaceContext, error) {
	resolver := workspace.NewResolver()
	return func(ctx context.Context, input *pipeline.RunInput) (*workspace.WorkspaceContext, error) {
		var teamID *string
		if input.TeamID != "" {
			teamID = &input.TeamID
		}
		return resolver.Resolve(ctx, workspace.ResolveParams{
			AgentID:   l.id,
			AgentType: l.agentType,
			UserID:    input.UserID,
			ChatID:    input.ChatID,
			TenantID:  l.tenantID.String(),
			PeerKind:  input.PeerKind,
			TeamID:    teamID,
			BaseDir:   l.workspace,
		})
	}
}

func (l *Loop) makeLoadContextFiles() func(ctx context.Context, userID string) ([]bootstrap.ContextFile, bool) {
	return func(ctx context.Context, userID string) ([]bootstrap.ContextFile, bool) {
		files := l.resolveContextFiles(ctx, userID)
		hadBootstrap := false
		for _, f := range files {
			if strings.HasSuffix(f.Path, "BOOTSTRAP.md") {
				hadBootstrap = true
				break
			}
		}
		return files, hadBootstrap
	}
}

func (l *Loop) makeBuildMessages() func(ctx context.Context, input *pipeline.RunInput, history []providers.Message, summary string) ([]providers.Message, error) {
	return func(ctx context.Context, input *pipeline.RunInput, history []providers.Message, summary string) ([]providers.Message, error) {
		msgs, _ := l.buildMessages(ctx, history, summary,
			input.Message, input.ExtraSystemPrompt,
			input.SessionKey, input.Channel, input.ChannelType,
			input.ChatTitle, input.PeerKind, input.UserID,
			input.HistoryLimit, input.SkillFilter, input.LightContext)
		return msgs, nil
	}
}

// makeInjectContext wraps injectContext() for the v3 pipeline.
// Reuses the existing injectContext() to avoid logic duplication.
// NOTE: injectContext() and this callback must stay in sync when new context values are added.
func (l *Loop) makeInjectContext(req *RunRequest) func(ctx context.Context, input *pipeline.RunInput) (context.Context, error) {
	return func(ctx context.Context, input *pipeline.RunInput) (context.Context, error) {
		result, err := l.injectContext(ctx, req)
		if err != nil {
			return ctx, err
		}
		// Sync message truncation from req back to pipeline input.
		input.Message = req.Message
		// Cache context window on session (first run only, matching runLoop behavior).
		if l.sessions.GetContextWindow(result.ctx, req.SessionKey) <= 0 {
			l.sessions.SetContextWindow(result.ctx, req.SessionKey, l.contextWindow)
		}
		return result.ctx, nil
	}
}

// makeLoadSessionHistory loads session history + summary before BuildMessages.
func (l *Loop) makeLoadSessionHistory() func(ctx context.Context, sessionKey string) ([]providers.Message, string) {
	return func(ctx context.Context, sessionKey string) ([]providers.Message, string) {
		history := l.sessions.GetHistory(ctx, sessionKey)
		summary := l.sessions.GetSummary(ctx, sessionKey)
		return history, summary
	}
}

func (l *Loop) makeEnrichMedia(req *RunRequest) func(ctx context.Context, state *pipeline.RunState) error {
	return func(ctx context.Context, state *pipeline.RunState) error {
		// enrichInputMedia enriches messages in-place: attaches inline images,
		// reloads historical media, enriches <media:*> tags, populates context
		// with refs for tool access. Must receive actual messages (not nil) to
		// avoid index-out-of-range panic on inline image attachment.
		msgs := state.Messages.All()
		if len(msgs) == 0 {
			return nil
		}
		enrichedCtx, enrichedMsgs, _ := l.enrichInputMedia(ctx, req, msgs)
		// Propagate enriched context (media images/docs/audio/video refs for tools).
		state.Ctx = enrichedCtx
		// Update history with enriched messages (media tags, inline images).
		// Skip system message (index 0) — only history + user messages are enriched.
		if len(enrichedMsgs) > 1 {
			state.Messages.SetHistory(enrichedMsgs[1:])
		}
		return nil
	}
}

func (l *Loop) makeInjectReminders(req *RunRequest) func(ctx context.Context, input *pipeline.RunInput, msgs []providers.Message) []providers.Message {
	return func(ctx context.Context, input *pipeline.RunInput, msgs []providers.Message) []providers.Message {
		updated, _ := l.injectTeamTaskReminders(ctx, req, msgs)
		return updated
	}
}

func (l *Loop) makeBuildFilteredTools(req *RunRequest) func(state *pipeline.RunState) ([]providers.ToolDefinition, error) {
	return func(state *pipeline.RunState) ([]providers.ToolDefinition, error) {
		maxIter := l.maxIterations
		if req.MaxIterations > 0 && req.MaxIterations < maxIter {
			maxIter = req.MaxIterations
		}
		allMsgs := state.Messages.All()
		toolDefs, _, returnedMsgs := l.buildFilteredTools(req, state.Context.HadBootstrap,
			state.Iteration, maxIter, allMsgs)
		// buildFilteredTools returns the full messages slice; only messages appended
		// beyond the original length are injections (e.g. final-iteration hint).
		// Appending the entire slice would duplicate system+history into pending.
		if len(returnedMsgs) > len(allMsgs) {
			for _, msg := range returnedMsgs[len(allMsgs):] {
				state.Messages.AppendPending(msg)
			}
		}
		return toolDefs, nil
	}
}

func (l *Loop) makeCallLLM(req *RunRequest) func(ctx context.Context, state *pipeline.RunState, chatReq providers.ChatRequest) (*providers.ChatResponse, error) {
	// emitRun enriches events with routing context so channels + WS can route them.
	emitRun := func(event AgentEvent) {
		event.RunKind = req.RunKind
		event.DelegationID = req.DelegationID
		event.TeamID = req.TeamID
		event.TeamTaskID = req.TeamTaskID
		event.ParentAgentID = req.ParentAgentID
		event.UserID = req.UserID
		event.Channel = req.Channel
		event.ChatID = req.ChatID
		event.SessionKey = req.SessionKey
		event.TenantID = l.tenantID
		l.emit(event)
	}

	return func(ctx context.Context, state *pipeline.RunState, chatReq providers.ChatRequest) (*providers.ChatResponse, error) {
		provider := state.Provider

		// Emit LLM span start (matching runLoop tracing behavior).
		start := time.Now().UTC()
		var opts []spanOption
		if state.Model != "" {
			opts = append(opts, withModel(state.Model))
		}
		if provider != nil {
			opts = append(opts, withProvider(provider.Name()))
		}
		spanID := l.emitLLMSpanStart(ctx, start, state.Iteration, chatReq.Messages, opts...)

		var resp *providers.ChatResponse
		var err error
		if req.Stream {
			resp, err = provider.ChatStream(ctx, chatReq, func(chunk providers.StreamChunk) {
				if chunk.Thinking != "" {
					emitRun(AgentEvent{
						Type:    protocol.ChatEventThinking,
						AgentID: l.id,
						RunID:   req.RunID,
						Payload: map[string]string{"content": chunk.Thinking},
					})
				}
				if chunk.Content != "" {
					emitRun(AgentEvent{
						Type:    protocol.ChatEventChunk,
						AgentID: l.id,
						RunID:   req.RunID,
						Payload: map[string]string{"content": chunk.Content},
					})
				}
			})
		} else {
			resp, err = provider.Chat(ctx, chatReq)
		}

		// Non-streaming: emit content events matching v2 behavior (channels need these).
		if !req.Stream && err == nil && resp != nil {
			if resp.Thinking != "" {
				emitRun(AgentEvent{
					Type:    protocol.ChatEventThinking,
					AgentID: l.id,
					RunID:   req.RunID,
					Payload: map[string]string{"content": resp.Thinking},
				})
			}
			if resp.Content != "" {
				emitRun(AgentEvent{
					Type:    protocol.ChatEventChunk,
					AgentID: l.id,
					RunID:   req.RunID,
					Payload: map[string]string{"content": resp.Content},
				})
			}
		}

		l.emitLLMSpanEnd(ctx, spanID, start, resp, err, opts...)
		return resp, err
	}
}

func (l *Loop) makePruneMessages() func(msgs []providers.Message, budget int) []providers.Message {
	return func(msgs []providers.Message, budget int) []providers.Message {
		return pruneContextMessages(msgs, budget, l.contextPruningCfg)
	}
}

func (l *Loop) makeCompactMessages() func(ctx context.Context, msgs []providers.Message, model string) ([]providers.Message, error) {
	return func(ctx context.Context, msgs []providers.Message, model string) ([]providers.Message, error) {
		compacted := l.compactMessagesInPlace(ctx, msgs)
		if compacted == nil {
			return msgs, nil // compaction failed, return original
		}
		return compacted, nil
	}
}

func (l *Loop) makeRunMemoryFlush() func(ctx context.Context, state *pipeline.RunState) error {
	return func(ctx context.Context, state *pipeline.RunState) error {
		settings := ResolveMemoryFlushSettings(l.compactionCfg)
		if settings == nil {
			return nil
		}
		l.runMemoryFlush(ctx, state.Input.SessionKey, settings)
		return nil
	}
}

func (l *Loop) makeFlushMessages(req *RunRequest) func(ctx context.Context, sessionKey string, msgs []providers.Message) error {
	// Track whether user message has been persisted (first flush only).
	// v2 adds user message to pendingMsgs explicitly; v3 keeps it in history
	// (via BuildMessages) so it never reaches FlushPending. This closure
	// persists the user message on first flush to match v2 session format.
	var userMsgFlushed bool
	return func(ctx context.Context, sessionKey string, msgs []providers.Message) error {
		if !userMsgFlushed && !req.HideInput && req.Message != "" {
			userMsgFlushed = true
			l.sessions.AddMessage(ctx, sessionKey, providers.Message{
				Role:    "user",
				Content: req.Message,
			})
		}
		for _, msg := range msgs {
			l.sessions.AddMessage(ctx, sessionKey, msg)
		}
		return nil
	}
}

func (l *Loop) makeUpdateMetadata(req *RunRequest) func(ctx context.Context, sessionKey string, usage providers.Usage) error {
	return func(ctx context.Context, sessionKey string, usage providers.Usage) error {
		l.sessions.UpdateMetadata(ctx, sessionKey, l.model, l.provider.Name(), req.Channel)
		l.sessions.AccumulateTokens(ctx, sessionKey, int64(usage.PromptTokens), int64(usage.CompletionTokens))
		// Persist session to DB (matching v2 finalizeRun behavior).
		// FlushMessages already ran, so all pending messages are in the cache.
		l.sessions.Save(ctx, sessionKey)
		return nil
	}
}

func (l *Loop) makeBootstrapCleanup() func(ctx context.Context, state *pipeline.RunState) error {
	return func(ctx context.Context, state *pipeline.RunState) error {
		if l.bootstrapCleanup == nil {
			return nil
		}
		return l.bootstrapCleanup(ctx, l.agentUUID, state.Input.UserID)
	}
}

