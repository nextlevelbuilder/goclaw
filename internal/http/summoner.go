package http

import (
	"context"
	"log/slog"
	"regexp"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Summoning event type constants.
const (
	SummonEventStarted       = "started"
	SummonEventFailed        = "failed"
	SummonEventCompleted     = "completed"
	SummonEventFileGenerated = "file_generated"
)

// frontmatterKey is the special key used to store frontmatter in the parsed file map.
const frontmatterKey = "__frontmatter__"

// summoningFiles is the ordered list of context files the LLM should generate.
// Only personality files — operational files (AGENTS.md, TOOLS.md)
// are kept as fixed templates from bootstrap.SeedToStore().
// USER_PREDEFINED.md is optional — generated only when description mentions user context.
var summoningFiles = []string{
	bootstrap.SoulFile,
	bootstrap.IdentityFile,
	bootstrap.UserPredefinedFile,
	bootstrap.CapabilitiesFile,
}

// fileTagRe parses <file name="SOUL.md">content</file> from LLM output.
var fileTagRe = regexp.MustCompile(`(?s)<file\s+name="([^"]+)">\s*(.*?)\s*</file>`)

// frontmatterTagRe parses <frontmatter>short expertise summary</frontmatter> from LLM output.
var frontmatterTagRe = regexp.MustCompile(`(?s)<frontmatter>\s*(.*?)\s*</frontmatter>`)

// AgentSummoner generates context files for predefined agents using an LLM.
// Runs one-shot background calls — no session data, no agent loop.
type AgentSummoner struct {
	agents      store.AgentStore
	providerReg *providers.Registry
	msgBus      *bus.MessageBus
}

// NewAgentSummoner creates a summoner backed by the given stores and provider registry.
func NewAgentSummoner(agents store.AgentStore, providerReg *providers.Registry, msgBus *bus.MessageBus) *AgentSummoner {
	return &AgentSummoner{
		agents:      agents,
		providerReg: providerReg,
		msgBus:      msgBus,
	}
}

// singleCallTimeout is the deadline for the optimistic single LLM call.
// If exceeded, we fall back to the 2-call approach with the remaining budget.
const singleCallTimeout = 300 * time.Second

// SummonAgent generates context files from a natural language description.
// Meant to be called as a goroutine: go summoner.SummonAgent(...)
// Tries a single LLM call first (all files at once). On timeout, falls back to
// 2 sequential calls (SOUL.md → IDENTITY.md + USER_PREDEFINED.md).
// On retry (resummon), skips files that were already generated (differ from template).
// On success: stores generated files and sets agent status to "active".
// On failure: keeps template files (already seeded) and sets status to store.AgentStatusSummonFailed.
func (s *AgentSummoner) SummonAgent(agentID uuid.UUID, tenantID uuid.UUID, providerName, model, description string) {
	ctx, cancel := context.WithTimeout(store.WithTenantID(context.Background(), tenantID), 600*time.Second)
	defer cancel()

	s.ensureBackfillFiles(ctx, agentID)
	s.emitEvent(agentID, tenantID, SummonEventStarted, "", "")

	// Check which files already exist (from a previous partial run)
	existingMap := s.loadExistingFiles(ctx, agentID)

	// Skip if all files already generated
	if s.isGenerated(existingMap, bootstrap.SoulFile) && s.isGenerated(existingMap, bootstrap.IdentityFile) {
		slog.Info("summoning: all files already generated, skipping", "agent", agentID)
		s.emitEvent(agentID, tenantID, SummonEventFileGenerated, bootstrap.SoulFile, "")
		s.emitEvent(agentID, tenantID, SummonEventFileGenerated, bootstrap.IdentityFile, "")
		s.finishSummon(ctx, agentID, tenantID, existingMap[bootstrap.IdentityFile], "", description)
		return
	}

	// === Optimistic single-call: generate all files at once ===
	singleCtx, singleCancel := context.WithTimeout(ctx, singleCallTimeout)
	files, err := s.generateFiles(singleCtx, providerName, model, s.buildCreatePrompt(description))
	singleCancel()

	if err == nil {
		slog.Info("summoning: single-call succeeded", "agent", agentID)
		s.storeFiles(ctx, agentID, tenantID, files)
		s.finishSummon(ctx, agentID, tenantID, files[bootstrap.IdentityFile], files[frontmatterKey], description)
		return
	}

	// Non-retryable error → fail immediately
	if !isRetryableError(err) {
		slog.Warn("summoning: single-call failed (non-retryable)", "agent", agentID, "error", err)
		s.emitEvent(agentID, tenantID, SummonEventFailed, "", err.Error())
		s.setAgentStatus(context.Background(), tenantID, agentID, store.AgentStatusSummonFailed)
		return
	}

	// === Fallback: 2-call approach ===
	slog.Info("summoning: single-call timed out, falling back to 2-call", "agent", agentID, "error", err)

	// Refresh existing files (single-call didn't store anything on error)
	existingMap = s.loadExistingFiles(ctx, agentID)

	var soulContent string
	var frontmatter string

	// Step 1: Generate SOUL.md
	if s.isGenerated(existingMap, bootstrap.SoulFile) {
		soulContent = existingMap[bootstrap.SoulFile]
		slog.Info("summoning: SOUL.md already generated, skipping", "agent", agentID)
		s.emitEvent(agentID, tenantID, SummonEventFileGenerated, bootstrap.SoulFile, "")
	} else {
		soulFiles, soulErr := s.generateFiles(ctx, providerName, model, s.buildSoulPrompt(description))
		if soulErr != nil {
			slog.Warn("summoning: SOUL.md generation failed", "agent", agentID, "error", soulErr)
			s.emitEvent(agentID, tenantID, SummonEventFailed, "", soulErr.Error())
			s.setAgentStatus(context.Background(), tenantID, agentID, store.AgentStatusSummonFailed)
			return
		}
		soulContent = soulFiles[bootstrap.SoulFile]
		frontmatter = soulFiles[frontmatterKey]
		if soulContent != "" {
			if storeErr := s.agents.SetAgentContextFile(ctx, agentID, bootstrap.SoulFile, soulContent); storeErr != nil {
				slog.Warn("summoning: failed to store SOUL.md", "agent", agentID, "error", storeErr)
			} else {
				s.emitEvent(agentID, tenantID, SummonEventFileGenerated, bootstrap.SoulFile, "")
			}
		}
		// CAPABILITIES.md is generated alongside SOUL.md in the first call
		if capContent := soulFiles[bootstrap.CapabilitiesFile]; capContent != "" {
			if storeErr := s.agents.SetAgentContextFile(ctx, agentID, bootstrap.CapabilitiesFile, capContent); storeErr != nil {
				slog.Warn("summoning: failed to store CAPABILITIES.md", "agent", agentID, "error", storeErr)
			} else {
				s.emitEvent(agentID, tenantID, SummonEventFileGenerated, bootstrap.CapabilitiesFile, "")
			}
		}
	}

	// Step 2: Generate IDENTITY.md + USER_PREDEFINED.md using SOUL.md as context
	identityNeeded := !s.isGenerated(existingMap, bootstrap.IdentityFile)
	userPredNeeded := !s.isGenerated(existingMap, bootstrap.UserPredefinedFile)

	var identityContent string
	if !identityNeeded && !userPredNeeded {
		identityContent = existingMap[bootstrap.IdentityFile]
		slog.Info("summoning: IDENTITY.md + USER_PREDEFINED.md already generated, skipping", "agent", agentID)
		s.emitEvent(agentID, tenantID, SummonEventFileGenerated, bootstrap.IdentityFile, "")
	} else {
		idFiles, idErr := s.generateFiles(ctx, providerName, model, s.buildIdentityPrompt(description, soulContent))
		if idErr != nil {
			slog.Warn("summoning: IDENTITY.md generation failed", "agent", agentID, "error", idErr)
			s.emitEvent(agentID, tenantID, SummonEventFailed, "", idErr.Error())
			s.setAgentStatus(context.Background(), tenantID, agentID, store.AgentStatusSummonFailed)
			return
		}
		identityContent = idFiles[bootstrap.IdentityFile]
		if frontmatter == "" {
			frontmatter = idFiles[frontmatterKey]
		}
		if identityContent != "" && identityNeeded {
			if storeErr := s.agents.SetAgentContextFile(ctx, agentID, bootstrap.IdentityFile, identityContent); storeErr != nil {
				slog.Warn("summoning: failed to store IDENTITY.md", "agent", agentID, "error", storeErr)
			} else {
				s.emitEvent(agentID, tenantID, SummonEventFileGenerated, bootstrap.IdentityFile, "")
			}
		}
		if upContent := idFiles[bootstrap.UserPredefinedFile]; upContent != "" && userPredNeeded {
			if storeErr := s.agents.SetAgentContextFile(ctx, agentID, bootstrap.UserPredefinedFile, upContent); storeErr != nil {
				slog.Warn("summoning: failed to store USER_PREDEFINED.md", "agent", agentID, "error", storeErr)
			} else {
				s.emitEvent(agentID, tenantID, SummonEventFileGenerated, bootstrap.UserPredefinedFile, "")
			}
		}
	}

	s.finishSummon(ctx, agentID, tenantID, identityContent, frontmatter, description)
}

// finishSummon saves agent metadata and marks the agent as active.
func (s *AgentSummoner) finishSummon(ctx context.Context, agentID, tenantID uuid.UUID, identityContent, frontmatter, description string) {
	updates := map[string]any{}
	if frontmatter == "" {
		frontmatter = truncateUTF8(description, 200)
	}
	if frontmatter != "" {
		updates["frontmatter"] = frontmatter
	}
	if name := extractIdentityName(identityContent); name != "" {
		agent, _ := s.agents.GetByID(ctx, agentID)
		if agent != nil && agent.DisplayName != "" {
			// User already set a custom name — preserve it, sync IDENTITY.md to match
			if name != agent.DisplayName {
				updated := bootstrap.UpdateIdentityField(identityContent, "Name", agent.DisplayName)
				if updated != identityContent {
					_ = s.agents.SetAgentContextFile(ctx, agentID, bootstrap.IdentityFile, updated)
				}
			}
		} else {
			// No custom name — use LLM-generated name
			updates["display_name"] = name
		}
	}
	if len(updates) > 0 {
		if err := s.agents.Update(ctx, agentID, updates); err != nil {
			slog.Warn("summoning: failed to save agent metadata", "agent", agentID, "error", err)
		}
	}
	s.setAgentStatus(ctx, tenantID, agentID, store.AgentStatusActive)
	s.emitEvent(agentID, tenantID, SummonEventCompleted, "", "")
	slog.Info("summoning: completed", "agent", agentID)
}

// loadExistingFiles reads agent context files and returns them as a map.
func (s *AgentSummoner) loadExistingFiles(ctx context.Context, agentID uuid.UUID) map[string]string {
	existing, _ := s.agents.GetAgentContextFiles(ctx, agentID)
	m := make(map[string]string, len(existing))
	for _, f := range existing {
		m[f.FileName] = f.Content
	}
	return m
}
