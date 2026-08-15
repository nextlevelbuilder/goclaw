package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
	usagecaps "github.com/nextlevelbuilder/goclaw/internal/usage/caps"
)

// announceTask publishes terminal state after child-run execution capacity has
// been released. Slow bus consumers and callbacks must never hold admission.
func (sm *SubagentManager) announceTask(
	ctx context.Context,
	task *SubagentTask,
	callback AsyncCallback,
	iterations int,
	terminalPersisted bool,
) {
	// Announce result to parent via bus (matching TS subagent-announce.ts pattern).
	// The announce goes through the parent agent's session so the agent can
	// reformulate the result for the user.
	if sm.msgBus != nil && task.OriginChannel != "" {
		elapsed := time.Since(time.UnixMilli(task.CreatedAt))

		item := AnnounceQueueItem{
			SubagentID:       task.ID,
			CompletionID:     task.dbID,
			DurablyPersisted: terminalPersisted,
			ParentTaskID:     task.ParentTaskID,
			Depth:            task.Depth,
			Label:            task.Label,
			Status:           task.Status,
			Result:           task.Result,
			Media:            task.Media,
			Runtime:          elapsed,
			Iterations:       iterations,
			InputTokens:      task.TotalInputTokens,
			OutputTokens:     task.TotalOutputTokens,
		}
		meta := AnnounceMetadata{
			OriginChannel:    task.OriginChannel,
			OriginChatID:     task.OriginChatID,
			OriginPeerKind:   task.OriginPeerKind,
			OriginLocalKey:   task.OriginLocalKey,
			OriginUserID:     task.OriginUserID,
			OriginSenderID:   task.OriginSenderID,
			OriginRole:       task.OriginRole,
			OriginSessionKey: task.OriginSessionKey,
			OriginTenantID:   task.OriginTenantID,
			RootAgentID:      task.RootAgentID,
			ParentAgent:      task.RootAgentKey,
			OriginTraceID:    task.OriginTraceID.String(),
			OriginRootSpanID: task.OriginRootSpanID.String(),
		}

		if sm.announceQueue != nil {
			// Use batched announce queue (matching TS debounce pattern)
			sessionKey := subagentAnnounceBatchKey(task)
			sm.announceQueue.Enqueue(sessionKey, item, meta)
		} else {
			// Direct publish (no batching)
			roster := sm.RosterForParent(TaskScope{
				TenantID: task.OriginTenantID, RootAgentID: task.RootAgentID, RootAgentKey: task.RootAgentKey,
			})
			announceContent := FormatBatchedAnnounce([]AnnounceQueueItem{item}, roster)

			announceMeta := map[string]string{
				MetaOriginChannel:       task.OriginChannel,
				MetaOriginPeerKind:      task.OriginPeerKind,
				MetaParentAgent:         task.RootAgentKey,
				MetaSubagentRootAgentID: task.RootAgentID.String(),
				"subagent_id":           task.ID,
				MetaSubagentLabel:       task.Label,
				MetaSubagentStatus:      task.Status,
				MetaSubagentResult:      task.Result,
				MetaSubagentRuntime:     fmt.Sprintf("%d", elapsed.Milliseconds()),
				MetaSubagentIterations:  fmt.Sprintf("%d", iterations),
				MetaSubagentInputToks:   fmt.Sprintf("%d", task.TotalInputTokens),
				MetaSubagentOutputToks:  fmt.Sprintf("%d", task.TotalOutputTokens),
				MetaSubagentParentTask:  task.ParentTaskID,
				MetaSubagentDepth:       fmt.Sprintf("%d", task.Depth),
				MetaOriginTraceID:       task.OriginTraceID.String(),
				MetaOriginRootSpanID:    task.OriginRootSpanID.String(),
			}
			if task.OriginLocalKey != "" {
				announceMeta[MetaOriginLocalKey] = task.OriginLocalKey
			}
			if task.OriginSessionKey != "" {
				announceMeta[MetaOriginSessionKey] = task.OriginSessionKey
			}
			if task.OriginSenderID != "" {
				announceMeta[MetaOriginSenderID] = task.OriginSenderID
			}
			if task.OriginRole != "" {
				announceMeta[MetaOriginRole] = task.OriginRole
			}
			if task.OriginUserID != "" {
				announceMeta[MetaOriginUserID] = task.OriginUserID
			}
			delivered := PublishAsyncCompletion(ctx, sm.msgBus, bus.InboundMessage{
				Channel:  "system",
				SenderID: fmt.Sprintf("subagent:%s", task.ID),
				ChatID:   task.OriginChatID,
				Content:  announceContent,
				UserID:   task.OriginUserID,
				TenantID: task.OriginTenantID,
				Metadata: announceMeta,
				Media:    task.Media,
			})
			if terminalPersisted {
				sm.UpdateAnnouncementStatus(ctx, task.RootAgentID, task.dbID, delivered)
			} else {
				slog.Error("subagent.announce_without_durable_terminal",
					"task_id", task.ID,
					"completion_id", task.dbID,
					"root_agent_id", task.RootAgentID,
					"delivered", delivered,
				)
			}
			if !delivered {
				slog.Warn("subagent.announce_deferred_to_ledger",
					"task_id", task.ID,
					"completion_id", task.dbID,
					"root_agent_id", task.RootAgentID,
					"reason", "inbound_bus_full",
				)
			}
		}
	}

	// Call completion callback
	if callback != nil {
		result := NewResult(fmt.Sprintf("Subagent '%s' completed in %d iterations.\n\nResult:\n%s",
			task.Label, iterations, task.Result))
		callback(ctx, result)
	}
}

