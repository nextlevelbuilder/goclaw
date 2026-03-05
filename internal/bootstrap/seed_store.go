package bootstrap

import (
	"context"
	"log/slog"
	"path/filepath"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

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
		// TOOLS.md and HEARTBEAT.md are only useful in standalone mode.
		// TOOLS.md: local tool notes (camera, SSH, device names).
		// HEARTBEAT.md: replaced by cron jobs in managed mode.
		if name == ToolsFile || name == HeartbeatFile {
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

		if err := agentStore.SetAgentContextFile(ctx, agentID, name, string(content)); err != nil {
			return seeded, err
		}
		seeded = append(seeded, name)
	}

	if len(seeded) > 0 {
		slog.Info("seeded agent context files to store", "agent", agentID, "files", seeded)
	}

	return seeded, nil
}

// userSeedFilesOpen is the full set of files seeded per-user for open agents.
// TOOLS.md and HEARTBEAT.md excluded — only useful in standalone mode.
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
}

// SeedUserFiles seeds embedded templates into user_context_files for a new user.
// For "open" agents: all 7 files (including BOOTSTRAP.md).
// For "predefined" agents: USER.md + BOOTSTRAP.md (user-focused onboarding template).
// Only writes files that don't already exist — safe to call multiple times.
//
// For predefined agents seeding USER.md: if the agent already has a populated
// USER.md in agent_context_files (e.g. written by the wizard or management dashboard),
// that content is used as the per-user seed instead of the blank embedded template.
// This ensures wizard-configured owner profiles are preserved on first chat.
//
// Returns the list of file names that were seeded.
func SeedUserFiles(ctx context.Context, agentStore store.AgentStore, agentID uuid.UUID, userID, agentType string) ([]string, error) {
	files := userSeedFilesOpen
	if agentType == store.AgentTypePredefined {
		files = userSeedFilesPredefined
	}

	// Check existing per-user files to avoid overwriting personalized content
	existing, err := agentStore.GetUserContextFiles(ctx, agentID, userID)
	if err != nil {
		return nil, err
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
				if err := agentStore.SetUserContextFile(ctx, agentID, userID, name, agentContent); err != nil {
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

		if err := agentStore.SetUserContextFile(ctx, agentID, userID, name, string(content)); err != nil {
			return seeded, err
		}
		seeded = append(seeded, name)
	}

	if len(seeded) > 0 {
		slog.Info("seeded user context files", "agent", agentID, "user", userID, "type", agentType, "files", seeded)
	}

	return seeded, nil
}
