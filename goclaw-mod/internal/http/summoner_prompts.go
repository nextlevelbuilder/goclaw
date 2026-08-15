package http

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

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

	capTemplate, err := bootstrap.ReadTemplate(bootstrap.CapabilitiesFile)
	if err != nil {
		slog.Warn("summoning: failed to read CAPABILITIES.md template", "error", err)
	}
	if capTemplate != "" {
		fmt.Fprintf(&sb, "<template name=\"CAPABILITIES.md\">\n%s\n</template>\n\n", capTemplate)
	}

	sb.WriteString(`IMPORTANT RULES:

1. Language: Write ALL content in the SAME LANGUAGE as the <description>. If description is in Vietnamese, write in Vietnamese. If in English, write in English. BUT keep ALL headings and section titles in English exactly as in the templates.

2. SOUL.md section guide — each section has a specific purpose:
   - "## Core Truths" — universal personality traits. KEEP the general advice. Do NOT inject agent-specific references here.
   - "## Boundaries" — rules and limits. CUSTOMIZE only if the description mentions specific boundaries.
   - "## Vibe" — communication style and personality ONLY. How the agent talks, its tone, its attitude. Do NOT put technical knowledge here.
   - "## Style" — communication preferences: tone, humor level, emoji usage, opinion strength, response length, formality. Generate SPECIFIC values based on the description. E.g. a cute sweet bot → warm tone, frequent emoji, playful humor. A formal business bot → professional tone, no emoji, measured opinions. These are knobs the user can later customize per agent.
   - Do NOT put domain expertise in SOUL.md — that goes in CAPABILITIES.md.
   - "## Continuity" — keep as-is (just translate if needed).
   - KEEP the exact English headings. Do NOT add the agent's name into Core Truths or Boundaries.

3. CAPABILITIES.md — domain expertise and technical skills:
   - "## Expertise" — domain-specific knowledge, technical skills, specialized instructions, keywords, parameters. If the description mentions any specialized domain (e.g. image generation, coding, writing), put that knowledge HERE.
   - "## Tools & Methods" — preferred workflows, methodologies. Only if mentioned in description.
   - If no domain expertise, generate a minimal CAPABILITIES.md with just the Expertise heading and a brief note.

4. Generate a short expertise summary (1-2 sentences, under 200 characters) for delegation discovery.

Output format:

<frontmatter>
(short expertise summary here)
</frontmatter>

<file name="SOUL.md">
(content here)
</file>

<file name="CAPABILITIES.md">
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
   - Purpose: mission statement only — what this agent does. Do NOT include domain expertise (that's CAPABILITIES.md). Can be multiple lines. Include URLs or references mentioned in the description.
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

// buildCreatePrompt constructs the prompt for generating all files in a single LLM call.
// Used by the optimistic single-call path; generates frontmatter + SOUL.md + IDENTITY.md + USER_PREDEFINED.md.
func (s *AgentSummoner) buildCreatePrompt(description string) string {
	var sb strings.Builder
	sb.WriteString("You are setting up a new AI assistant. Based on the description below, generate the required files.\n\n")

	fmt.Fprintf(&sb, "<description>\n%s\n</description>\n\n", description)

	soulTemplate, err := bootstrap.ReadTemplate(bootstrap.SoulFile)
	if err != nil {
		slog.Warn("summoning: failed to read SOUL.md template", "error", err)
	}
	identityTemplate, err := bootstrap.ReadTemplate(bootstrap.IdentityFile)
	if err != nil {
		slog.Warn("summoning: failed to read IDENTITY.md template", "error", err)
	}
	userPredefinedTemplate, err := bootstrap.ReadTemplate(bootstrap.UserPredefinedFile)
	if err != nil {
		slog.Warn("summoning: failed to read USER_PREDEFINED.md template", "error", err)
	}
	capTemplate, err := bootstrap.ReadTemplate(bootstrap.CapabilitiesFile)
	if err != nil {
		slog.Warn("summoning: failed to read CAPABILITIES.md template", "error", err)
	}

	sb.WriteString("<templates>\n")
	if soulTemplate != "" {
		fmt.Fprintf(&sb, "<file name=\"SOUL.md\">\n%s\n</file>\n", soulTemplate)
	}
	if identityTemplate != "" {
		fmt.Fprintf(&sb, "<file name=\"IDENTITY.md\">\n%s\n</file>\n", identityTemplate)
	}
	if capTemplate != "" {
		fmt.Fprintf(&sb, "<file name=\"CAPABILITIES.md\">\n%s\n</file>\n", capTemplate)
	}
	if userPredefinedTemplate != "" {
		fmt.Fprintf(&sb, "<file name=\"USER_PREDEFINED.md\">\n%s\n</file>\n", userPredefinedTemplate)
	}
	sb.WriteString("</templates>\n\n")

	sb.WriteString(`IMPORTANT RULES:

1. Language: Write ALL content in the SAME LANGUAGE as the <description>. If description is in Vietnamese, write in Vietnamese. If in English, write in English. BUT keep ALL headings and section titles in English exactly as in the templates.

2. SOUL.md section guide — each section has a specific purpose:
   - "## Core Truths" — universal personality traits. KEEP the general advice. Do NOT inject agent-specific references here.
   - "## Boundaries" — rules and limits. CUSTOMIZE only if the description mentions specific boundaries.
   - "## Vibe" — communication style and personality ONLY. How the agent talks, its tone, its attitude. Do NOT put technical knowledge here.
   - "## Style" — communication preferences: tone, humor level, emoji usage, opinion strength, response length, formality. Generate SPECIFIC values based on the description. E.g. a cute sweet bot → warm tone, frequent emoji, playful humor. A formal business bot → professional tone, no emoji, measured opinions. These are knobs the user can later customize per agent.
   - Do NOT put domain expertise in SOUL.md — that goes in CAPABILITIES.md.
   - "## Continuity" — keep as-is (just translate if needed).
   - KEEP the exact English headings. Do NOT add the agent's name into Core Truths or Boundaries.

3. CAPABILITIES.md — domain expertise and technical skills:
   - "## Expertise" — domain-specific knowledge, technical skills, specialized instructions, keywords, parameters. If the description mentions any specialized domain (e.g. image generation, coding, writing), put that knowledge HERE.
   - "## Tools & Methods" — preferred workflows, methodologies. Only if mentioned in description.
   - If no domain expertise, generate a minimal CAPABILITIES.md with just the Expertise heading and a brief note.

4. IDENTITY.md rules:
   - KEEP the exact English heading: "# IDENTITY.md - Who Am I?"
   - Fill in ONLY the field values: Name, Creature, Purpose, Vibe, Emoji based on the description.
   - Purpose: mission statement only — what this agent does. Do NOT include domain expertise (that's CAPABILITIES.md).
   - REMOVE all template placeholder/instruction text (the italic hints in parentheses).
   - Leave Avatar blank.
   - Keep the footer note section as-is.

5. Generate a short expertise summary (1-2 sentences, under 200 characters) for delegation discovery.

6. USER_PREDEFINED.md (OPTIONAL — only generate if relevant):
   - Generate this file if the description mentions ANY of: owner/creator info (name, username, role), target users/audience, user groups, communication policies, language requirements, or group-specific context.
   - This is the RIGHT place for information about specific people (owner, creator, team members, contacts) — do NOT put personal/people info in SOUL.md or IDENTITY.md.
   - If the description is purely about the agent's personality/expertise with no people or user-context, OMIT this file entirely.
   - Content: owner/creator info, baseline rules for ALL users — default language, communication norms, audience assumptions, boundaries that individual users cannot override.
   - Keep headings in English. Write content in the same language as the description.

Output format — generate in this EXACT order:

<frontmatter>
(short expertise summary here)
</frontmatter>

<file name="SOUL.md">
(content here)
</file>

<file name="IDENTITY.md">
(content here)
</file>

<file name="CAPABILITIES.md">
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
		if f.FileName != bootstrap.SoulFile && f.FileName != bootstrap.IdentityFile && f.FileName != bootstrap.UserPredefinedFile && f.FileName != bootstrap.CapabilitiesFile {
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
   - Do NOT put domain expertise in SOUL.md — that goes in CAPABILITIES.md. If SOUL.md has an "## Expertise" section, migrate it to CAPABILITIES.md.
   - "## Continuity" — memory/persistence rules. Usually unchanged.

2b. CAPABILITIES.md — domain expertise and technical skills:
   - "## Expertise" — domain-specific knowledge, technical skills, keywords, parameters, specialized instructions. If the edit adds domain knowledge (e.g. image generation techniques, coding standards, writing styles), it goes HERE.
   - "## Tools & Methods" — preferred workflows, methodologies.
   - If CAPABILITIES.md doesn't exist in current_files, generate it when the edit adds domain knowledge.

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

<file name="CAPABILITIES.md">
(complete updated content, or omit if unchanged/not needed)
</file>

<file name="USER_PREDEFINED.md">
(complete updated content, or omit if unchanged/not needed)
</file>
`)
	return sb.String()
}
