package agent

import (
	"context"
	"fmt"
	"strings"

	pluginhooks "github.com/nextlevelbuilder/goclaw/internal/plugins/hooks"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

func (l *Loop) preToolHookBlock(ctx context.Context, tc providers.ToolCall) *tools.Result {
	if l.hookExecutor == nil {
		return nil
	}

	results := l.hookExecutor.Fire(ctx, pluginhooks.PreToolUse, map[string]any{
		"tool_name": tc.Name,
		"tool_id":   tc.ID,
		"tool_args": tc.Arguments,
	})
	if !pluginhooks.HasPreventOrDeny(results) {
		return nil
	}

	message := strings.TrimSpace(pluginhooks.CollectMessages(results))
	if message == "" {
		message = fmt.Sprintf("tool %q blocked by plugin hook", tc.Name)
	}
	return tools.ErrorResult(message)
}

func (l *Loop) postToolHookMessages(ctx context.Context, tc providers.ToolCall, result *tools.Result) []providers.Message {
	if l.hookExecutor == nil || result == nil {
		return nil
	}

	event := pluginhooks.PostToolUse
	if result.IsError {
		event = pluginhooks.PostToolFailure
	}
	results := l.hookExecutor.Fire(ctx, event, map[string]any{
		"tool_name": tc.Name,
		"tool_id":   tc.ID,
		"tool_args": tc.Arguments,
		"result":    result.ForLLM,
		"paths":     result.TouchedPaths,
	})
	message := strings.TrimSpace(pluginhooks.CollectMessages(results))
	if message == "" {
		return nil
	}
	return []providers.Message{{
		Role:    "user",
		Content: "[Plugin hook]\n" + message,
	}}
}

func (l *Loop) autoActivateSkillMessages(ctx context.Context, rs *runState, touchedPaths []string) []providers.Message {
	if l.skillsLoader == nil || len(touchedPaths) == 0 {
		return nil
	}
	if rs.activatedSkills == nil {
		rs.activatedSkills = make(map[string]bool)
	}

	contents := l.skillsLoader.ActivatedSkillContents(ctx, touchedPaths)
	if len(contents) == 0 {
		return nil
	}

	var msgs []providers.Message
	for slug, content := range contents {
		if rs.activatedSkills[slug] || strings.TrimSpace(content) == "" {
			continue
		}
		rs.activatedSkills[slug] = true
		msgs = append(msgs, providers.Message{
			Role: "user",
			Content: fmt.Sprintf(
				"[Auto-activated skill: %s]\nRead and follow this skill immediately.\n\n%s",
				slug,
				content,
			),
		})
	}
	return msgs
}

func (l *Loop) fireSessionEndHooks(ctx context.Context, runID, sessionKey string, hadAPIError bool) {
	if l.hookExecutor == nil || hadAPIError {
		return
	}
	l.hookExecutor.Fire(ctx, pluginhooks.SessionEnd, map[string]any{
		"agent_id":    l.id,
		"run_id":      runID,
		"session_key": sessionKey,
	})
}
