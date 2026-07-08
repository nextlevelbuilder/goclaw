package http

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/mcp"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/tokencount"
)

// mcpPreviewAdapter wraps *mcp.Manager to satisfy agent.MCPPreviewLister.
// It converts mcp.MCPToolPreviewInfo to agent.MCPToolPreviewInfo so the
// agent package does not need to import the mcp package (which would cycle).
type mcpPreviewAdapter struct {
	mgr *mcp.Manager
}

// NewMCPPreviewAdapter wraps an *mcp.Manager as an agent.MCPPreviewLister.
// Use this when wiring up the prompt preview handler in cmd/gateway.go.
func NewMCPPreviewAdapter(mgr *mcp.Manager) agent.MCPPreviewLister {
	slog.Debug("NewMCPPreviewAdapter called", "mgr_nil", mgr == nil)
	if mgr == nil {
		slog.Warn("NewMCPPreviewAdapter: mgr is nil — MCP tools will not appear in prompt preview")
	} else {
		slog.Debug("NewMCPPreviewAdapter: MCP preview adapter created with manager")
	}
	return &mcpPreviewAdapter{mgr: mgr}
}

// ListToolsForAgent implements agent.MCPPreviewLister.
func (a *mcpPreviewAdapter) ListToolsForAgent(ctx context.Context, agentID uuid.UUID, userID string) ([]agent.MCPToolPreviewInfo, error) {
	mcpTools, err := a.mgr.ListToolsForAgent(ctx, agentID, userID)
	if err != nil {
		return nil, err
	}
	result := make([]agent.MCPToolPreviewInfo, 0, len(mcpTools))
	for _, mt := range mcpTools {
		result = append(result, agent.MCPToolPreviewInfo{
			RegisteredName: mt.RegisteredName,
			Description:    mt.Description,
			Parameters:     mt.Parameters,
		})
	}
	return result, nil
}

// promptPreviewSection represents a named section in the system prompt.
type promptPreviewSection struct {
	Name  string `json:"name"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

// promptPreviewResponse is the API response for system prompt preview.
type promptPreviewResponse struct {
	Mode       string                      `json:"mode"`
	Prompt     string                      `json:"prompt"`
	TokenCount int                         `json:"token_count"`
	Sections   []promptPreviewSection      `json:"sections"`
	Tools      []providers.ToolDefinition  `json:"tools,omitempty"`
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
	// Load per-tenant disabled tools for this agent's tenant.
	var disabledTools map[string]bool
	if h.disabledToolsStore != nil && ag.TenantID != uuid.Nil {
		if disabled, err := h.disabledToolsStore.ListDisabled(ctx, ag.TenantID); err == nil && len(disabled) > 0 {
			disabledTools = make(map[string]bool, len(disabled))
			for _, name := range disabled {
				disabledTools[name] = true
			}
			slog.Debug("handleSystemPromptPreview.disabled_tools", "agent_id", ag.ID, "tenant", ag.TenantID, "disabled", len(disabled))
		}
	}

	slog.Debug("handleSystemPromptPreview.mcp_lister", "agent_id", ag.ID, "mcp_lister_nil", h.mcpPreviewMgr == nil)
	result := agent.BuildPreviewPrompt(ctx, ag, mode, r.URL.Query().Get("user_id"), agent.PreviewDeps{
		AgentStore:       h.agents,
		TeamStore:        h.teamStore,
		AgentLinks:       h.agentLinkStore,
		ProviderReg:      h.providerReg,
		ToolLister:       h.toolsReg,
		ToolPolicy:       h.toolPE,
		SkillsLoader:     h.skillsLoader,
		SkillAccessStore: h.skillAccessStore,
		MCPLister:        h.mcpPreviewMgr,
		DisabledTools:    disabledTools,
		DataDir:          h.dataDir,
	})

	counter := tokencount.NewFallbackCounter()
	tokens := counter.Count("claude-3", result.Prompt)
	sections := parseSections(result.Prompt)

	// Log MCP section presence for debugging
	mcpStart := strings.Index(result.Prompt, "mcp_")
	if mcpStart >= 0 {
		end := mcpStart + 500
		if end > len(result.Prompt) {
			end = len(result.Prompt)
		}
		slog.Debug("handleSystemPromptPreview.mcp_section_found", "agent_id", ag.ID, "preview", result.Prompt[mcpStart:end])
	} else {
		slog.Debug("handleSystemPromptPreview.no_mcp_section", "agent_id", ag.ID, "prompt_len", len(result.Prompt))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(promptPreviewResponse{
		Mode:       string(mode),
		Prompt:     result.Prompt,
		TokenCount: tokens,
		Sections:   sections,
		Tools:      result.ToolDefs,
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
