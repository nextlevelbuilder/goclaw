package cmd

import (
	"context"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// resolveProjectOverrides looks up a project bound to the given chat and returns
// the project ID + per-server MCP environment overrides. Returns ("", nil) when:
//   - projectStore is nil (feature disabled — backward compatible)
//   - no project is bound to this chat
//   - any lookup error (non-blocking, logged as warning)
func resolveProjectOverrides(ctx context.Context, projectStore store.ProjectStore, channelType, chatID string) (string, map[string]map[string]string) {
	if projectStore == nil || channelType == "" || chatID == "" {
		return "", nil
	}

	project, err := projectStore.GetProjectByChatID(ctx, channelType, chatID)
	if err != nil {
		slog.Warn("project: lookup failed", "channel_type", channelType, "chat_id", chatID, "error", err)
		return "", nil
	}
	if project == nil {
		return "", nil
	}

	overrides, err := projectStore.GetMCPOverridesMap(ctx, project.ID)
	if err != nil {
		slog.Warn("project: failed to load MCP overrides", "project", project.Slug, "error", err)
		return project.ID.String(), nil
	}

	if overrides != nil {
		slog.Info("project: resolved",
			"project", project.Slug,
			"channel_type", channelType,
			"chat_id", chatID,
			"mcp_overrides", len(overrides),
		)
	}

	return project.ID.String(), overrides
}
