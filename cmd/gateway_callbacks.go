package cmd

import (
	"context"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// buildEnsureUserProfile creates the user profile resolution callback.
// Creates/resolves user profile and returns effective workspace.
// Separated from seeding to allow independent lifecycle management.
func buildEnsureUserProfile(as store.AgentStore) agent.EnsureUserProfileFunc {
	return func(ctx context.Context, agentID uuid.UUID, userID, workspace, channel string) (string, bool, error) {
		isNew, effectiveWs, err := as.GetOrCreateUserProfile(ctx, agentID, userID, workspace, channel)
		if err != nil {
			return effectiveWs, false, err
		}

		return effectiveWs, isNew, nil
	}
}

// buildSeedUserFiles creates the context file seeding callback.
// Seeds BOOTSTRAP.md, USER.md, etc. into user_context_files.
// isNew=true seeds all files; isNew=false only seeds if user has zero files
// (avoids re-seeding BOOTSTRAP.md after auto-cleanup on server restart).
func buildSeedUserFiles(as store.AgentStore) agent.SeedUserFilesFunc {
	return func(ctx context.Context, agentID uuid.UUID, userID, agentType string, isNew bool) error {
		_, err := bootstrap.SeedUserFiles(ctx, as, agentID, userID, agentType, !isNew)
		return err
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

// buildCacheInvalidate creates a callback that invalidates the context file cache
// for a user after SeedUserFiles writes via raw agentStore. Without this,
// LoadContextFiles may return stale (empty) cached results on the first turn.
func buildCacheInvalidate(intc *tools.ContextFileInterceptor) agent.CacheInvalidateFunc {
	if intc == nil {
		return nil
	}
	return func(agentID uuid.UUID, userID string) {
		intc.InvalidateUser(agentID, userID)
	}
}

// buildContextFileLoader creates the per-request context file loader callback.
// Delegates to the ContextFileInterceptor for type-aware routing.
func buildContextFileLoader(intc *tools.ContextFileInterceptor) agent.ContextFileLoaderFunc {
	return func(ctx context.Context, agentID uuid.UUID, userID, agentType string) []bootstrap.ContextFile {
		return intc.LoadContextFiles(ctx, agentID, userID, agentType)
	}
}
