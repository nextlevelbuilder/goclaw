package http

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
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
}

// fileTagRe parses <file name="SOUL.md">content</file> from LLM output.
var fileTagRe = regexp.MustCompile(`(?s)<file\s+name="([^"]+)">\s*(.*?)\s*</file>`)

// identityNameRe extracts the Name field from IDENTITY.md format: - **Name:** value
var identityNameRe = regexp.MustCompile(`(?m)^-\s*\*\*Name:\*\*\s*(.+)$`)

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

// SummonAgent generates context files from a natural language description.
// Meant to be called as a goroutine: go summoner.SummonAgent(...)
// Generates SOUL.md first, then IDENTITY.md + USER_PREDEFINED.md using SOUL.md as context,
// emitting progress events after each file so the UI can show incremental progress.
// On retry, skips files that were already generated (differ from template).
// On success: stores generated files and sets agent status to "active".
// On failure: keeps template files (already seeded) and sets status to store.AgentStatusSummonFailed.
func (s *AgentSummoner) SummonAgent(agentID uuid.UUID, providerName, model, description string) {
	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Second)
	defer cancel()

	s.ensureUserPredefined(ctx, agentID)

	s.emitEvent(agentID, SummonEventStarted, "", "")

	// Check which files already exist (from a previous partial run)
	existing, _ := s.agents.GetAgentContextFiles(ctx, agentID)
	existingMap := make(map[string]string, len(existing))
	for _, f := range existing {
		existingMap[f.FileName] = f.Content
	}

	var soulContent string
	var frontmatter string

	// Step 1: Generate SOUL.md (skip if already generated, i.e. differs from template)
	if s.isGenerated(existingMap, bootstrap.SoulFile) {
		soulContent = existingMap[bootstrap.SoulFile]
		slog.Info("summoning: SOUL.md already generated, skipping", "agent", agentID)
		s.emitEvent(agentID, SummonEventFileGenerated, bootstrap.SoulFile, "")
	} else {
		soulFiles, err := s.generateFiles(ctx, providerName, model, s.buildSoulPrompt(description))
		if err != nil {
			slog.Warn("summoning: SOUL.md generation failed", "agent", agentID, "error", err)
			s.emitEvent(agentID, SummonEventFailed, "", err.Error())
			s.setAgentStatus(context.Background(), agentID, store.AgentStatusSummonFailed)
			return
		}

		soulContent = soulFiles[bootstrap.SoulFile]
		frontmatter = soulFiles[frontmatterKey]
		if soulContent != "" {
			if err := s.agents.SetAgentContextFile(ctx, agentID, bootstrap.SoulFile, soulContent); err != nil {
				slog.Warn("summoning: failed to store SOUL.md", "agent", agentID, "error", err)
			} else {
				s.emitEvent(agentID, SummonEventFileGenerated, bootstrap.SoulFile, "")
			}
		}
	}

	// Step 2: Generate IDENTITY.md + USER_PREDEFINED.md using SOUL.md as context
	identityNeeded := !s.isGenerated(existingMap, bootstrap.IdentityFile)
	userPredNeeded := !s.isGenerated(existingMap, bootstrap.UserPredefinedFile)

	var identityContent string
	if !identityNeeded && !userPredNeeded {
		// Both already generated from a previous run
		identityContent = existingMap[bootstrap.IdentityFile]
		slog.Info("summoning: IDENTITY.md + USER_PREDEFINED.md already generated, skipping", "agent", agentID)
		s.emitEvent(agentID, SummonEventFileGenerated, bootstrap.IdentityFile, "")
	} else {
		idFiles, err := s.generateFiles(ctx, providerName, model, s.buildIdentityPrompt(description, soulContent))
		if err != nil {
			slog.Warn("summoning: IDENTITY.md generation failed", "agent", agentID, "error", err)
			s.emitEvent(agentID, SummonEventFailed, "", err.Error())
			s.setAgentStatus(context.Background(), agentID, store.AgentStatusSummonFailed)
			return
		}

		identityContent = idFiles[bootstrap.IdentityFile]
		if frontmatter == "" {
			frontmatter = idFiles[frontmatterKey]
		}

		// Store IDENTITY.md
		if identityContent != "" && identityNeeded {
			if err := s.agents.SetAgentContextFile(ctx, agentID, bootstrap.IdentityFile, identityContent); err != nil {
				slog.Warn("summoning: failed to store IDENTITY.md", "agent", agentID, "error", err)
			} else {
				s.emitEvent(agentID, SummonEventFileGenerated, bootstrap.IdentityFile, "")
			}
		}

		// Store USER_PREDEFINED.md (optional — only if LLM generated it)
		if upContent := idFiles[bootstrap.UserPredefinedFile]; upContent != "" && userPredNeeded {
			if err := s.agents.SetAgentContextFile(ctx, agentID, bootstrap.UserPredefinedFile, upContent); err != nil {
				slog.Warn("summoning: failed to store USER_PREDEFINED.md", "agent", agentID, "error", err)
			} else {
				s.emitEvent(agentID, SummonEventFileGenerated, bootstrap.UserPredefinedFile, "")
			}
		}
	}

	// Save frontmatter + display_name
	updates := map[string]any{}
	if frontmatter == "" {
		frontmatter = truncateUTF8(description, 200)
	}
	if frontmatter != "" {
		updates["frontmatter"] = frontmatter
	}
	if name := extractIdentityName(identityContent); name != "" {
		updates["display_name"] = name
	}
	if len(updates) > 0 {
		if err := s.agents.Update(ctx, agentID, updates); err != nil {
			slog.Warn("summoning: failed to save agent metadata", "agent", agentID, "error", err)
		}
	}

	s.setAgentStatus(ctx, agentID, store.AgentStatusActive)
	s.emitEvent(agentID, SummonEventCompleted, "", "")

	slog.Info("summoning: completed", "agent", agentID)
}

