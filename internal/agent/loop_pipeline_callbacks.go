package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

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
	checkReadOnly      func(state *pipeline.RunState) (*providers.Message, bool)
	sanitizeContent    func(string) string
	flushMessages      func(ctx context.Context, sessionKey string, msgs []providers.Message) error
	updateMetadata     func(ctx context.Context, sessionKey string, usage providers.Usage) error
	bootstrapCleanup   func(ctx context.Context, state *pipeline.RunState) error
	maybeSummarize     func(ctx context.Context, sessionKey string)
}

func (l *Loop) makeResolveWorkspace(req *RunRequest) func(ctx context.Context, input *pipeline.RunInput) (*workspace.WorkspaceContext, error) {
	return func(ctx context.Context, input *pipeline.RunInput) (*workspace.WorkspaceContext, error) {
		// Delegate to WorkspaceResolver if available, else return nil (adapter fills later)
		return nil, nil // TODO: wire workspace.Resolver when v3 workspace is active
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

// makeExecuteToolCall wraps tool execution: name resolution, policy check, execute, process result.
// Uses bridgeRS to share loop detection state between the pipeline and agent's processToolResult.
func (l *Loop) makeExecuteToolCall(req *RunRequest, bridgeRS *runState) func(ctx context.Context, state *pipeline.RunState, tc providers.ToolCall) ([]providers.Message, error) {
	emitRun := func(event AgentEvent) {
		event.RunKind = req.RunKind
		event.SessionKey = req.SessionKey
		event.UserID = req.UserID
		event.Channel = req.Channel
		l.emit(event)
	}

	return func(ctx context.Context, state *pipeline.RunState, tc providers.ToolCall) ([]providers.Message, error) {
		registryName := l.resolveToolCallName(tc.Name)
		argsJSON, _ := json.Marshal(tc.Arguments)
		slog.Info("tool call", "agent", l.id, "tool", tc.Name, "args_len", len(argsJSON))

		// Emit tool.call event
		emitRun(AgentEvent{
			Type:    protocol.AgentEventToolCall,
			AgentID: l.id,
			RunID:   state.RunID,
			Payload: map[string]any{"name": tc.Name, "id": tc.ID, "arguments": tc.Arguments},
		})

		// Execute tool via registry (policy filtering already done by BuildFilteredTools in ThinkStage)
		result := l.tools.ExecuteWithContext(ctx, registryName, tc.Arguments,
			req.Channel, req.ChatID, req.PeerKind, req.SessionKey, nil)

		// Process result via existing processToolResult (uses bridgeRS for loop detection)
		toolMsg, warningMsgs, action := l.processToolResult(ctx, bridgeRS, req, emitRun, tc, registryName, result, state.Context.HadBootstrap)

		// Sync side effects back to pipeline RunState
		state.Tool.LoopKilled = bridgeRS.loopKilled
		state.Tool.AsyncToolCalls = bridgeRS.asyncToolCalls
		for _, mr := range bridgeRS.mediaResults[len(state.Tool.MediaResults):] {
			state.Tool.MediaResults = append(state.Tool.MediaResults, pipeline.MediaResult{
				Path: mr.Path, ContentType: mr.ContentType, Size: mr.Size, AsVoice: mr.AsVoice,
			})
		}
		state.Tool.Deliverables = bridgeRS.deliverables
		state.Evolution.BootstrapWrite = bridgeRS.bootstrapWriteDetected
		state.Evolution.TeamTaskSpawns = bridgeRS.teamTaskSpawns

		if state.Tool.LoopKilled && action == toolResultBreak {
			state.Observe.FinalContent = bridgeRS.finalContent
		}

		var msgs []providers.Message
		msgs = append(msgs, toolMsg)
		msgs = append(msgs, warningMsgs...)
		return msgs, nil
	}
}

// makeCheckReadOnly wraps read-only streak detection using the bridged runState.
func (l *Loop) makeCheckReadOnly(req *RunRequest, bridgeRS *runState) func(state *pipeline.RunState) (*providers.Message, bool) {
	return func(state *pipeline.RunState) (*providers.Message, bool) {
		warnMsg, shouldBreak := l.checkReadOnlyStreak(bridgeRS, req)
		if shouldBreak {
			state.Tool.LoopKilled = bridgeRS.loopKilled
			state.Observe.FinalContent = bridgeRS.finalContent
		}
		return warnMsg, shouldBreak
	}
}
