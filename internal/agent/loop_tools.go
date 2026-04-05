package agent

import (
	"context"
	"log/slog"
	"path/filepath"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// toolResultAction describes what the caller should do after processing a tool result.
type toolResultAction int

const (
	toolResultContinue toolResultAction = iota // proceed normally
	toolResultWarning                          // injected warning message, continue
	toolResultBreak                            // critical loop detected, break iteration
)

// processToolResult handles post-execution bookkeeping for a single tool result:
// loop detection, event emission, media collection, deliverables, and message building.
// Used by both single-tool and parallel-tool paths to eliminate duplication.
//
// Returns the tool message, an optional warning message to inject, and an action signal.
// The caller must append toolMsg and warningMsg to messages/pendingMsgs, and break if action == toolResultBreak.
func (l *Loop) processToolResult(
	ctx context.Context,
	rs *runState,
	req *RunRequest,
	emitRun func(AgentEvent),
	tc providers.ToolCall,
	registryName string,
	result *tools.Result,
	hadBootstrap bool,
) (toolMsg providers.Message, warningMsgs []providers.Message, action toolResultAction) {

	// Record for loop detection.
	argsHash := rs.loopDetector.record(registryName, tc.Arguments)
	rs.loopDetector.recordResult(argsHash, result.ForLLM)
	rs.loopDetector.recordMutation(registryName, tc.Arguments)

	if result.Async {
		rs.asyncToolCalls = append(rs.asyncToolCalls, tc.Name)
	}

	if result.IsError {
		errMsg := result.ForLLM
		if len(errMsg) > 200 {
			errMsg = errMsg[:200] + "..."
		}
		slog.Warn("tool error", "agent", l.id, "tool", tc.Name, "error", errMsg)
	}

	// Count successful spawn calls for orphan detection (post-execution).
	if registryName == "spawn" && !result.IsError {
		if tid, _ := tc.Arguments["team_task_id"].(string); tid != "" {
			rs.teamTaskSpawns++
		}
	}
	if hadBootstrap && bootstrapToolAllowlist[registryName] {
		rs.bootstrapWriteDetected = true
	}

	// Emit tool result event.
	toolResultPayload := map[string]any{
		"name":      tc.Name,
		"id":        tc.ID,
		"is_error":  result.IsError,
		"arguments": tc.Arguments,
		"result":    truncateStr(result.ForLLM, 1000),
	}
	if result.IsError && result.ForLLM != "" {
		toolResultPayload["content"] = result.ForLLM
	}
	emitRun(AgentEvent{
		Type:    protocol.AgentEventToolResult,
		AgentID: l.id,
		RunID:   req.RunID,
		Payload: toolResultPayload,
	})

	l.scanWebToolResult(tc.Name, result)

	// Collect MEDIA: paths from tool results.
	// Prefer result.Media (explicit) over ForLLM MEDIA: prefix (legacy) to avoid duplicates.
	if len(result.Media) > 0 {
		for _, mf := range result.Media {
			ct := mf.MimeType
			if ct == "" {
				ct = mimeFromExt(filepath.Ext(mf.Path))
			}
			rs.mediaResults = append(rs.mediaResults, MediaResult{Path: mf.Path, ContentType: ct})
		}
	} else if mr := parseMediaResult(result.ForLLM); mr != nil {
		rs.mediaResults = append(rs.mediaResults, *mr)
	}
	// Auto-attach workspace media to task (covers create_image/audio/video).
	if teamWs := tools.ToolTeamWorkspaceFromCtx(ctx); teamWs != "" {
		for _, mf := range result.Media {
			tools.AutoAttachWorkspaceFile(ctx, l.teamStore, teamWs, mf.Path)
		}
	}
	if result.Deliverable != "" {
		rs.deliverables = append(rs.deliverables, result.Deliverable)
	}

	toolMsg = providers.Message{
		Role:       "tool",
		Content:    result.ForLLM,
		ToolCallID: tc.ID,
		IsError:    result.IsError,
	}

	action = toolResultContinue

	// Check for tool call loop after recording result.
	if level, msg := rs.loopDetector.detect(registryName, argsHash); level != "" {
		if level == "critical" {
			slog.Warn("tool loop critical", "agent", l.id, "tool", registryName, "message", msg)
			rs.finalContent = "I was unable to complete this task — I got stuck repeatedly calling " + registryName + " without making progress. Please try rephrasing your request."
			rs.loopKilled = true
			return toolMsg, nil, toolResultBreak
		}
		slog.Warn("tool loop warning", "agent", l.id, "tool", registryName, "message", msg)
		warningMsgs = append(warningMsgs, providers.Message{Role: "user", Content: msg})
		action = toolResultWarning
	}

	// Check for same tool returning identical results with different args.
	if rh := hashResult(result.ForLLM); rh != "" {
		if level, msg := rs.loopDetector.detectSameResult(registryName, rh); level != "" {
			if level == "critical" {
				slog.Warn("tool loop critical: same result",
					"tool", registryName, "agent", l.id, "run", req.RunID)
				rs.finalContent = msg
				rs.loopKilled = true
				return toolMsg, nil, toolResultBreak
			}
			warningMsgs = append(warningMsgs, providers.Message{Role: "user", Content: msg})
			action = toolResultWarning
		}
	}

	return toolMsg, warningMsgs, action
}

// checkReadOnlyStreak detects when the agent is stuck in a read-only loop.
// Returns warning messages to inject and whether the loop should break.
func (l *Loop) checkReadOnlyStreak(rs *runState, req *RunRequest) (warningMsg *providers.Message, shouldBreak bool) {
	level, msg := rs.loopDetector.detectReadOnlyStreak()
	if level == "" {
		return nil, false
	}
	if level == "critical" {
		slog.Warn("tool loop critical: read-only streak",
			"streak", rs.loopDetector.readOnlyStreak,
			"unique", rs.loopDetector.readOnlyUnique,
			"agent", l.id, "run", req.RunID)
		rs.finalContent = msg
		rs.loopKilled = true
		return nil, true
	}
	slog.Warn("tool loop warning: read-only streak",
		"streak", rs.loopDetector.readOnlyStreak, "agent", l.id, "run", req.RunID)
	warnMsg := providers.Message{Role: "user", Content: msg}
	return &warnMsg, false
}