// RegenerateAgent updates context files based on an edit prompt.
// Reads existing files, sends them + edit instructions to LLM, stores results.
// Synchronous — caller should run in goroutine if needed.
func (s *AgentSummoner) RegenerateAgent(agentID uuid.UUID, providerName, model, editPrompt string) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	s.ensureUserPredefined(ctx, agentID)

	s.emitEvent(agentID, SummonEventStarted, "", "")

	// Read existing files for context
	existing, err := s.agents.GetAgentContextFiles(ctx, agentID)
	if err != nil {
		slog.Warn("summoning: failed to read existing files", "agent", agentID, "error", err)
		s.emitEvent(agentID, SummonEventFailed, "", err.Error())
		s.setAgentStatus(context.Background(), agentID, store.AgentStatusSummonFailed)
		return
	}

	prompt := s.buildEditPrompt(existing, editPrompt)

	files, err := s.generateFiles(ctx, providerName, model, prompt)
	if err != nil {
		slog.Warn("summoning: regeneration failed", "agent", agentID, "error", err)
		s.emitEvent(agentID, SummonEventFailed, "", err.Error())
		// Use fresh context — the original may have timed out, but we still need to update status.
		s.setAgentStatus(context.Background(), agentID, store.AgentStatusSummonFailed)
		return
	}

	s.storeFiles(ctx, agentID, files)

	// Update frontmatter + display_name if IDENTITY.md was regenerated
	updates := map[string]any{}
	if fm, ok := files[frontmatterKey]; ok && fm != "" {
		updates["frontmatter"] = fm
	}
	if name := extractIdentityName(files[bootstrap.IdentityFile]); name != "" {
		updates["display_name"] = name
	}
	if len(updates) > 0 {
		if err := s.agents.Update(ctx, agentID, updates); err != nil {
			slog.Warn("summoning: failed to save agent metadata", "agent", agentID, "error", err)
		}
	}

	s.setAgentStatus(ctx, agentID, store.AgentStatusActive)
	s.emitEvent(agentID, SummonEventCompleted, "", "")

	slog.Info("summoning: regeneration completed", "agent", agentID, "files", len(files))
}

// isGenerated checks if a context file has been generated (differs from the default template).
func (s *AgentSummoner) isGenerated(existingMap map[string]string, fileName string) bool {
	content, ok := existingMap[fileName]
	if !ok || content == "" {
		return false
	}
	template, err := bootstrap.ReadTemplate(fileName)
	if err != nil {
		return false
	}
	return content != template
}

