package party

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// BuildPersonaSystemPrompt builds the system prompt for a persona in a party round.
func BuildPersonaSystemPrompt(persona PersonaInfo, session *store.PartySessionData, soulMD string) string {
	var sb strings.Builder

	// Persona identity
	sb.WriteString(soulMD)
	sb.WriteString("\n\n")

	// Party context
	sb.WriteString("<party-context>\n")
	sb.WriteString(fmt.Sprintf("<topic>%s</topic>\n", session.Topic))

	var ctx map[string]json.RawMessage
	if json.Unmarshal(session.Context, &ctx) == nil {
		if docs, ok := ctx["documents"]; ok {
			sb.WriteString("<documents>\n")
			sb.Write(docs)
			sb.WriteString("\n</documents>\n")
		}
		if code, ok := ctx["codebase"]; ok {
			sb.WriteString("<codebase>\n")
			sb.Write(code)
			sb.WriteString("\n</codebase>\n")
		}
		if notes, ok := ctx["meeting_notes"]; ok {
			sb.WriteString("<meeting-notes>\n")
			sb.Write(notes)
			sb.WriteString("\n</meeting-notes>\n")
		}
		if custom, ok := ctx["custom"]; ok {
			sb.WriteString("<additional>\n")
			sb.Write(custom)
			sb.WriteString("\n</additional>\n")
		}
	}
	sb.WriteString("</party-context>\n\n")

	// Round history (sliding window — last 3 rounds)
	var history []RoundResult
	if json.Unmarshal(session.History, &history) == nil && len(history) > 0 {
		start := 0
		if len(history) > 3 {
			start = len(history) - 3
		}
		sb.WriteString("<round-history>\n")
		for _, r := range history[start:] {
			sb.WriteString(fmt.Sprintf("Round %d [%s]:\n", r.Round, r.Mode))
			for _, m := range r.Messages {
				sb.WriteString(fmt.Sprintf("  %s %s: %s\n", m.Emoji, m.DisplayName, truncate(m.Content, 500)))
			}
		}
		sb.WriteString("</round-history>\n")
	}

	return sb.String()
}

// BuildStandardRoundPrompt builds the user message for a standard round.
func BuildStandardRoundPrompt(session *store.PartySessionData, personas []PersonaInfo) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Round %d discussion about: %s\n\n", session.Round, session.Topic))
	sb.WriteString("Respond as each of these personas IN CHARACTER. Each persona gives their expert analysis.\n")
	sb.WriteString("Format each response as: {emoji} {name}: {response}\n")
	sb.WriteString("Encourage genuine disagreement where expertise conflicts.\n\n")
	sb.WriteString("Personas:\n")
	for _, p := range personas {
		sb.WriteString(fmt.Sprintf("- %s %s (%s)\n", p.Emoji, p.DisplayName, p.SpeakingStyle))
	}
	return sb.String()
}

// BuildThinkingPrompt builds the user message for Deep Mode Step 1 (independent thinking).
func BuildThinkingPrompt(session *store.PartySessionData, persona PersonaInfo) string {
	return fmt.Sprintf(
		"Round %d: Think independently about \"%s\".\n"+
			"Share your analysis from your %s expertise.\n"+
			"Be specific, cite relevant standards/principles.\n"+
			"Identify risks, opportunities, and trade-offs.\n"+
			"Stay completely in character as %s.",
		session.Round, session.Topic, persona.DisplayName, persona.DisplayName)
}

// BuildCrossTalkPrompt builds the prompt for Deep Mode Step 2 (cross-talk from collected thoughts).
func BuildCrossTalkPrompt(session *store.PartySessionData, personas []PersonaInfo, thoughts []PersonaThought) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Round %d cross-talk about: %s\n\n", session.Round, session.Topic))
	sb.WriteString("Each persona has shared their independent thinking below.\n")
	sb.WriteString("Now generate cross-talk: personas respond to EACH OTHER.\n")
	sb.WriteString("Challenge disagreements explicitly. Build on agreements.\n")
	sb.WriteString("Stay in character. Format: {emoji} {name}: {response}\n\n")

	sb.WriteString("<persona-thoughts>\n")
	for _, t := range thoughts {
		sb.WriteString(fmt.Sprintf("<%s>\n%s\n</%s>\n", t.PersonaKey, t.Content, t.PersonaKey))
	}
	sb.WriteString("</persona-thoughts>\n")
	return sb.String()
}

// BuildTokenRingTurnPrompt builds the prompt for one persona's turn in Token-Ring mode.
func BuildTokenRingTurnPrompt(session *store.PartySessionData, persona PersonaInfo, thoughts []PersonaThought, priorTurns []PersonaMessage, isLast bool) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Round %d, your turn in the discussion about: %s\n\n", session.Round, session.Topic))

	sb.WriteString("Independent thoughts from all personas:\n")
	for _, t := range thoughts {
		sb.WriteString(fmt.Sprintf("- %s: %s\n", t.PersonaKey, truncate(t.Content, 300)))
	}

	if len(priorTurns) > 0 {
		sb.WriteString("\nPrior responses this round:\n")
		for _, m := range priorTurns {
			sb.WriteString(fmt.Sprintf("  %s %s: %s\n", m.Emoji, m.DisplayName, m.Content))
		}
		sb.WriteString("\nRespond to what others have said. Challenge or build on their points.\n")
	}

	if isLast {
		sb.WriteString("\nYou are the LAST speaker. Synthesize: what does the team agree on? What remains unresolved?\n")
	}

	sb.WriteString("\nStay completely in character. Be direct and specific.")
	return sb.String()
}

// BuildSummaryPrompt builds the prompt for generating the discussion summary.
func BuildSummaryPrompt(session *store.PartySessionData) string {
	return fmt.Sprintf(`Summarize this party mode discussion.

Topic: %s
Rounds: %d

Discussion history:
%s

Generate a structured summary with:
1. Points of Agreement (unanimous decisions)
2. Points of Disagreement (who disagrees, why)
3. Decisions Made
4. Action Items (action, assignee persona, deadline suggestion, checkpoint link)
5. Compliance Notes (if any security/PCI-DSS/SBV items)

Format as clean markdown.`, session.Topic, session.Round, string(session.History))
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
