package bootstrap

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// retryOnBusy retries fn up to 3 times on SQLITE_BUSY errors with 500ms delay.
func retryOnBusy(fn func() error) error {
	var lastErr error
	for attempt := range 3 {
		lastErr = fn()
		if lastErr == nil {
			return nil
		}
		if !strings.Contains(lastErr.Error(), "SQLITE_BUSY") && !strings.Contains(lastErr.Error(), "database is locked") {
			return lastErr
		}
		if attempt < 2 {
			slog.Warn("bootstrap: retrying after SQLITE_BUSY", "attempt", attempt+1)
			time.Sleep(500 * time.Millisecond)
		}
	}
	return lastErr
}

// SeedToStore seeds embedded templates into agent_context_files (agent-level).
// Used for predefined agents only — open agents get per-user files via SeedUserFiles.
// Only writes files that don't already have content.
// Returns the list of file names that were seeded.
func SeedToStore(ctx context.Context, agentStore store.AgentStore, agentID uuid.UUID, agentType string) ([]string, error) {
	// Open agents don't need agent-level context files —
	// all files are seeded per-user from embedded templates on first chat.
	if agentType == store.AgentTypeOpen {
		return nil, nil
	}

	existing, err := agentStore.GetAgentContextFiles(ctx, agentID)
	if err != nil {
		slog.Warn("bootstrap: failed to check existing agent files", "agent", agentID, "error", err)
		return nil, err
	}

	// Build set of files that already have content
	hasContent := make(map[string]bool)
	for _, f := range existing {
		if f.Content != "" {
			hasContent[f.FileName] = true
		}
	}

	var seeded []string
	for _, name := range templateFiles {
		// USER.md is always per-user for predefined agents — don't seed at agent level.
		// Having it at agent level alongside the user copy can confuse the model.
		if name == UserFile {
			continue
		}
		// TOOLS.md: local tool notes (camera, SSH, device names) — not applicable.
		if name == ToolsFile {
			continue
		}
		if hasContent[name] {
			continue
		}

		content, err := templateFS.ReadFile(filepath.Join("templates", name))
		if err != nil {
			slog.Warn("bootstrap: failed to read embedded template", "file", name, "error", err)
			continue
		}

		if err := retryOnBusy(func() error { return agentStore.SetAgentContextFile(ctx, agentID, name, string(content)) }); err != nil {
			return seeded, err
		}
		seeded = append(seeded, name)
	}

	// Seed USER_PREDEFINED.md for predefined agents (agent-level, not in templateFiles).
	// Provides baseline user-handling rules shared across all users.
	if !hasContent[UserPredefinedFile] {
		content, err := templateFS.ReadFile(filepath.Join("templates", UserPredefinedFile))
		if err == nil {
			if err := retryOnBusy(func() error { return agentStore.SetAgentContextFile(ctx, agentID, UserPredefinedFile, string(content)) }); err != nil {
				return seeded, err
			}
			seeded = append(seeded, UserPredefinedFile)
		}
	}

	if len(seeded) > 0 {
		slog.Info("seeded agent context files to store", "agent", agentID, "files", seeded)
	}

	return seeded, nil
}

// userSeedFilesOpen is the full set of files seeded per-user for open agents.
// TOOLS.md excluded — not applicable.
var userSeedFilesOpen = []string{
	AgentsFile,
	SoulFile,
	IdentityFile,
	UserFile,
	BootstrapFile,
}

// userSeedFilesPredefined is the set of files seeded per-user for predefined agents.
// Only USER.md — predefined agents already have full context (SOUL.md, IDENTITY.md, AGENTS.md)
// and don't need a bootstrap onboarding ritual. They just need to learn the user's profile.
var userSeedFilesPredefined = []string{
	UserFile,
	BootstrapFile,
}

