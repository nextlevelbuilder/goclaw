package http

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/tokencount"
)

// promptPreviewSection represents a named section in the system prompt.
type promptPreviewSection struct {
	Name  string `json:"name"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

// promptPreviewResponse is the API response for system prompt preview.
type promptPreviewResponse struct {
	Mode       string                 `json:"mode"`
	Prompt     string                 `json:"prompt"`
	TokenCount int                    `json:"token_count"`
	Sections   []promptPreviewSection `json:"sections"`
}

// handleSystemPromptPreview renders the actual system prompt for an agent in a given mode.
// GET /v1/agents/{id}/system-prompt-preview?mode=full|task|minimal|none&user_id=xxx
func (h *AgentsHandler) handleSystemPromptPreview(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("id")
	mode := agent.PromptMode(r.URL.Query().Get("mode"))
	switch mode {
	case agent.PromptFull, agent.PromptTask, agent.PromptMinimal, agent.PromptNone:
		// valid
	case "":
		mode = agent.PromptFull
	default:
		http.Error(w, "invalid mode: must be full, task, minimal, or none", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	ag, err := h.agents.GetByKey(ctx, agentID)
	if err != nil {
		http.Error(w, "agent not found", http.StatusNotFound)
		return
	}

	// Build preview prompt — reuses same BuildSystemPrompt() as LLM pipeline.
	// Runtime-only fields (channel, peer kind, credentials) are zero-valued;
	// BuildSystemPrompt nil-checks every field so these sections are simply skipped.
	prompt := agent.BuildPreviewPrompt(ctx, ag, mode, r.URL.Query().Get("user_id"), agent.PreviewDeps{
		AgentStore:   h.agents,
		TeamStore:    h.teamStore,
		AgentLinks:   h.agentLinkStore,
		ProviderReg:  h.providerReg,
		ToolLister:   h.toolsReg,
		SkillsLoader: h.skillsLoader,
		DataDir:      h.dataDir,
	})

	counter := tokencount.NewFallbackCounter()
	tokens := counter.Count("claude-3", prompt)
	sections := parseSections(prompt)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(promptPreviewResponse{
		Mode:       string(mode),
		Prompt:     prompt,
		TokenCount: tokens,
		Sections:   sections,
	})
}

// parseSections extracts section boundaries from ## markdown headers.
func parseSections(prompt string) []promptPreviewSection {
	var sections []promptPreviewSection
	lines := strings.Split(prompt, "\n")
	pos := 0
	for _, line := range lines {
		if strings.HasPrefix(line, "## ") || strings.HasPrefix(line, "# ") {
			name := strings.TrimPrefix(strings.TrimPrefix(line, "## "), "# ")
			sections = append(sections, promptPreviewSection{
				Name:  name,
				Start: pos,
			})
			if len(sections) > 1 {
				sections[len(sections)-2].End = pos - 1
			}
		}
		pos += len(line) + 1
	}
	if len(sections) > 0 {
		sections[len(sections)-1].End = len(prompt)
	}
	return sections
}
