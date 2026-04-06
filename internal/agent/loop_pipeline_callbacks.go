package agent

import (
	"context"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/pipeline"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/workspace"
)

// pipelineCallbacks creates all callback closures that capture *Loop.
// Each callback bridges a pipeline.PipelineDeps function to an existing Loop method.
func (l *Loop) pipelineCallbacks(req *RunRequest, bridgeRS *runState) pipelineCallbackSet {
	return pipelineCallbackSet{
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
		flushMessages:      l.makeFlushMessages(),
		updateMetadata:     l.makeUpdateMetadata(req),
		bootstrapCleanup:   l.makeBootstrapCleanup(),
		maybeSummarize:     l.maybeSummarize,
	}
}

// pipelineCallbackSet groups all typed callbacks for PipelineDeps.
type pipelineCallbackSet struct {
	resolveWorkspace   func(ctx context.Context, input *pipeline.RunInput) (*workspace.WorkspaceContext, error)
	loadContextFiles   func(ctx context.Context, userID string) ([]bootstrap.ContextFile, bool)
	buildMessages      func(ctx context.Context, input *pipeline.RunInput, history []providers.Message) ([]providers.Message, error)
	enrichMedia        func(ctx context.Context, input *pipeline.RunInput) error
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

func (l *Loop) makeBuildMessages() func(ctx context.Context, input *pipeline.RunInput, history []providers.Message) ([]providers.Message, error) {
	return func(ctx context.Context, input *pipeline.RunInput, history []providers.Message) ([]providers.Message, error) {
		summary := "" // summarization handled separately
		msgs, _ := l.buildMessages(ctx, history, summary,
			input.Message, input.ExtraSystemPrompt,
			input.SessionKey, input.Channel, input.ChannelType,
			input.ChatTitle, input.PeerKind, input.UserID,
			input.HistoryLimit, input.SkillFilter, input.LightContext)
		return msgs, nil
	}
}

func (l *Loop) makeEnrichMedia(req *RunRequest) func(ctx context.Context, input *pipeline.RunInput) error {
	return func(ctx context.Context, input *pipeline.RunInput) error {
		// enrichInputMedia returns updated context, messages, and media refs.
		// The messages contain vision-injected content. For now we enrich the req
		// in place so buildMessages picks up media changes on its next call.
		// Full pipeline-native media handling is a follow-up.
		_, _, _ = l.enrichInputMedia(ctx, req, nil)
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
		toolDefs, _, injectedMsgs := l.buildFilteredTools(req, state.Context.HadBootstrap,
			state.Iteration, maxIter, state.Messages.All())
		// Append tool-awareness messages (e.g., dynamic tool hints) to pending buffer
		for _, msg := range injectedMsgs {
			state.Messages.AppendPending(msg)
		}
		return toolDefs, nil
	}
}

func (l *Loop) makeCallLLM(req *RunRequest) func(ctx context.Context, state *pipeline.RunState, chatReq providers.ChatRequest) (*providers.ChatResponse, error) {
	return func(ctx context.Context, state *pipeline.RunState, chatReq providers.ChatRequest) (*providers.ChatResponse, error) {
		provider := state.Provider
		if req.Stream {
			return provider.ChatStream(ctx, chatReq, func(chunk providers.StreamChunk) {
				// Streaming chunks emitted via existing event system
				if l.onEvent != nil {
					l.onEvent(AgentEvent{Type: "chunk", Payload: chunk})
				}
			})
		}
		return provider.Chat(ctx, chatReq)
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

func (l *Loop) makeFlushMessages() func(ctx context.Context, sessionKey string, msgs []providers.Message) error {
	return func(ctx context.Context, sessionKey string, msgs []providers.Message) error {
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