// SeedUserFiles seeds embedded templates into user_context_files for a new user.
// For "open" agents: all 5 files (including BOOTSTRAP.md).
// For "predefined" agents: USER.md + BOOTSTRAP.md (user-focused onboarding template).
// Only writes files that don't already exist — safe to call multiple times.
//
// When skipIfAnyExist is true, returns immediately if the user already has ANY context
// files (even with empty content slots). This prevents re-seeding BOOTSTRAP.md after
// auto-cleanup (which DELETEs the row) on server restart — existing files like USER.md
// indicate the user is not brand-new, so BOOTSTRAP.md should stay gone.
// Use skipIfAnyExist=true for existing profiles, false for newly created profiles.
//
// For predefined agents seeding USER.md: if the agent already has a populated
// USER.md in agent_context_files (e.g. written by the wizard or management dashboard),
// that content is used as the per-user seed instead of the blank embedded template.
// This ensures wizard-configured owner profiles are preserved on first chat.
//
// Returns the list of file names that were seeded.
func SeedUserFiles(ctx context.Context, agentStore store.AgentStore, agentID uuid.UUID, userID, agentType string, skipIfAnyExist bool) ([]string, error) {
	files := userSeedFilesOpen
	if agentType == store.AgentTypePredefined {
		files = userSeedFilesPredefined
	}

	// Check existing per-user files to avoid overwriting personalized content
	existing, err := agentStore.GetUserContextFiles(ctx, agentID, userID)
	if err != nil {
		slog.Warn("bootstrap: failed to check existing user files", "agent", agentID, "user", userID, "error", err)
		return nil, err
	}

	// Early exit: user already has files → not a brand-new user.
	// Avoids re-seeding BOOTSTRAP.md after auto-cleanup on server restart.
	if skipIfAnyExist && len(existing) > 0 {
		slog.Debug("bootstrap: skip user seed (existing files)", "agent", agentID, "user", userID, "existing", len(existing))
		return nil, nil
	}

	hasFile := make(map[string]bool, len(existing))
	for _, f := range existing {
		if f.Content != "" {
			hasFile[f.FileName] = true
		}
	}

	// For predefined agents: load agent-level files once to use as seed fallback.
	// USER.md at agent-level may contain a pre-configured owner profile (e.g. set by
	// the wizard or management dashboard). Use it as the per-user seed instead of the
	// blank embedded template so the agent starts with the correct owner context.
	var agentLevelFiles map[string]string
	if agentType == store.AgentTypePredefined {
		agentFiles, err := agentStore.GetAgentContextFiles(ctx, agentID)
		if err == nil && len(agentFiles) > 0 {
			agentLevelFiles = make(map[string]string, len(agentFiles))
			for _, f := range agentFiles {
				if f.Content != "" {
					agentLevelFiles[f.FileName] = f.Content
				}
			}
		}
	}

	var seeded []string
	for _, name := range files {
		if hasFile[name] {
			continue // already has personalized content, don't overwrite
		}

		// For predefined agents seeding USER.md: prefer agent-level content as seed.
		// This propagates wizard/dashboard-configured owner profile to the first user.
		if agentType == store.AgentTypePredefined && name == UserFile {
			if agentContent, ok := agentLevelFiles[name]; ok {
				if err := retryOnBusy(func() error { return agentStore.SetUserContextFile(ctx, agentID, userID, name, agentContent) }); err != nil {
					return seeded, err
				}
				seeded = append(seeded, name)
				continue
			}
			// No agent-level USER.md → fall through to blank embedded template
		}

		// Predefined agents use a user-focused bootstrap template
		templateName := name
		if agentType == store.AgentTypePredefined && name == BootstrapFile {
			templateName = "BOOTSTRAP_PREDEFINED.md"
		}

		content, err := templateFS.ReadFile(filepath.Join("templates", templateName))
		if err != nil {
			slog.Warn("bootstrap: failed to read embedded template for user seed", "file", name, "error", err)
			continue
		}

		if err := retryOnBusy(func() error { return agentStore.SetUserContextFile(ctx, agentID, userID, name, string(content)) }); err != nil {
			return seeded, err
		}
		seeded = append(seeded, name)
	}

	if len(seeded) > 0 {
		slog.Info("seeded user context files", "agent", agentID, "user", userID, "type", agentType, "files", seeded)
	}

	return seeded, nil
}

// EmbeddedUserFiles returns in-memory context files from embedded templates.
// Used as a fallback when DB seeding fails (e.g. SQLITE_BUSY) so the first
// turn still gets bootstrap onboarding without waiting for DB recovery.
func EmbeddedUserFiles(agentType string) []ContextFile {
	files := userSeedFilesOpen
	if agentType == store.AgentTypePredefined {
		files = userSeedFilesPredefined
	}
	var result []ContextFile
	for _, name := range files {
		templateName := name
		if agentType == store.AgentTypePredefined && name == BootstrapFile {
			templateName = "BOOTSTRAP_PREDEFINED.md"
		}
		content, err := templateFS.ReadFile(filepath.Join("templates", templateName))
		if err != nil {
			continue
		}
		result = append(result, ContextFile{Path: name, Content: string(content)})
	}
	return result
}
