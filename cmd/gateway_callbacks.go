package cmd

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// buildEnsureUserFiles creates the per-user file seeding callback.
// Seeds per-user context files on first chat (new user profile).
func buildEnsureUserFiles(as store.AgentStore, configPermStore store.ConfigPermissionStore) agent.EnsureUserFilesFunc {
	return func(ctx context.Context, agentID uuid.UUID, userID, agentType, workspace, channel string) (string, error) {
		isNew, effectiveWs, err := as.GetOrCreateUserProfile(ctx, agentID, userID, workspace, channel)
		if err != nil {
			return effectiveWs, err
		}
		if !isNew {
			return effectiveWs, nil // already profiled = already seeded
		}

		// Auto-add first group member as a file writer (bootstrap the allowlist).
		if configPermStore != nil && (strings.HasPrefix(userID, "group:") || strings.HasPrefix(userID, "guild:")) {
			senderID := store.SenderIDFromContext(ctx)
			if senderID != "" {
				parts := strings.SplitN(senderID, "|", 2)
				numericID := parts[0]
				senderUsername := ""
				if len(parts) > 1 {
					senderUsername = parts[1]
				}
				meta, _ := json.Marshal(map[string]string{"displayName": "", "username": senderUsername})
				if addErr := configPermStore.Grant(ctx, &store.ConfigPermission{
					AgentID:    agentID,
					Scope:      userID,
					ConfigType: "file_writer",
					UserID:     numericID,
					Permission: "allow",
					Metadata:   meta,
				}); addErr != nil {
					slog.Warn("failed to auto-add group file writer", "error", addErr, "sender", numericID, "group", userID)
				}
				// No bus broadcast needed — Grant already invalidates cache
			}
		}

		_, err = bootstrap.SeedUserFiles(ctx, as, agentID, userID, agentType)
		return effectiveWs, err
	}
}

// buildBootstrapCleanup creates a callback that removes BOOTSTRAP.md for a user.
// Used as a safety net after enough conversation turns, in case the LLM
// didn't clear BOOTSTRAP.md itself. Idempotent — no-op if already cleared.
func buildBootstrapCleanup(as store.AgentStore) agent.BootstrapCleanupFunc {
	return func(ctx context.Context, agentID uuid.UUID, userID string) error {
		return as.DeleteUserContextFile(ctx, agentID, userID, bootstrap.BootstrapFile)
	}
}

// buildContextFileLoader creates the per-request context file loader callback.
// Delegates to the ContextFileInterceptor for type-aware routing.
func buildContextFileLoader(intc *tools.ContextFileInterceptor) agent.ContextFileLoaderFunc {
	return func(ctx context.Context, agentID uuid.UUID, userID, agentType string) []bootstrap.ContextFile {
		return intc.LoadContextFiles(ctx, agentID, userID, agentType)
	}
}
