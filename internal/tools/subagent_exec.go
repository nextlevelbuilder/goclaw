package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
)

// runTask executes the subagent in a goroutine.
func (sm *SubagentManager) runTask(ctx context.Context, task *SubagentTask, callback AsyncCallback) {
	iterations := sm.executeTask(ctx, task)

	// Announce result to parent via bus (matching TS subagent-announce.ts pattern).
	// The announce goes through the parent agent's session so the agent can
	// reformulate the result for the user.
	if sm.msgBus != nil && task.OriginChannel != "" {
		elapsed := time.Since(time.UnixMilli(task.CreatedAt))

		item := AnnounceQueueItem{
			SubagentID: task.ID,
			Label:      task.Label,
			Status:     task.Status,
			Result:     task.Result,
			Media:      task.Media,
			Runtime:    elapsed,
			Iterations: iterations,
		}
		meta := AnnounceMetadata{
			OriginChannel:    task.OriginChannel,
			OriginChatID:     task.OriginChatID,
			OriginPeerKind:   task.OriginPeerKind,
			OriginLocalKey:   task.OriginLocalKey,
			OriginUserID:     task.OriginUserID,
			OriginSessionKey: task.OriginSessionKey,
			ParentAgent:      task.ParentID,
			OriginTraceID:    task.OriginTraceID.String(),
			OriginRootSpanID: task.OriginRootSpanID.String(),
		}

		if sm.announceQueue != nil {
			// Use batched announce queue (matching TS debounce pattern)
			sessionKey := fmt.Sprintf("announce:%s:%s", task.ParentID, task.OriginChatID)
			sm.announceQueue.Enqueue(sessionKey, item, meta)
		} else {
			// Direct publish (no batching)
			roster := sm.RosterForParent(task.ParentID)
			announceContent := FormatBatchedAnnounce([]AnnounceQueueItem{item}, roster)

			announceMeta := map[string]string{
				"origin_channel":      task.OriginChannel,
				"origin_peer_kind":    task.OriginPeerKind,
				"parent_agent":        task.ParentID,
				"subagent_id":         task.ID,
				"subagent_label":      task.Label,
				"origin_trace_id":     task.OriginTraceID.String(),
				"origin_root_span_id": task.OriginRootSpanID.String(),
			}
			if task.OriginLocalKey != "" {
				announceMeta["origin_local_key"] = task.OriginLocalKey
			}
			if task.OriginSessionKey != "" {
				announceMeta["origin_session_key"] = task.OriginSessionKey
			}
			sm.msgBus.PublishInbound(bus.InboundMessage{
				Channel:  "system",
				SenderID: fmt.Sprintf("subagent:%s", task.ID),
				ChatID:   task.OriginChatID,
				Content:  announceContent,
				UserID:   task.OriginUserID,
				Metadata: announceMeta,
				Media:    task.Media,
			})
		}
	}

	// Call completion callback
	if callback != nil {
		result := NewResult(fmt.Sprintf("Subagent '%s' completed in %d iterations.\n\nResult:\n%s",
			task.Label, iterations, task.Result))
		callback(ctx, result)
	}
}

// executeTask runs the LLM tool loop for a subagent. Returns iteration count.
func (sm *SubagentManager) executeTask(ctx context.Context, task *SubagentTask) int {
	// Tracing: generate a root span ID for this subagent execution.
	// LLM/tool spans will nest under this root span via parent_span_id.
	// The root span itself links to the parent agent's root span (from ctx).
	subRootSpanID := store.GenNewID()
	taskStart := time.Now().UTC()

	// Use a detached context for tracing so spans are emitted even if parent ctx is cancelled.
	// We copy tracing values but remove the cancellation chain.
	traceCtx := context.Background()
	if collector := tracing.CollectorFromContext(ctx); collector != nil {
		traceCtx = tracing.WithCollector(traceCtx, collector)
		traceCtx = tracing.WithTraceID(traceCtx, tracing.TraceIDFromContext(ctx))
		// Keep original parent_span_id (parent agent's root span) for the subagent root span.
		traceCtx = tracing.WithParentSpanID(traceCtx, tracing.ParentSpanIDFromContext(ctx))
	}

	// subCtx overrides parent_span_id so child spans nest under subRootSpanID.
	// traceCtx retains the original parent_span_id for the root subagent span.
	subTraceCtx := tracing.WithParentSpanID(traceCtx, subRootSpanID)

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

		// Schedule auto-archive
		if task.spawnConfig.ArchiveAfterMinutes > 0 {
			go sm.scheduleArchive(task.ID, time.Duration(task.spawnConfig.ArchiveAfterMinutes)*time.Minute)
		}
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
			if p, err := sm.providerReg.Get(parentProviderName); err == nil {
				activeProvider = p
			}
		}
	}

	// Emit running subagent root span (after model resolution so span has correct model).
	sm.emitSubagentSpanStart(traceCtx, subRootSpanID, taskStart, task, model, activeProvider.Name())

	// Build subagent system prompt (matching TS buildSubagentSystemPrompt pattern).
	workspace := ToolWorkspaceFromCtx(ctx)
	systemPrompt := sm.buildSubagentSystemPrompt(task, task.spawnConfig, workspace)

	messages := []providers.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: task.Task},
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
		resp, err := activeProvider.Chat(ctx, chatReq)
		sm.emitLLMSpanEnd(subTraceCtx, llmSpanID, llmStart, resp, err)

		if err != nil {
			sm.mu.Lock()
			task.Status = TaskStatusFailed
			task.Result = fmt.Sprintf("LLM error at iteration %d: %v", iteration, err)
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
			result := toolsReg.Execute(ctx, tc.Name, tc.Arguments)
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
					mediaFiles = append(mediaFiles, bus.MediaFile{Path: p})
				}
			}

			messages = append(messages, providers.Message{
				Role:       "tool",
				Content:    result.ForLLM,
				ToolCallID: tc.ID,
			})
		}
	}

	sm.mu.Lock()
	if finalContent == "" {
		finalContent = "Task completed but no final response was generated."
	}
	task.Status = TaskStatusCompleted
	task.Result = finalContent
	task.Media = mediaFiles
	sm.mu.Unlock()

	slog.Info("subagent completed", "id", task.ID, "iterations", iteration)
	return iteration
}