// generateFiles calls the LLM and parses the XML-tagged response into file map.
func (s *AgentSummoner) generateFiles(ctx context.Context, providerName, model, prompt string) (map[string]string, error) {
	provider, err := s.resolveProvider(providerName)
	if err != nil {
		return nil, fmt.Errorf("resolve provider: %w", err)
	}

	slog.Info("summoning: calling LLM", "provider", providerName, "model", model, "prompt_len", len(prompt))

	resp, err := provider.Chat(ctx, providers.ChatRequest{
		Messages: []providers.Message{
			{Role: "user", Content: prompt},
		},
		Model: model,
		Options: map[string]interface{}{
			"max_tokens":  8192,
			"temperature": 0.7,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", providerName, err)
	}

	slog.Info("summoning: LLM responded", "provider", providerName, "content_len", len(resp.Content))

	files := parseFileResponse(resp.Content)
	if len(files) == 0 {
		return nil, fmt.Errorf("LLM returned no parseable files (response length: %d)", len(resp.Content))
	}

	return files, nil
}

// storeFiles saves generated files to agent_context_files and emits progress events.
func (s *AgentSummoner) storeFiles(ctx context.Context, agentID uuid.UUID, files map[string]string) {
	for _, name := range summoningFiles {
		content, ok := files[name]
		if !ok || content == "" {
			continue
		}
		if err := s.agents.SetAgentContextFile(ctx, agentID, name, content); err != nil {
			slog.Warn("summoning: failed to store file", "agent", agentID, "file", name, "error", err)
			continue
		}
		s.emitEvent(agentID, SummonEventFileGenerated, name, "")
	}
}

func (s *AgentSummoner) resolveProvider(name string) (providers.Provider, error) {
	if s.providerReg == nil {
		return nil, fmt.Errorf("no provider registry")
	}

	provider, err := s.providerReg.Get(name)
	if err != nil {
		// Fallback to first available provider
		names := s.providerReg.List()
		if len(names) == 0 {
			return nil, fmt.Errorf("no providers configured")
		}
		provider, err = s.providerReg.Get(names[0])
		if err != nil {
			return nil, err
		}
		slog.Warn("summoning: provider not found, using fallback", "wanted", name, "using", names[0])
	}
	return provider, nil
}

// ensureUserPredefined seeds USER_PREDEFINED.md template if it doesn't exist yet.
// Backfills agents created before this feature was added.
func (s *AgentSummoner) ensureUserPredefined(ctx context.Context, agentID uuid.UUID) {
	existing, err := s.agents.GetAgentContextFiles(ctx, agentID)
	if err != nil {
		return
	}
	for _, f := range existing {
		if f.FileName == bootstrap.UserPredefinedFile {
			return // already exists
		}
	}
	if tpl, err := bootstrap.ReadTemplate(bootstrap.UserPredefinedFile); err == nil {
		_ = s.agents.SetAgentContextFile(ctx, agentID, bootstrap.UserPredefinedFile, tpl)
	}
}

func (s *AgentSummoner) setAgentStatus(ctx context.Context, agentID uuid.UUID, status string) {
	if err := s.agents.Update(ctx, agentID, map[string]any{"status": status}); err != nil {
		slog.Warn("summoning: failed to update agent status", "agent", agentID, "status", status, "error", err)
	}
}

func (s *AgentSummoner) emitEvent(agentID uuid.UUID, eventType, fileName, errMsg string) {
	if s.msgBus == nil {
		return
	}
	payload := map[string]interface{}{
		"type":     eventType,
		"agent_id": agentID.String(),
	}
	if fileName != "" {
		payload["file"] = fileName
	}
	if errMsg != "" {
		payload["error"] = errMsg
	}
	s.msgBus.Broadcast(bus.Event{
		Name:    protocol.EventAgentSummoning,
		Payload: payload,
	})
}

// buildSoulPrompt constructs the prompt for SOUL.md generation.
func (s *AgentSummoner) buildSoulPrompt(description string) string {
	var sb strings.Builder
	sb.WriteString("You are setting up a new AI assistant. Based on the description below, generate the SOUL.md file that defines its personality.\n\n")

	fmt.Fprintf(&sb, "<description>\n%s\n</description>\n\n", description)

	soulTemplate, err := bootstrap.ReadTemplate(bootstrap.SoulFile)
	if err != nil {
		slog.Warn("summoning: failed to read SOUL.md template", "error", err)
	}
	if soulTemplate != "" {
		fmt.Fprintf(&sb, "<template>\n%s\n</template>\n\n", soulTemplate)
	}

	sb.WriteString(`IMPORTANT RULES:

1. Language: Write ALL content in the SAME LANGUAGE as the <description>. If description is in Vietnamese, write in Vietnamese. If in English, write in English. BUT keep ALL headings and section titles in English exactly as in the templates.

2. SOUL.md section guide — each section has a specific purpose:
   - "## Core Truths" — universal personality traits. KEEP the general advice. Do NOT inject agent-specific references here.
   - "## Boundaries" — rules and limits. CUSTOMIZE only if the description mentions specific boundaries.
   - "## Vibe" — communication style and personality ONLY. How the agent talks, its tone, its attitude. Do NOT put technical knowledge here.
   - "## Style" — communication preferences: tone, humor level, emoji usage, opinion strength, response length, formality. Generate SPECIFIC values based on the description. E.g. a cute sweet bot → warm tone, frequent emoji, playful humor. A formal business bot → professional tone, no emoji, measured opinions. These are knobs the user can later customize per agent.
   - "## Expertise" — domain-specific knowledge, technical skills, specialized instructions, keywords, parameters. If the description mentions any specialized domain (e.g. image generation, coding, writing), put that knowledge HERE. Remove the placeholder text. If no domain expertise, omit this section entirely.
   - "## Continuity" — keep as-is (just translate if needed).
   - KEEP the exact English headings. Do NOT add the agent's name into Core Truths or Boundaries.

3. Generate a short expertise summary (1-2 sentences, under 200 characters) for delegation discovery.

Output format:

<frontmatter>
(short expertise summary here)
</frontmatter>

<file name="SOUL.md">
(content here)
</file>`)

	return sb.String()
}

// buildIdentityPrompt constructs the prompt for IDENTITY.md + USER_PREDEFINED.md generation,
// using the already-generated SOUL.md as context for consistency.
func (s *AgentSummoner) buildIdentityPrompt(description, soulContent string) string {
	var sb strings.Builder
	sb.WriteString("You are setting up a new AI assistant. The SOUL.md (personality) has already been generated. Now generate IDENTITY.md and optionally USER_PREDEFINED.md based on the description and soul.\n\n")

	fmt.Fprintf(&sb, "<description>\n%s\n</description>\n\n", description)

	if soulContent != "" {
		fmt.Fprintf(&sb, "<soul>\n%s\n</soul>\n\n", soulContent)
	}

	identityTemplate, err := bootstrap.ReadTemplate(bootstrap.IdentityFile)
	if err != nil {
		slog.Warn("summoning: failed to read IDENTITY.md template", "error", err)
	}
	userPredefinedTemplate, err := bootstrap.ReadTemplate(bootstrap.UserPredefinedFile)
	if err != nil {
		slog.Warn("summoning: failed to read USER_PREDEFINED.md template", "error", err)
	}

	sb.WriteString("<templates>\n")
	if identityTemplate != "" {
		fmt.Fprintf(&sb, "<file name=\"IDENTITY.md\">\n%s\n</file>\n", identityTemplate)
	}
	if userPredefinedTemplate != "" {
		fmt.Fprintf(&sb, "<file name=\"USER_PREDEFINED.md\">\n%s\n</file>\n", userPredefinedTemplate)
	}
	sb.WriteString("</templates>\n\n")

	sb.WriteString(`IMPORTANT RULES:

1. Language: Write ALL content in the SAME LANGUAGE as the <description>. Keep headings in English.

2. IDENTITY.md rules:
   - KEEP the exact English heading: "# IDENTITY.md - Who Am I?"
   - Fill in ONLY the field values: Name, Creature, Purpose, Vibe, Emoji based on the description and soul.
   - The Name, Creature, and Vibe should MATCH the personality defined in the soul.
   - Purpose: mission statement — what this agent does, key resources, focus areas. Can be multiple lines. Include URLs or references mentioned in the description.
   - REMOVE all template placeholder/instruction text (the italic hints in parentheses).
   - Leave Avatar blank.
   - Keep the footer note section as-is.

3. USER_PREDEFINED.md (OPTIONAL — only generate if relevant):
   - Generate this file if the description mentions ANY of: owner/creator info (name, username, role), target users/audience, user groups, communication policies, language requirements, or group-specific context.
   - This is the RIGHT place for information about specific people (owner, creator, team members, contacts) — do NOT put personal/people info in IDENTITY.md.
   - If the description is purely about the agent's personality/expertise with no people or user-context, OMIT this file entirely.
   - Content: owner/creator info, baseline rules for ALL users — default language, communication norms, audience assumptions, boundaries that individual users cannot override.
   - Keep headings in English. Write content in the same language as the description.

Output format:

<file name="IDENTITY.md">
(content here)
</file>

<file name="USER_PREDEFINED.md">
(content here — or omit this entire block if not relevant)
</file>`)

	return sb.String()
}

// buildEditPrompt constructs the prompt for editing existing SOUL.md, IDENTITY.md, and USER_PREDEFINED.md.
func (s *AgentSummoner) buildEditPrompt(existing []store.AgentContextFileData, editPrompt string) string {
	var sb strings.Builder
	sb.WriteString("You are updating an existing AI assistant's configuration files.\n\nHere are the current files:\n\n<current_files>\n")
	for _, f := range existing {
		if f.Content == "" {
			continue
		}
		// Only include editable files
		if f.FileName != bootstrap.SoulFile && f.FileName != bootstrap.IdentityFile && f.FileName != bootstrap.UserPredefinedFile {
			continue
		}
		fmt.Fprintf(&sb, "<file name=%q>\n%s\n</file>\n", f.FileName, f.Content)
	}
	sb.WriteString("</current_files>\n\n")
	fmt.Fprintf(&sb, "<edit_instructions>\n%s\n</edit_instructions>\n\n", editPrompt)
	sb.WriteString(`IMPORTANT RULES:

1. Language: Write ALL content in the SAME LANGUAGE as the existing files. Keep headings in English.

2. SOUL.md section guide — place content in the RIGHT section:
   - "## Core Truths" — universal personality traits. Do NOT add domain-specific content here.
   - "## Boundaries" — rules and limits.
   - "## Vibe" — communication style and personality ONLY. Tone, attitude, how the agent talks. NOT technical knowledge.
   - "## Style" — communication preferences (tone, humor, emoji, opinions, length, formality). Update if the edit changes personality or communication style.
   - "## Expertise" — domain-specific knowledge, technical skills, keywords, parameters, specialized instructions. If the edit adds domain knowledge (e.g. image generation techniques, coding standards, writing styles), it goes HERE. Create this section if it doesn't exist yet (between Vibe and Continuity).
   - "## Continuity" — memory/persistence rules. Usually unchanged.

3. Output the COMPLETE updated file content, not just the changed parts. The output will REPLACE the entire file.

4. Only output files that actually need changes. Omit unchanged files entirely.

5. If the edit changes the agent's expertise, also update the frontmatter summary.

6. USER_PREDEFINED.md: If the edit mentions owner/creator info (name, username, role), people information, user-facing policies, communication rules, audience targeting, or group-specific context, update this file. This is the RIGHT place for information about specific people — do NOT put personal/people info in SOUL.md or IDENTITY.md. If it doesn't exist yet in current_files and the edit introduces people or user/group context, generate it. Omit if unchanged and not newly needed.

Output format:

<frontmatter>
(updated expertise summary, or omit if unchanged)
</frontmatter>

<file name="SOUL.md">
(complete updated content, or omit if unchanged)
</file>

<file name="IDENTITY.md">
(complete updated content, or omit if unchanged)
</file>

<file name="USER_PREDEFINED.md">
(complete updated content, or omit if unchanged/not needed)
</file>
`)
	return sb.String()
}

// extractIdentityName extracts the Name field from IDENTITY.md content.
// Matches format: - **Name:** value
func extractIdentityName(content string) string {
	if content == "" {
		return ""
	}
	m := identityNameRe.FindStringSubmatch(content)
	if len(m) < 2 {
		return ""
	}
	return strings.TrimSpace(m[1])
}

// truncateUTF8 truncates s to at most maxLen runes, appending "…" if truncated.
func truncateUTF8(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "…"
}

// parseFileResponse extracts file contents and frontmatter from XML-tagged LLM output.
// Frontmatter is stored under the special key "__frontmatter__".
func parseFileResponse(content string) map[string]string {
	files := make(map[string]string)
	matches := fileTagRe.FindAllStringSubmatch(content, -1)
	for _, m := range matches {
		name := strings.TrimSpace(m[1])
		body := strings.TrimSpace(m[2])
		if name != "" && body != "" {
			files[name] = body
		}
	}
	// Extract frontmatter tag if present
	if fm := frontmatterTagRe.FindStringSubmatch(content); len(fm) > 1 {
		if trimmed := strings.TrimSpace(fm[1]); trimmed != "" {
			files[frontmatterKey] = trimmed
		}
	}
	return files
}
