package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"path"
	"regexp"
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

	repo, repoOK := inferWorkIntakeRepo(route.Repos, msg.Content)
	if !repoOK {
		publishWorkIntakeError(deps, msg, "Which repo should I plan against? Mention one of: "+strings.Join(route.Repos, ", "))
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

	threadName := buildWorkIntakeThreadName(repo, msg.Content)
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

	worktree := buildWorkIntakeWorktreePath(route, repo, msg.Content)
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

	args := map[string]any{
		"kind":           "autoplan",
		"command":        command,
		"args":           []any{"--ask", msg.Content, "--repo", repo, "--base-ref", baseRef, "--worktree", worktree, "--channel", msg.Channel, "--thread-id", thread.ThreadID},
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
		"repo", repo,
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

func inferWorkIntakeRepo(repos []string, content string) (string, bool) {
	if len(repos) == 0 {
		return "", false
	}
	if len(repos) == 1 {
		return repos[0], true
	}
	text := strings.ToLower(content)
	var matches []string
	for _, repo := range repos {
		name := strings.ToLower(path.Base(repo))
		full := strings.ToLower(repo)
		if containsTokenish(text, name) || strings.Contains(text, full) {
			matches = append(matches, repo)
		}
	}
	if len(matches) == 1 {
		return matches[0], true
	}
	return "", false
}

func buildWorkIntakeThreadName(repo, content string) string {
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
	name := path.Base(repo) + " / " + strings.Join(words, " ")
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

func buildWorkIntakeWorktreePath(route config.WorkIntakeRoute, repo, content string) string {
	root := firstNonEmpty(route.WorkspaceRoot, defaultWorkIntakeRoot)
	slug := slugify(path.Base(repo) + "-" + stripWorkIntakeScaffolding(content))
	if len(slug) > 48 {
		slug = slug[:48]
		slug = strings.Trim(slug, "-")
	}
	if slug == "" {
		slug = "discord-plan"
	}
	return root + "/worktrees/" + slug + "-" + uuid.NewString()[:8]
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

func containsTokenish(haystack, needle string) bool {
	if needle == "" {
		return false
	}
	re := regexp.MustCompile(`(^|[^a-z0-9_-])` + regexp.QuoteMeta(needle) + `([^a-z0-9_-]|$)`)
	return re.MatchString(haystack)
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
