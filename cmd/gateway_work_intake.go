package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"path"
	"slices"
	"strings"
	"unicode"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/sessions"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

const (
	defaultWorkIntakeCommand = "/app/agent/bin/run-discord-plan"
	defaultWorkIntakeRoot    = "/data/workspace-eng"
	defaultWorkIntakeTimeout = "30m"
)

func maybeHandleWorkIntake(ctx context.Context, msg bus.InboundMessage, deps *ConsumerDeps, agentID, peerKind, sessionKey string) bool {
	route, ok := matchWorkIntakeRoute(deps.Cfg.Gateway.WorkIntake, msg, agentID, peerKind)
	if !ok {
		return false
	}
	if !looksLikeWorkIntake(msg.Content) {
		return false
	}
	ask := stripWorkIntakeScaffolding(msg.Content)

	repos, repoOK := selectWorkIntakeRepos(route.Repos)
	if !repoOK {
		publishWorkIntakeError(deps, msg, "I can't start planning because this channel has no configured repos.")
		return true
	}

	if deps.ChannelMgr == nil {
		publishWorkIntakeError(deps, msg, "I can't start planning because the Discord channel manager is not available.")
		return true
	}
	if deps.SubagentTasks == nil {
		publishWorkIntakeError(deps, msg, "I can't start planning because job task persistence is not available.")
		return true
	}
	if deps.Cfg.Gateway.JobsCallbackSecret == "" {
		publishWorkIntakeError(deps, msg, "I can't start planning because the job callback secret is not configured.")
		return true
	}

	threadName := buildWorkIntakeThreadName(repos, ask)
	thread, err := deps.ChannelMgr.CreateDiscordThread(ctx, msg.Channel, channels.DiscordThreadParams{
		ChannelID:          firstNonEmpty(msg.Metadata["channel_id"], msg.ChatID),
		MessageID:          msg.Metadata["message_id"],
		Name:               threadName,
		AutoArchiveMinutes: 1440,
	})
	if err != nil {
		slog.Warn("work_intake: create discord thread failed", "err", err, "channel", msg.Channel, "chat_id", msg.ChatID)
		publishWorkIntakeError(deps, msg, fmt.Sprintf("I couldn't create the planning thread: %v", err))
		return true
	}

	worktree := buildWorkIntakeWorktreePath(route, repos, ask)
	workspaceRoot := firstNonEmpty(route.WorkspaceRoot, defaultWorkIntakeRoot)
	command := firstNonEmpty(route.Command, defaultWorkIntakeCommand)
	timeout := firstNonEmpty(route.Timeout, defaultWorkIntakeTimeout)
	baseRef := firstNonEmpty(route.BaseRef, "main")

	tool := tools.NewSpawnJobTool(deps.SubagentTasks, deps.Cfg.Gateway.AgentServiceURL, []byte(deps.Cfg.Gateway.JobsCallbackSecret))
	toolCtx := tools.WithToolChannel(ctx, msg.Channel)
	toolCtx = tools.WithToolChatID(toolCtx, thread.ThreadID)
	toolCtx = tools.WithToolPeerKind(toolCtx, string(sessions.PeerGroup))
	toolCtx = tools.WithToolAgentKey(toolCtx, agentID)
	toolCtx = tools.WithToolSessionKey(toolCtx, sessions.BuildScopedSessionKey(agentID, msg.Channel, sessions.PeerGroup, thread.ThreadID))

	jobArgs := []any{"--ask", ask}
	for _, repo := range repos {
		jobArgs = append(jobArgs, "--candidate-repo", repo)
	}
	jobArgs = append(jobArgs, "--base-ref", baseRef, "--worktree", worktree, "--channel", msg.Channel, "--thread-id", thread.ThreadID)

	args := map[string]any{
		"kind":           "autoplan",
		"command":        command,
		"args":           jobArgs,
		"cwd":            workspaceRoot,
		"workspace_root": workspaceRoot,
		"worktree_path":  worktree,
		"timeout":        timeout,
		"sinks":          []any{map[string]any{"type": "discord", "channel": msg.Channel, "thread_id": thread.ThreadID}},
	}
	result := tool.Execute(toolCtx, args)
	if result.IsError {
		slog.Warn("work_intake: spawn_job failed", "result", result.ForLLM, "channel", msg.Channel, "thread_id", thread.ThreadID)
		publishWorkIntakeThreadMessage(deps, msg, thread.ThreadID, "I created this planning thread, but could not start the Kubernetes planning Job:\n"+result.ForLLM)
		return true
	}

	slog.Info("work_intake: spawned planning job",
		"channel", msg.Channel,
		"parent_chat_id", msg.ChatID,
		"thread_id", thread.ThreadID,
		"repos", strings.Join(repos, ","),
		"worktree", worktree,
	)
	publishWorkIntakeThreadMessage(deps, msg, thread.ThreadID, "Started the planning Job. Progress and any planning questions will appear here.")
	return true
}

func matchWorkIntakeRoute(cfg config.WorkIntakeConfig, msg bus.InboundMessage, agentID, peerKind string) (config.WorkIntakeRoute, bool) {
	if !cfg.Enabled || peerKind != string(sessions.PeerGroup) || msg.Metadata["is_thread"] == "true" {
		return config.WorkIntakeRoute{}, false
	}
	for _, route := range cfg.Routes {
		if route.AgentID != "" && route.AgentID != agentID {
			continue
		}
		if route.Channel != "" && route.Channel != msg.Channel {
			continue
		}
		if len(route.ChatIDs) > 0 && !slices.Contains(route.ChatIDs, firstNonEmpty(msg.Metadata["channel_id"], msg.ChatID)) {
			continue
		}
		if len(route.Repos) == 0 {
			continue
		}
		return route, true
	}
	return config.WorkIntakeRoute{}, false
}

