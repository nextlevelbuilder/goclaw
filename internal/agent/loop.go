package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/safego"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// indexedResult holds the output of a single parallel tool execution, preserving
// the original call index so results can be sorted back into deterministic order.
type indexedResult struct {
	idx          int
	tc           providers.ToolCall
	registryName string
	result       *tools.Result
	argsJSON     string
	spanStart    time.Time
}

func (l *Loop) runLoop(ctx context.Context, req RunRequest) (result *RunResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			buf := make([]byte, 8192)
			n := runtime.Stack(buf, false)
			slog.Error("agent loop panicked", "agent", l.id, "session", req.SessionKey,
				"panic", fmt.Sprint(r), "stack", string(buf[:n]))
			result = nil
			err = fmt.Errorf("agent loop panic: %v", r)
		}
	}()

	// Per-run emit wrapper: enriches every AgentEvent with delegation + routing context.
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

	// Inject context: agent/tenant/user/workspace scoping, input guard, message truncation.
	ctxSetup, err := l.injectContext(ctx, &req)
	if err != nil {
		return nil, err
	}
	ctx = ctxSetup.ctx
	resolvedTeamSettings := ctxSetup.resolvedTeamSettings

	// 0. Cache agent's context window on the session (first run only).
	// Enables scheduler's adaptive throttle to use the real value instead of hardcoded 200K.
	if l.sessions.GetContextWindow(ctx, req.SessionKey) <= 0 {
		l.sessions.SetContextWindow(ctx, req.SessionKey, l.contextWindow)
	}

	// 0b. Load adaptive tool timing from session metadata.
	toolTiming := ParseToolTiming(l.sessions.GetSessionMetadata(ctx, req.SessionKey))

	// Resolve slow_tool notification config from already-loaded team settings (no extra DB query).
	slowToolEnabled := tools.ParseTeamNotifyConfig(resolvedTeamSettings).SlowTool

	// 1. Build messages from session history
	history := l.sessions.GetHistory(ctx, req.SessionKey)
	summary := l.sessions.GetSummary(ctx, req.SessionKey)

	// buildMessages resolves context files once and also detects BOOTSTRAP.md presence
	// (hadBootstrap) — no extra DB roundtrip needed for bootstrap detection.
	messages, hadBootstrap := l.buildMessages(ctx, history, summary, req.Message, req.ExtraSystemPrompt, req.SessionKey, req.Channel, req.ChannelType, req.ChatTitle, req.PeerKind, req.UserID, req.HistoryLimit, req.SkillFilter, req.LightContext)

	// 1b–2f. Persist and enrich all incoming media (images, docs, audio, video).
	ctx, messages, mediaRefs := l.enrichInputMedia(ctx, &req, messages)

	// 2g–2h. Inject leader pending-task reminders and member task context.
	messages, memberTask := l.injectTeamTaskReminders(ctx, &req, messages)

	// 3. Buffer new messages — write to session only AFTER the run completes.
	// This prevents concurrent runs from seeing each other's in-progress messages.
	// NOTE: pendingMsgs stores text + lightweight MediaRefs (not base64 images).
	// Use enriched content (with media IDs and paths) from the messages array
	// instead of raw req.Message, so historical messages retain full refs (RC-1 fix).
	var initPendingMsgs []providers.Message
	if !req.HideInput {
		enrichedContent := req.Message
		if len(messages) > 0 {
			for i := len(messages) - 1; i >= 0; i-- {
				if messages[i].Role == "user" {
					enrichedContent = messages[i].Content
					break
				}
			}
		}
		initPendingMsgs = append(initPendingMsgs, providers.Message{
			Role:      "user",
			Content:   enrichedContent,
			MediaRefs: mediaRefs,
		})
	}

	// 4. Run LLM iteration loop — all mutable state encapsulated in runState.
	rs := &runState{
		pendingMsgs: initPendingMsgs,
	}

	// Inject retry hook so channels can update placeholder on LLM retries.
	ctx = providers.WithRetryHook(ctx, func(attempt, maxAttempts int, err error) {
		emitRun(AgentEvent{
			Type:    protocol.AgentEventRunRetrying,
			AgentID: l.id,
			RunID:   req.RunID,
			Payload: map[string]string{
				"attempt":     fmt.Sprintf("%d", attempt),
				"maxAttempts": fmt.Sprintf("%d", maxAttempts),
				"error":       err.Error(),
			},
		})
	})

	maxIter := l.maxIterations
	if req.MaxIterations > 0 && req.MaxIterations < maxIter {
		maxIter = req.MaxIterations
	}

	// Budget check: query monthly spent once before starting iterations.
	if l.budgetMonthlyCents > 0 && l.tracingStore != nil && l.agentUUID != uuid.Nil {
		now := time.Now().UTC()
		spent, err := l.tracingStore.GetMonthlyAgentCost(ctx, l.agentUUID, now.Year(), now.Month())
		if err == nil {
			spentCents := int(spent * 100)
			if spentCents >= l.budgetMonthlyCents {
				slog.Warn("agent budget exceeded", "agent", l.id, "spent_cents", spentCents, "budget_cents", l.budgetMonthlyCents)
				return nil, fmt.Errorf("monthly budget exceeded ($%.2f / $%.2f)", spent, float64(l.budgetMonthlyCents)/100)
			}
		}
	}

	for rs.iteration < maxIter {
		rs.iteration++

		slog.Debug("agent iteration", "agent", l.id, "iteration", rs.iteration, "messages", len(messages))

		// Skill evolution: budget pressure nudges at 70% and 90% of iteration budget.
		// Ephemeral (in-memory only, not persisted to session) — LLM sees them during this run only.
		if l.skillEvolve && maxIter > 0 {
			locale := store.LocaleFromContext(ctx)
			iterPct := float64(rs.iteration) / float64(maxIter)
			if iterPct >= 0.90 && !rs.skillNudge90Sent {
				rs.skillNudge90Sent = true
				messages = append(messages, providers.Message{
					Role:    "user",
					Content: i18n.T(locale, i18n.MsgSkillNudge90Pct),
				})
			} else if iterPct >= 0.70 && !rs.skillNudge70Sent {
				rs.skillNudge70Sent = true
				messages = append(messages, providers.Message{
					Role:    "user",
					Content: i18n.T(locale, i18n.MsgSkillNudge70Pct),
				})
			}
		}

		// Member progress nudge: remind to report progress every 6 iterations.
		// Suggests percent based on iteration ratio — model can adjust but has a baseline.
		if req.TeamTaskID != "" && memberTask.Subject != "" && rs.iteration > 0 && rs.iteration%6 == 0 {
			var nudge string
			if maxIter > 0 {
				suggestedPct := rs.iteration * 100 / maxIter
				nudge = fmt.Sprintf(
					"[System] You are at iteration %d/%d (~%d%% of budget) working on task #%d: %q. "+
						"Report your progress now: team_tasks(action=\"progress\", percent=%d, text=\"what you've accomplished so far\"). "+
						"Adjust percent based on actual work completed.",
					rs.iteration, maxIter, suggestedPct, memberTask.TaskNumber, memberTask.Subject, suggestedPct)
			} else {
				nudge = fmt.Sprintf(
					"[System] You are at iteration %d working on task #%d: %q. "+
						"Report your progress now: team_tasks(action=\"progress\", percent=50, text=\"what you've accomplished so far\"). "+
						"Adjust percent based on actual work completed.",
					rs.iteration, memberTask.TaskNumber, memberTask.Subject)
			}
			messages = append(messages, providers.Message{Role: "user", Content: nudge})
		}

		// Iteration budget nudge: when model has used 75% of iterations without
		// producing any text response, warn it to start summarizing.
		if maxIter > 0 && rs.iteration > 1 && rs.iteration == maxIter*3/4 && rs.finalContent == "" {
			messages = append(messages, providers.Message{
				Role:    "user",
				Content: "[System] You have used 75% of your iteration budget without providing a text response. Start summarizing your findings and respond to the user within the next few iterations.",
			})
		}

		// Inject iteration progress into context so tools can adapt (e.g. web_fetch reduces maxChars).
		iterCtx := tools.WithIterationProgress(ctx, tools.IterationProgress{
			Current: rs.iteration,
			Max:     maxIter,
		})

		// Emit activity event: thinking phase
		emitRun(AgentEvent{
			Type:    protocol.AgentEventActivity,
			AgentID: l.id,
			RunID:   req.RunID,
			Payload: map[string]any{"phase": "thinking", "iteration": rs.iteration},
		})

		// Build per-iteration tool list: policy, tenant exclusions, bootstrap, skill visibility,
		// channel type, and final-iteration stripping.
		var toolDefs []providers.ToolDefinition
		var allowedTools map[string]bool
		// Resolve per-user MCP tools (servers requiring user credentials).
		// Must run before buildFilteredTools so tools are in the Registry for policy filtering.
		// Uses resolved credential user ID so merged tenant users get correct MCP creds.
		if mcpUserID := store.CredentialUserIDFromContext(iterCtx); mcpUserID != "" {
			l.getUserMCPTools(iterCtx, mcpUserID)
		}
		toolDefs, allowedTools, messages = l.buildFilteredTools(&req, hadBootstrap, rs.iteration, maxIter, messages)

		// Use per-request overrides if set (e.g. heartbeat uses cheaper provider/model).
		model := l.model
		provider := l.provider
		if req.ModelOverride != "" {
			model = req.ModelOverride
		}
		if req.ProviderOverride != nil {
			provider = req.ProviderOverride
		}

		chatReq := providers.ChatRequest{
			Messages: messages,
			Tools:    toolDefs,
			Model:    model,
			Options: map[string]any{
				providers.OptMaxTokens:   l.effectiveMaxTokens(),
				providers.OptTemperature: config.DefaultTemperature,
				providers.OptSessionKey:  req.SessionKey,
				providers.OptAgentID:     l.agentUUID.String(),
				providers.OptUserID:      req.UserID,
				providers.OptChannel:     req.Channel,
				providers.OptChatID:      req.ChatID,
				providers.OptPeerKind:    req.PeerKind,
				providers.OptWorkspace:   tools.ToolWorkspaceFromCtx(ctx),
			},
		}
		if tid := store.TenantIDFromContext(ctx); tid != uuid.Nil {
			chatReq.Options[providers.OptTenantID] = tid.String()
		}
		reasoningDecision := providers.ResolveReasoningDecision(
			provider,
			model,
			l.reasoningConfig.Effort,
			l.reasoningConfig.Fallback,
			l.reasoningConfig.Source,
		)
		if effort := reasoningDecision.RequestEffort(); effort != "" {
			chatReq.Options[providers.OptThinkingLevel] = effort
		}
		if reasoningDecision.Reason != "" {
			slog.Debug("reasoning normalized",
				"provider", provider.Name(),
				"model", model,
				"requested", reasoningDecision.RequestedEffort,
				"effective", reasoningDecision.EffectiveEffort,
				"reason", reasoningDecision.Reason)
		}

		// Call LLM (streaming or non-streaming)
		var resp *providers.ChatResponse
		var err error

		callCtx := providers.WithChatGPTOAuthRoutingObservation(ctx, providers.NewChatGPTOAuthRoutingObservation())
		if reasoningDecision.HasObservation() {
			callCtx = providers.WithReasoningDecision(callCtx, reasoningDecision)
		}
		llmSpanStart := time.Now().UTC()
		llmSpanID := l.emitLLMSpanStart(callCtx, llmSpanStart, rs.iteration, messages, withModel(model), withProvider(provider.Name()))

		if req.Stream {
			resp, err = provider.ChatStream(callCtx, chatReq, func(chunk providers.StreamChunk) {
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
			resp, err = provider.Chat(callCtx, chatReq)
		}

		if err != nil {
			l.emitLLMSpanEnd(callCtx, llmSpanID, llmSpanStart, nil, err, withModel(model), withProvider(provider.Name()))
			return nil, fmt.Errorf("LLM call failed (iteration %d): %w", rs.iteration, err)
		}

		l.emitLLMSpanEnd(callCtx, llmSpanID, llmSpanStart, resp, nil, withModel(model), withProvider(provider.Name()))

		// For non-streaming responses, emit thinking and content as single events
		if !req.Stream {
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

		if resp.Usage != nil {
			rs.totalUsage.PromptTokens += resp.Usage.PromptTokens
			rs.totalUsage.CompletionTokens += resp.Usage.CompletionTokens
			rs.totalUsage.TotalTokens += resp.Usage.TotalTokens
			rs.totalUsage.ThinkingTokens += resp.Usage.ThinkingTokens
		}

		// Mid-loop context management: uses history-only token count (excludes overhead
		// from system prompt, tool definitions, context files) for threshold comparison.
		// Two-phase approach: prune old tool results first, then compact only if still over budget.
		if !rs.midLoopCompacted && l.contextWindow > 0 {
			historyShare := config.DefaultHistoryShare
			if l.compactionCfg != nil && l.compactionCfg.MaxHistoryShare > 0 {
				historyShare = l.compactionCfg.MaxHistoryShare
			}
			historyBudget := int(float64(l.contextWindow) * historyShare)

			// Calibrate overhead on first LLM response with usage data.
			if !rs.overheadCalibrated && resp.Usage != nil && resp.Usage.PromptTokens > 0 {
				historyEst := EstimateHistoryTokens(messages)
				rs.overheadTokens = max(resp.Usage.PromptTokens-historyEst, 0)
				rs.overheadCalibrated = true
			}

			// Compute history-only token count (excludes system prompt/tools overhead).
			historyTokens := 0
			if resp.Usage != nil && resp.Usage.PromptTokens > 0 && rs.overheadCalibrated {
				historyTokens = resp.Usage.PromptTokens - rs.overheadTokens
			} else {
				historyTokens = EstimateHistoryTokens(messages)
			}

			// Phase 1: Prune old tool results before resorting to full compaction (at 70% of budget).
			// Re-triggers each iteration — new tool results may have grown context since last prune.
			if historyTokens >= int(float64(historyBudget)*0.7) {
				pruned := pruneContextMessages(messages, l.contextWindow, l.contextPruningCfg)
				if len(pruned) > 0 {
					messages = pruned
					historyTokens = EstimateHistoryTokens(messages)
				}
				slog.Info("mid_loop_pruning",
					"agent", l.id,
					"history_tokens", historyTokens,
					"budget", historyBudget,
					"overhead", rs.overheadTokens)
			}

			// Phase 2: Full compaction only if still over budget after pruning.
			if historyTokens >= historyBudget {
				rs.midLoopCompacted = true
				emitRun(AgentEvent{
					Type:    protocol.AgentEventActivity,
					AgentID: l.id,
					RunID:   req.RunID,
					Payload: map[string]any{"phase": "compacting", "iteration": rs.iteration},
				})
				if compacted := l.compactMessagesInPlace(ctx, messages); compacted != nil {
					messages = compacted
				}
				slog.Info("mid_loop_compaction",
					"agent", l.id,
					"history_tokens", historyTokens,
					"budget", historyBudget,
					"overhead", rs.overheadTokens,
					"context_window", l.contextWindow)
			}
		}

		// Output truncated (max_tokens hit) or tool call args malformed.
		// Inject a system hint so the model can retry with shorter output.
		// Cap consecutive truncation retries to avoid burning through all iterations.
		truncated := resp.FinishReason == "length" && len(resp.ToolCalls) > 0
		parseErr := !truncated && hasParseErrors(resp.ToolCalls)
		if truncated || parseErr {
			rs.truncationRetries++
			reason := "output truncated (max_tokens)"
			if parseErr {
				reason = "tool call arguments malformed (likely truncated)"
			}
			slog.Warn(reason, "agent", l.id, "iteration", rs.iteration,
				"truncation_retry", rs.truncationRetries, "max_tokens", l.effectiveMaxTokens())

			if rs.truncationRetries >= maxTruncationRetries {
				slog.Warn("truncation retry limit reached, giving up",
					"agent", l.id, "retries", rs.truncationRetries)
				rs.finalContent = resp.Content
				if rs.finalContent == "" {
					rs.finalContent = "[Unable to complete: output repeatedly exceeded max_tokens. Try a simpler request or increase the token limit.]"
				}
				break
			}

			hint := "[System] Your output was truncated because it exceeded max_tokens. Your tool call arguments were incomplete. Please retry with shorter content — split large writes into multiple smaller calls, or reduce the amount of text."
			if parseErr {
				hint = "[System] One or more tool call arguments were malformed (truncated JSON). This usually means your output was too long. Please retry with shorter content — split large writes into multiple smaller calls."
			}
			messages = append(messages,
				providers.Message{Role: "assistant", Content: resp.Content},
				providers.Message{Role: "user", Content: hint},
			)
			continue
		}
		// Reset truncation counter on successful tool call (model recovered).
		rs.truncationRetries = 0

		// No tool calls — exit or drain injected follow-ups.
		if len(resp.ToolCalls) == 0 {
			// Mid-run injection (Point B): drain all buffered user follow-up messages
			// before exiting. If found, save current assistant response and continue
			// the loop so the LLM can respond to the injected messages.
			if forLLM, forSession := l.drainInjectChannel(req.InjectCh, emitRun); len(forLLM) > 0 {
				messages = append(messages, providers.Message{Role: "assistant", Content: resp.Content})
				messages = append(messages, forLLM...)
				rs.pendingMsgs = append(rs.pendingMsgs, providers.Message{Role: "assistant", Content: resp.Content})
				rs.pendingMsgs = append(rs.pendingMsgs, forSession...)
				continue
			}

			rs.finalContent = resp.Content
			rs.finalThinking = resp.Thinking
			break
		}

		// Ensure globally unique tool call IDs (OpenAI-compatible APIs return 400 on duplicates).
		// Skip if raw content is present (Anthropic thinking passback) to avoid desync.
		if resp.RawAssistantContent == nil {
			resp.ToolCalls = uniquifyToolCallIDs(resp.ToolCalls, req.RunID, rs.iteration)
		}

		// Build assistant message with tool calls
		assistantMsg := providers.Message{
			Role:                "assistant",
			Content:             resp.Content,
			Thinking:            resp.Thinking, // reasoning_content passback for thinking models (Kimi, DeepSeek)
			ToolCalls:           resp.ToolCalls,
			Phase:               resp.Phase,               // preserve Codex phase metadata (gpt-5.3-codex)
			RawAssistantContent: resp.RawAssistantContent, // preserve thinking blocks for Anthropic passback
		}
		messages = append(messages, assistantMsg)
		rs.pendingMsgs = append(rs.pendingMsgs, assistantMsg)

		// Emit block.reply for intermediate assistant content during tool iterations.
		// Non-streaming channels (Zalo, Discord, WhatsApp) would otherwise lose this text.
		if resp.Content != "" {
			sanitized := SanitizeAssistantContent(resp.Content)
			if sanitized != "" && !IsSilentReply(sanitized) {
				rs.blockReplies++
				rs.lastBlockReply = sanitized
				emitRun(AgentEvent{
					Type:    protocol.AgentEventBlockReply,
					AgentID: l.id,
					RunID:   req.RunID,
					Payload: map[string]string{"content": sanitized},
				})
			}
		}

		// Track team_tasks create for orphan detection (argument-based, pre-execution).
		// Spawn counting is done post-execution so failed spawns don't get counted.
		for _, tc := range resp.ToolCalls {
			if l.resolveToolCallName(tc.Name) == "team_tasks" {
				if action, _ := tc.Arguments["action"].(string); action == "create" {
					rs.teamTaskCreates++
				}
			}
		}

		// Tool budget check: soft stop when total tool calls exceed the per-agent limit.
		// Same pattern as maxIterations — no error thrown, LLM summarizes and returns.
		rs.totalToolCalls += len(resp.ToolCalls)
		if l.maxToolCalls > 0 && rs.totalToolCalls > l.maxToolCalls {
			slog.Warn("security.tool_budget_exceeded",
				"agent", l.id, "total", rs.totalToolCalls, "limit", l.maxToolCalls)
			messages = append(messages, providers.Message{
				Role:    "user",
				Content: fmt.Sprintf("[System] Tool call budget reached (%d/%d). Do NOT call any more tools. Summarize results so far and respond to the user.", rs.totalToolCalls, l.maxToolCalls),
			})
			continue // one more LLM call for summarization, then loop exits (no tool calls)
		}

		// Emit activity event: tool execution phase
		if len(resp.ToolCalls) > 0 {
			toolNames := make([]string, len(resp.ToolCalls))
			for i, tc := range resp.ToolCalls {
				toolNames[i] = tc.Name
			}
			emitRun(AgentEvent{
				Type:    protocol.AgentEventActivity,
				AgentID: l.id,
				RunID:   req.RunID,
				Payload: map[string]any{
					"phase":     "tool_exec",
					"tool":      toolNames[0],
					"tools":     toolNames,
					"iteration": rs.iteration,
				},
			})
		}

		// Execute tool calls (parallel when multiple, sequential when single)
		if len(resp.ToolCalls) == 1 {
			// Single tool: sequential — no goroutine overhead
			tc := resp.ToolCalls[0]
			emitRun(AgentEvent{
				Type:    protocol.AgentEventToolCall,
				AgentID: l.id,
				RunID:   req.RunID,
				Payload: map[string]any{"name": tc.Name, "id": tc.ID, "arguments": truncateToolArgs(tc.Arguments, 500)},
			})

			argsJSON, _ := json.Marshal(tc.Arguments)
			slog.Info("tool call", "agent", l.id, "tool", tc.Name, "args_len", len(argsJSON))

			registryName := l.resolveToolCallName(tc.Name)

			toolSpanStart := time.Now().UTC()
			toolSpanID := l.emitToolSpanStart(ctx, toolSpanStart, tc.Name, tc.ID, string(argsJSON))

			stopSlowTimer := toolTiming.StartSlowTimer(tc.Name, l.id, req.RunID, slowToolEnabled, emitRun)
			var result *tools.Result
			if allowedTools != nil && !allowedTools[registryName] {
				// Attempt lazy activation: deferred MCP tools can be activated on first call
				// so the LLM can call them by name directly without mcp_tool_search.
				if l.tools.TryActivateDeferred(registryName) {
					// Verify tool isn't explicitly denied by policy before allowing.
					if l.toolPolicy != nil && l.toolPolicy.IsDenied(registryName, l.agentToolPolicy) {
						slog.Warn("security.tool_policy_denied_lazy", "agent", l.id, "tool", tc.Name, "resolved", registryName)
						result = tools.ErrorResult("tool not allowed by policy: " + tc.Name)
					} else {
						allowedTools[registryName] = true
						slog.Info("mcp.tool.lazy_activated", "agent", l.id, "tool", tc.Name, "resolved", registryName)
					}
				} else {
					slog.Warn("security.tool_policy_blocked", "agent", l.id, "tool", tc.Name, "resolved", registryName)
					result = tools.ErrorResult("tool not allowed by policy: " + tc.Name)
				}
			}
			if result == nil {
				result = l.tools.ExecuteWithContext(iterCtx, registryName, tc.Arguments, req.Channel, req.ChatID, req.PeerKind, req.SessionKey, nil)
			}
			stopSlowTimer()

			l.emitToolSpanEnd(ctx, toolSpanID, toolSpanStart, result)

			// Record tool execution time for adaptive thresholds.
			toolTiming.Record(tc.Name, time.Since(toolSpanStart).Milliseconds())

			// Process tool result: loop detection, events, media, deliverables.
			toolMsg, warningMsgs, action := l.processToolResult(ctx, rs, &req, emitRun, tc, registryName, result, hadBootstrap)
			messages = append(messages, toolMsg)
			rs.pendingMsgs = append(rs.pendingMsgs, toolMsg)
			messages = append(messages, warningMsgs...)
			if action == toolResultBreak {
				break
			}

			// Check for read-only streak (single tool path).
			if warnMsg, shouldBreak := l.checkReadOnlyStreak(rs, &req); shouldBreak {
				break
			} else if warnMsg != nil {
				messages = append(messages, *warnMsg)
			}
		} else {
			// Multiple tools: parallel execution via goroutines.
			// Each goroutine performs lazy MCP activation + policy checking independently.
			// Tool instances are immutable (context-based) so concurrent access is safe.
			// Results are collected then processed sequentially for deterministic ordering.

			// 1. Emit all tool.call events upfront (client sees all calls starting)
			for _, tc := range resp.ToolCalls {
				emitRun(AgentEvent{
					Type:    protocol.AgentEventToolCall,
					AgentID: l.id,
					RunID:   req.RunID,
					Payload: map[string]any{"name": tc.Name, "id": tc.ID, "arguments": truncateToolArgs(tc.Arguments, 500)},
				})
			}

			// 2. Execute all tools in parallel
			resultCh := make(chan indexedResult, len(resp.ToolCalls))
			var wg sync.WaitGroup

			for i, tc := range resp.ToolCalls {
				wg.Add(1)
				go func(idx int, tc providers.ToolCall) {
					defer wg.Done()
					defer safego.Recover(func(v any) {
						resultCh <- indexedResult{
							idx:          idx,
							tc:           tc,
							registryName: tc.Name,
							result:       tools.ErrorResult(fmt.Sprintf("tool %q panicked: %v", tc.Name, v)),
						}
					}, "agent", l.id, "tool", tc.Name)
					argsJSON, _ := json.Marshal(tc.Arguments)
					slog.Info("tool call", "agent", l.id, "tool", tc.Name, "args_len", len(argsJSON), "parallel", true)
					spanStart := time.Now().UTC()
					registryName := l.resolveToolCallName(tc.Name)
					// Emit running span inside goroutine — goroutine-safe (channel send only).
					// End is also emitted here to prevent orphans on ctx cancellation.
					spanID := l.emitToolSpanStart(ctx, spanStart, tc.Name, tc.ID, string(argsJSON))

					stopSlowTimer := toolTiming.StartSlowTimer(tc.Name, l.id, req.RunID, slowToolEnabled, emitRun)
					var result *tools.Result
					if allowedTools != nil && !allowedTools[registryName] {
						// Attempt lazy activation for deferred MCP tools.
						// Note: don't write back to allowedTools — concurrent goroutines share
						// the map and writes would race. TryActivateDeferred is idempotent.
						if l.tools.TryActivateDeferred(registryName) {
							// Verify tool isn't explicitly denied by policy before allowing.
							if l.toolPolicy != nil && l.toolPolicy.IsDenied(registryName, l.agentToolPolicy) {
								slog.Warn("security.tool_policy_denied_lazy", "agent", l.id, "tool", tc.Name, "resolved", registryName)
								result = tools.ErrorResult("tool not allowed by policy: " + tc.Name)
							} else {
								slog.Info("mcp.tool.lazy_activated", "agent", l.id, "tool", tc.Name, "resolved", registryName)
							}
						} else {
							slog.Warn("security.tool_policy_blocked", "agent", l.id, "tool", tc.Name, "resolved", registryName)
							result = tools.ErrorResult("tool not allowed by policy: " + tc.Name)
						}
					}
					if result == nil {
						result = l.tools.ExecuteWithContext(iterCtx, registryName, tc.Arguments, req.Channel, req.ChatID, req.PeerKind, req.SessionKey, nil)
					}
					stopSlowTimer()
					l.emitToolSpanEnd(ctx, spanID, spanStart, result)
					resultCh <- indexedResult{idx: idx, tc: tc, registryName: registryName, result: result, argsJSON: string(argsJSON), spanStart: spanStart}
				}(i, tc)
			}

			// Close channel after all goroutines complete (run in separate goroutine to avoid deadlock)
			go func() { wg.Wait(); close(resultCh) }()

			// 3. Collect results (respect context cancellation to allow /stop)
			collected := make([]indexedResult, 0, len(resp.ToolCalls))
		collectLoop:
			for range resp.ToolCalls {
				select {
				case r, ok := <-resultCh:
					if !ok {
						break collectLoop
					}
					collected = append(collected, r)
				case <-ctx.Done():
					// Trade-off: responsive /stop cancellation skips finalizeRun() cleanup
					// (bootstrap cleanup, message flush). Stuck agent is worse than lost finalization.
					return nil, ctx.Err()
				}
			}

			// 4. Sort by original index → deterministic message ordering
			sort.Slice(collected, func(i, j int) bool {
				return collected[i].idx < collected[j].idx
			})

			// 5. Process results sequentially: emit events, append messages, save to session
			// Note: tool span start/end already emitted inside goroutines above.
			// IMPORTANT: warning messages (role="user") must be deferred until AFTER all
			// tool results are appended. Inserting a user message between tool results
			// breaks the Anthropic API requirement that all tool_results for a set of
			// tool_uses must be consecutive (causes "tool_use ids without tool_result"
			// errors when routed through LiteLLM OpenAI→Anthropic conversion).
			var loopStuck bool
			var deferredWarnings []providers.Message
			for _, r := range collected {
				// Record tool execution time for adaptive thresholds.
				toolTiming.Record(r.tc.Name, time.Since(r.spanStart).Milliseconds())

				// Process tool result: loop detection, events, media, deliverables.
				toolMsg, warningMsgs, action := l.processToolResult(ctx, rs, &req, emitRun, r.tc, r.registryName, r.result, hadBootstrap)
				messages = append(messages, toolMsg)
				rs.pendingMsgs = append(rs.pendingMsgs, toolMsg)
				deferredWarnings = append(deferredWarnings, warningMsgs...)
				if action == toolResultBreak {
					loopStuck = true
					break
				}
			}
			// Append deferred warnings after all tool results to preserve consecutive grouping.
			messages = append(messages, deferredWarnings...)

			// Check read-only streak after processing all parallel results.
			if !loopStuck {
				if warnMsg, shouldBreak := l.checkReadOnlyStreak(rs, &req); shouldBreak {
					loopStuck = true
				} else if warnMsg != nil {
					messages = append(messages, *warnMsg)
				}
			}

			if loopStuck {
				break
			}
		}

		// Mid-run injection (Point A): drain any user follow-up messages
		// that arrived during tool execution. Append them after tool results
		// so the next LLM call sees: [tool results...] + [user follow-ups...].
		if forLLM, forSession := l.drainInjectChannel(req.InjectCh, emitRun); len(forLLM) > 0 {
			messages = append(messages, forLLM...)
			rs.pendingMsgs = append(rs.pendingMsgs, forSession...)
		}

		// Periodic checkpoint: flush pending messages to session every 5 iterations
		// to limit data loss on container crash (#294). Trade-off: partial visibility
		// to concurrent reads vs full data loss on crash.
		// AddMessage writes to in-memory cache; Save persists to DB. We must clear
		// rs.pendingMsgs after AddMessage to prevent double-add in the final flush.
		const checkpointInterval = 5
		if rs.iteration > 0 && rs.iteration%checkpointInterval == 0 && len(rs.pendingMsgs) > 0 {
			for _, msg := range rs.pendingMsgs {
				l.sessions.AddMessage(ctx, req.SessionKey, msg)
			}
			rs.checkpointFlushedMsgs += len(rs.pendingMsgs)
			rs.pendingMsgs = rs.pendingMsgs[:0]
			l.sessions.Save(ctx, req.SessionKey) //nolint:errcheck — best-effort persistence
		}

	}

	// 5-9. Finalize: sanitize, media dedup, session flush, bootstrap cleanup, build result.
	return l.finalizeRun(ctx, rs, &req, history, hadBootstrap, toolTiming), nil
}

// resolveToolCallName strips the configured tool call prefix from a name
// returned by the model, returning the original registry name.
// Example: prefix "proxy_" + model calls "proxy_exec" → returns "exec".
func (l *Loop) resolveToolCallName(name string) string {
	if l.agentToolPolicy != nil && l.agentToolPolicy.ToolCallPrefix != "" {
		return tools.StripToolPrefix(l.agentToolPolicy.ToolCallPrefix, name)
	}
	return name
}

// maxTruncationRetries caps consecutive truncation/parse-error retries.
// After this many retries the loop gives up rather than burning all iterations.
const maxTruncationRetries = 3

// hasParseErrors returns true if any tool call has a non-empty ParseError,
// indicating the arguments JSON was malformed or truncated by the provider.
func hasParseErrors(calls []providers.ToolCall) bool {
	for _, tc := range calls {
		if tc.ParseError != "" {
			return true
		}
	}
	return false
}

func truncateToolArgs(args map[string]any, maxLen int) map[string]any {
	out := make(map[string]any, len(args))
	for k, v := range args {
		if s, ok := v.(string); ok && len(s) > maxLen {
			out[k] = truncateStr(s, maxLen)
		} else {
			out[k] = v
		}
	}
	return out
}