func subagentAnnounceBatchKey(task *SubagentTask) string {
	return strings.Join([]string{
		"announce",
		task.OriginTenantID.String(),
		task.RootAgentID.String(),
		task.RootAgentKey,
		task.OriginSessionKey,
		task.OriginChannel,
		task.OriginChatID,
		task.OriginPeerKind,
		task.OriginLocalKey,
		task.OriginUserID,
		task.OriginSenderID,
		task.OriginRole,
	}, "\x00")
}

// executeTask runs the LLM tool loop for a subagent. Returns iteration count.
func (sm *SubagentManager) executeTask(ctx context.Context, task *SubagentTask) int {
	// Tracing: generate a root span ID for this subagent execution.
	// LLM/tool spans will nest under this root span via parent_span_id.
	// The root span itself links to the parent agent's root span (from ctx).
	subRootSpanID := store.GenNewID()
	taskStart := time.Now().UTC()

	// Detach cancellation while preserving the delegation redactor and the
	// original trace parent. Rebuilding from Background would silently drop the
	// artifact confidentiality boundary on nested synchronous work.
	traceCtx := context.WithoutCancel(ctx)

	// subCtx overrides parent_span_id so child spans nest under subRootSpanID.
	// traceCtx retains the original parent_span_id for the root subagent span.
	subTraceCtx := tracing.WithParentSpanID(traceCtx, subRootSpanID)
	toolCtx := tracing.WithParentSpanID(ctx, subRootSpanID)

	var model string
	var finalContent string
	iteration := 0

	defer func() {
		sm.mu.Lock()
		task.CompletedAt = time.Now().UnixMilli()
		sm.mu.Unlock()

		// Finalize root subagent span on exit (uses traceCtx which is never cancelled).
		sm.emitSubagentSpanEnd(traceCtx, subRootSpanID, taskStart, task, finalContent)
		slog.Debug("subagent tracing: root span finalized",
			"id", task.ID, "span_id", subRootSpanID,
			"trace_id", tracing.TraceIDFromContext(traceCtx),
			"status", task.Status, "iterations", iteration)

	}()

	if ctx.Err() != nil {
		sm.mu.Lock()
		task.Status = TaskStatusCancelled
		task.Result = "cancelled before execution"
		sm.mu.Unlock()
		return 0
	}

	// Build tools for subagent (no spawn/subagent tools to prevent recursion)
	toolsReg := sm.createTools()
	toolsReg.Register(NewSpawnTool(sm, task.RootAgentKey, task.Depth))
	sm.applyDenyList(toolsReg, task.Depth, task.spawnConfig)

	// Determine model (cascading priority):
	// 1. Per-task model override (highest — LLM specified model in spawn call)
	// 2. SubagentConfig.Model (agent-level subagent override)
	// 3. Parent agent's model (inherit from the agent that spawned us)
	// 4. SubagentManager default model (system-wide fallback)
	model = sm.model
	if parentModel := ParentModelFromCtx(ctx); parentModel != "" {
		model = parentModel
	}
	if task.spawnConfig.Model != "" {
		model = task.spawnConfig.Model
	}
	if task.Model != "" {
		model = task.Model
	}

	// Determine provider (cascading priority):
	// 1. Parent agent's provider (inherit so model/provider combo stays valid)
	// 2. SubagentManager default provider (system-wide fallback)
	activeProvider := sm.provider
	if sm.providerReg != nil {
		if parentProviderName := ParentProviderFromCtx(ctx); parentProviderName != "" {
			if p, err := sm.providerReg.Get(ctx, parentProviderName); err == nil {
				activeProvider = p
			}
		}
	}

	// Emit running subagent root span (after model resolution so span has correct model).
	sm.emitSubagentSpanStart(traceCtx, subRootSpanID, taskStart, task, model, activeProvider.Name())

	// Build subagent system prompt (matching TS buildSubagentSystemPrompt pattern).
	workspace := ToolWorkspaceFromCtx(ctx)
	promptWorkspace := workspace
	if IsDelegationArtifactRun(ctx) {
		promptWorkspace = "outputs/"
	}
	systemPrompt := tracing.RedactText(ctx, sm.buildSubagentSystemPrompt(task, task.spawnConfig, promptWorkspace))

	messages := []providers.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: tracing.RedactText(ctx, task.Task)},
	}

	// Run LLM iteration loop (similar to agent loop but simplified)
	var mediaFiles []bus.MediaFile
	maxIterations := 20

	for iteration < maxIterations {
		iteration++

		if ctx.Err() != nil {
			sm.mu.Lock()
			task.Status = TaskStatusCancelled
			task.Result = "cancelled during execution"
			sm.mu.Unlock()
			return iteration
		}

		chatReq := providers.ChatRequest{
			Messages: messages,
			Tools:    toolsReg.ProviderDefs(),
			Model:    model,
			Options: map[string]any{
				"max_tokens":  4096,
				"temperature": 0.5,
			},
		}

		llmStart := time.Now().UTC()
		llmSpanID := sm.emitLLMSpanStart(subTraceCtx, llmStart, iteration, model, activeProvider.Name(), messages)

		maxRetries := task.spawnConfig.MaxRetries
		if maxRetries <= 0 {
			maxRetries = 2
		}
		var resp *providers.ChatResponse
		var err error
		for attempt := 0; attempt <= maxRetries; attempt++ {
			if attempt > 0 {
				backoff := time.Duration(attempt) * 2 * time.Second
				select {
				case <-ctx.Done():
				case <-time.After(backoff):
				}
				if ctx.Err() != nil {
					break
				}
				slog.Info("subagent LLM retry", "id", task.ID, "iteration", iteration, "attempt", attempt+1)
			}
			resp, err = sm.chatSubagentWithUsageCap(ctx, task, activeProvider, model, chatReq, iteration, attempt+1)
			if err == nil {
				break
			}
		}

		sm.emitLLMSpanEnd(subTraceCtx, llmSpanID, llmStart, resp, err)

		// Accumulate token usage for cost tracking.
		if resp != nil && resp.Usage != nil {
			sm.mu.Lock()
			task.TotalInputTokens += int64(resp.Usage.PromptTokens)
			task.TotalOutputTokens += int64(resp.Usage.CompletionTokens)
			sm.mu.Unlock()
		}

		if err != nil {
			sm.mu.Lock()
			task.Status = TaskStatusFailed
			task.Result = tracing.RedactText(ctx, fmt.Sprintf("LLM error at iteration %d: %v", iteration, err))
			sm.mu.Unlock()
			slog.Warn("subagent LLM error", "id", task.ID, "iteration", iteration, "error", err)
			return iteration
		}

		// No tool calls → done
		if len(resp.ToolCalls) == 0 {
			finalContent = resp.Content
			break
		}

		// Build assistant message
		assistantMsg := providers.Message{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		}
		messages = append(messages, assistantMsg)

		// Execute tools
		for _, tc := range resp.ToolCalls {
			slog.Debug("subagent tool call", "id", task.ID, "tool", tc.Name)

			argsJSON, _ := json.Marshal(tc.Arguments)
			toolStart := time.Now().UTC()
			toolSpanID := sm.emitToolSpanStart(subTraceCtx, toolStart, tc.Name, tc.ID, string(argsJSON))
			result := toolsReg.Execute(toolCtx, tc.Name, tc.Arguments)
			result.ForLLM = tracing.RedactText(ctx, result.ForLLM)
			sm.emitToolSpanEnd(subTraceCtx, toolSpanID, toolStart, result.ForLLM, result.IsError)

			// Capture media file paths from tool results (e.g. image generation).
			if len(result.Media) > 0 {
				mediaFiles = append(mediaFiles, result.Media...)
			} else if strings.HasPrefix(strings.TrimSpace(result.ForLLM), "MEDIA:") {
				// Fallback: parse MEDIA: prefix from ForLLM (same as agent loop's parseMediaResult)
				p := strings.TrimSpace(strings.TrimSpace(result.ForLLM)[6:])
				if nl := strings.IndexByte(p, '\n'); nl >= 0 {
					p = strings.TrimSpace(p[:nl])
				}
				if p != "" {
					mediaFiles = append(mediaFiles, bus.MediaFile{Path: p, Filename: filepath.Base(p)})
				}
			}

			messages = append(messages, providers.Message{
				Role:       "tool",
				Content:    result.ForLLM,
				ToolCallID: tc.ID,
				ToolName:   tc.Name,
			})
		}
	}

	sm.mu.Lock()
	if task.Status != TaskStatusCancelled {
		if finalContent == "" {
			finalContent = "Task completed but no final response was generated."
		}
		finalContent = tracing.RedactText(ctx, finalContent)
		task.Status = TaskStatusCompleted
		task.Result = finalContent
		task.Media = mediaFiles
	}
	sm.mu.Unlock()

	slog.Info("subagent completed", "id", task.ID, "iterations", iteration)

	return iteration
}

