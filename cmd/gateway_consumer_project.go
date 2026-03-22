package cmd

import (
	"context"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// resolveProjectOverrides looks up the project for a chat and returns its ID + MCP env overrides.
// Returns empty values if no project is configured (backward compatible).
func resolveProjectOverrides(ctx context.Context, projectStore store.ProjectStore, channelType, chatID string) (string, map[string]map[string]string) {
	if projectStore == nil || channelType == "" || chatID == "" {
		return "", nil
	}
	project, err := projectStore.GetProjectByChatID(ctx, channelType, chatID)
	if err != nil {
		slog.Warn("project.resolve_failed", "channelType", channelType, "chatID", chatID, "error", err)
		return "", nil
	}
	if project == nil {
		return "", nil
	}
	overrides, err := projectStore.GetMCPOverridesMap(ctx, project.ID)
	if err != nil {
		slog.Warn("project.overrides_failed", "project", project.Slug, "error", err)
		return project.ID.String(), nil
	}
	return project.ID.String(), overrides
}