func looksLikeWorkIntake(content string) bool {
	text := strings.ToLower(stripWorkIntakeScaffolding(content))
	if text == "" {
		return false
	}
	readOnlyPrefixes := []string{
		"what is ", "what are ", "why ", "how does ", "how do ", "where ", "when ",
		"is there ", "are there ", "do we ", "does ", "can you explain", "tell me about",
	}
	for _, prefix := range readOnlyPrefixes {
		if strings.HasPrefix(text, prefix) && !strings.Contains(text, "create a plan") && !strings.Contains(text, "implement") {
			return false
		}
	}
	actionPhrases := []string{
		"create a plan", "draft a plan", "write a plan", "plan to", "plan the", "autoplan",
		"implement", "fix", "bug", "feature", "upgrade", "support", "add ", "build ",
		"update ", "migrate", "refactor", "wire ", "ship ",
	}
	return slices.ContainsFunc(actionPhrases, func(phrase string) bool {
		return strings.Contains(text, phrase)
	})
}

func selectWorkIntakeRepos(repos []string) ([]string, bool) {
	if len(repos) == 0 {
		return nil, false
	}
	out := make([]string, 0, len(repos))
	seen := make(map[string]struct{}, len(repos))
	for _, repo := range repos {
		repo = strings.TrimSpace(repo)
		if repo == "" {
			continue
		}
		if _, ok := seen[repo]; ok {
			continue
		}
		seen[repo] = struct{}{}
		out = append(out, repo)
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

func buildWorkIntakeThreadName(repos []string, content string) string {
	summary := stripWorkIntakeScaffolding(content)
	summary = strings.TrimPrefix(summary, "@Gillen")
	summary = strings.TrimSpace(summary)
	summary = strings.Trim(summary, ".")
	if summary == "" {
		summary = "plan"
	}
	words := strings.Fields(summary)
	if len(words) > 8 {
		words = words[:8]
	}
	name := workIntakeRepoLabel(repos) + " / " + strings.Join(words, " ")
	name = sanitizeDiscordThreadName(name)
	if len([]rune(name)) > 100 {
		name = string([]rune(name)[:100])
		name = strings.TrimSpace(name)
	}
	if name == "" {
		return "planning job"
	}
	return name
}

func buildWorkIntakeWorktreePath(route config.WorkIntakeRoute, repos []string, content string) string {
	root := firstNonEmpty(route.WorkspaceRoot, defaultWorkIntakeRoot)
	slug := slugify(workIntakeRepoLabel(repos) + "-" + stripWorkIntakeScaffolding(content))
	if len(slug) > 48 {
		slug = slug[:48]
		slug = strings.Trim(slug, "-")
	}
	if slug == "" {
		slug = "discord-plan"
	}
	return root + "/worktrees/" + slug + "-" + uuid.NewString()[:8]
}

func workIntakeRepoLabel(repos []string) string {
	if len(repos) == 0 {
		return "planning"
	}
	if len(repos) == 1 {
		return path.Base(repos[0])
	}
	parts := make([]string, 0, len(repos))
	for _, repo := range repos {
		base := path.Base(repo)
		if base != "" && base != "." {
			parts = append(parts, base)
		}
	}
	if len(parts) == 0 {
		return "multi-repo"
	}
	label := strings.Join(parts, "+")
	if len([]rune(label)) > 40 {
		label = "multi-repo"
	}
	return label
}

func publishWorkIntakeError(deps *ConsumerDeps, msg bus.InboundMessage, content string) {
	if deps.MsgBus == nil {
		return
	}
	meta := channels.CopyFinalRoutingMeta(msg.Metadata)
	if msg.Metadata["message_id"] != "" {
		meta["reply_to_message_id"] = msg.Metadata["message_id"]
	}
	deps.MsgBus.PublishOutbound(bus.OutboundMessage{
		Channel:  msg.Channel,
		ChatID:   msg.ChatID,
		Content:  content,
		Metadata: meta,
		TenantID: msg.TenantID,
	})
}

func publishWorkIntakeThreadMessage(deps *ConsumerDeps, msg bus.InboundMessage, threadID, content string) {
	if deps.MsgBus == nil {
		return
	}
	deps.MsgBus.PublishOutbound(bus.OutboundMessage{
		Channel:  msg.Channel,
		ChatID:   threadID,
		Content:  content,
		Metadata: channels.CopyFinalRoutingMeta(msg.Metadata),
		TenantID: msg.TenantID,
	})
}

func stripWorkIntakeScaffolding(content string) string {
	lines := strings.Split(content, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "[From:") {
			lines = lines[i+1:]
			break
		}
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "[From:") || strings.HasPrefix(line, "[Chat messages since") {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, " ")
}

func sanitizeDiscordThreadName(s string) string {
	var b strings.Builder
	lastSpace := false
	for _, r := range s {
		if unicode.IsControl(r) {
			continue
		}
		if unicode.IsSpace(r) {
			if !lastSpace {
				b.WriteRune(' ')
				lastSpace = true
			}
			continue
		}
		lastSpace = false
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

func slugify(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteRune('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