func (sm *SubagentManager) chatSubagentWithUsageCap(ctx context.Context, task *SubagentTask, activeProvider providers.Provider, model string, chatReq providers.ChatRequest, iteration, attempt int) (*providers.ChatResponse, error) {
	budget := usagecaps.AgentBudget{
		ContextWindow: task.OriginContextWindow,
		MaxTokens:     task.OriginMaxTokens,
	}
	if budget.ContextWindow <= 0 {
		budget.ContextWindow = sm.contextWindow
	}
	if budget.MaxTokens <= 0 {
		budget.MaxTokens = sm.maxTokens
	}
	chatReq = clampToolRequestMaxTokens(chatReq, budget.MaxTokens)
	if fallbackProvider, ok := activeProvider.(*providers.ModelFallbackProvider); ok {
		before := func(callCtx context.Context, entry providers.FallbackCandidate, actualReq providers.ChatRequest) (providers.FallbackAfterCall, error) {
			// Guard this fallback request with the calling agent budget.
			if guardErr := usagecaps.GuardContextWindow(clampToolRequestMaxTokens(actualReq, budget.MaxTokens), entry.ProviderName, actualReq.Model, "subagent:"+task.ID, budget); guardErr != nil {
				return nil, guardErr
			}
			reservation, err := sm.reserveSubagentUsage(callCtx, task, entry.ProviderName, actualReq.Model, actualReq, fmt.Sprintf("%d:%d:%s:%s", iteration, attempt, entry.ProviderName, actualReq.Model))
			if err != nil {
				return nil, err
			}
			return func(resp *providers.ChatResponse, callErr error, _ providers.FallbackCallInfo) {
				if reservation != nil {
					reservation.Reconcile(callCtx, resp, callErr)
				}
			}, nil
		}
		return fallbackProvider.ChatWithHook(ctx, chatReq, before)
	}
	// Guard the non-fallback request with the calling agent budget.
	if guardErr := usagecaps.GuardContextWindow(chatReq, activeProvider.Name(), model, "subagent:"+task.ID, budget); guardErr != nil {
		return nil, guardErr
	}
	reservation, err := sm.reserveSubagentUsage(ctx, task, activeProvider.Name(), model, chatReq, fmt.Sprintf("%d:%d", iteration, attempt))
	if err != nil {
		return nil, err
	}
	resp, err := activeProvider.Chat(ctx, chatReq)
	if reservation != nil {
		reservation.Reconcile(ctx, resp, err)
	}
	return resp, err
}

func (sm *SubagentManager) reserveSubagentUsage(ctx context.Context, task *SubagentTask, providerName, model string, chatReq providers.ChatRequest, suffix string) (*usagecaps.Reservation, error) {
	if sm.usageCaps == nil {
		return nil, nil
	}
	return sm.usageCaps.Preflight(ctx, usagecaps.Request{
		TenantID:        task.OriginTenantID,
		AgentID:         task.OriginAgentID,
		ProviderName:    providerName,
		ModelID:         model,
		ReservationKey:  fmt.Sprintf("%s:%s", task.ID, suffix),
		Messages:        chatReq.Messages,
		MaxOutputTokens: 4096,
	})
}
