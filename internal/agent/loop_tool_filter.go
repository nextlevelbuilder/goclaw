package agent

import (
	"slices"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// imageGenToolDef is the native image_generation tool sentinel. Its Type-only form
// is passed through by the Codex/OpenAI request builder as a bare {"type":"image_generation"}
// object — no "function" wrapper, no parameters.
var imageGenToolDef = providers.ToolDefinition{Type: "image_generation"}

func (l *Loop) toolVisibleForChannel(name, channelType string, telegramManagerPermissions []string) bool {
	if name == "telegram_manager" {
		return channelType == "telegram" && len(telegramManagerPermissions) > 0
	}
	if l.tools == nil {
		return true
	}
	tool, ok := l.tools.Get(name)
	if !ok {
		return true
	}
	ca, ok := tool.(tools.ChannelAware)
	if !ok {
		return true
	}
	if channelType == "" {
		return false
	}
	return slices.Contains(ca.RequiredChannelTypes(), channelType)
}

// buildFilteredTools resolves the per-iteration tool definitions based on policy,
// disabled tools, bootstrap mode, skill visibility, channel type, and iteration budget.
// Per-user MCP tools (require_user_credentials servers) are passed in via userTools —
// their objects deliberately live ONLY in l.mcpUserTools (cross-user isolation), NOT
// in the shared registry. They are surfaced via a request-scoped overlay registry so
// the PolicyEngine evaluates AND emits them under the SAME allow/deny rules as
// registry tools; execution routes per-actor via executeToolForActor.
// Returns tool definitions for the provider, an allowed-tools map for execution validation,
// and the (potentially modified) messages slice when final-iteration stripping appends a hint.
func (l *Loop) buildFilteredTools(req *RunRequest, hadBootstrap bool, iteration, maxIter int, messages []providers.Message, userTools []tools.Tool) ([]providers.ToolDefinition, map[string]bool, []providers.Message) {
	// Build provider request with policy-filtered tools.
	var toolDefs []providers.ToolDefinition
	var allowedTools map[string]bool
	if l.toolPolicy != nil {
		// Per-user MCP tool objects are NOT in the shared registry (cross-user
		// isolation — see getUserMCPTools), so FilterTools alone cannot emit them.
		// Wrap the registry in a request-scoped overlay that adds the calling actor's
		// per-user tools: FilterTools then evaluates them through the same policy
		// pipeline (profile/allow/deny, incl. an explicit deny on a per-user tool name)
		// and emits them via overlay.Get. The overlay is local and discarded after this
		// call, so the shared registry — and thus other users — stay unaffected.
		// NewUserToolOverlay returns l.tools unchanged when userTools is empty.
		registry := tools.NewUserToolOverlay(l.tools, userTools)
		toolDefs = l.toolPolicy.FilterTools(registry, l.id, l.provider.Name(), l.agentToolPolicy, req.ToolAllow, false, false)
		allowedTools = make(map[string]bool, len(toolDefs))
		for _, td := range toolDefs {
			if td.Function != nil {
				allowedTools[td.Function.Name] = true
			}
		}
	} else {
		// No policy → all tools allowed. ProviderDefs() omits per-user MCP tools (not in
		// the shared registry), so append their defs directly (dedup by name).
		toolDefs = l.tools.ProviderDefs()
		if len(userTools) > 0 {
			seen := make(map[string]bool, len(toolDefs))
			for _, td := range toolDefs {
				if td.Function != nil {
					seen[td.Function.Name] = true
				}
			}
			for _, t := range userTools {
				if t == nil || seen[t.Name()] {
					continue
				}
				seen[t.Name()] = true
				toolDefs = append(toolDefs, tools.ToProviderDef(t))
			}
		}
	}

	// V3 orchestration mode filtering: hide tools the agent shouldn't see.
	// spawn: no delegate/team_tasks. delegate: no team_tasks. team: all.
	if orchDeny := orchModeDenyTools(l.orchMode); len(orchDeny) > 0 {
		filtered := toolDefs[:0:0]
		for _, td := range toolDefs {
			if td.Function == nil || !orchDeny[td.Function.Name] {
				filtered = append(filtered, td)
			} else {
				if allowedTools != nil {
					delete(allowedTools, td.Function.Name)
				}
			}
		}
		toolDefs = filtered
	}

	// Per-tenant tool exclusions: remove tools disabled for this agent's tenant.
	if len(l.disabledTools) > 0 {
		filtered := toolDefs[:0]
		for _, td := range toolDefs {
			if td.Function == nil || !l.disabledTools[td.Function.Name] {
				filtered = append(filtered, td)
			} else {
				if allowedTools != nil {
					delete(allowedTools, td.Function.Name)
				}
			}
		}
		toolDefs = filtered
	}

	// Bootstrap mode: restrict API tool definitions to write_file only (open agents).
	// Predefined agents keep all tools — BOOTSTRAP.md guides behavior.
	if hadBootstrap && l.agentType != store.AgentTypePredefined {
		var bootstrapDefs []providers.ToolDefinition
		for _, td := range toolDefs {
			if td.Function != nil && bootstrapToolAllowlist[td.Function.Name] {
				bootstrapDefs = append(bootstrapDefs, td)
			}
		}
		toolDefs = bootstrapDefs
	}

	// Hide skill_manage from LLM when skill_evolve is off.
	// Tool stays in the registry (shared) but won't appear in API tool definitions.
	if !l.skillEvolve {
		filtered := toolDefs[:0:0]
		for _, td := range toolDefs {
			if td.Function == nil || td.Function.Name != "skill_manage" {
				filtered = append(filtered, td)
			}
		}
		toolDefs = filtered
	}

	// Hide channel-specific tools when channel type doesn't match. Channel-aware
	// tools are hidden when there is no channel context; telegram_manager also
	// requires explicit channel permissions before it is visible to the LLM.
	filtered := toolDefs[:0:0]
	for _, td := range toolDefs {
		if td.Function != nil && !l.toolVisibleForChannel(td.Function.Name, req.ChannelType, req.TelegramManagerPermissions) {
			if allowedTools != nil {
				delete(allowedTools, td.Function.Name)
			}
			continue
		}
		filtered = append(filtered, td)
	}
	toolDefs = filtered

	// Final iteration: strip all tools to force a text-only response.
	// Without this the model may keep requesting tools and exit with "...".
	if iteration == maxIter {
		toolDefs = nil
		messages = append(messages, providers.Message{
			Role:    "user",
			Content: "[System] Final iteration reached. Summarize all findings and respond to the user now. No more tool calls allowed.",
		})
		return toolDefs, allowedTools, messages
	}

	// Two-tier image generation gate:
	//   (1) provider supports native image_generation (ImageGeneration capability)
	//   (2) agent config allows it (allowImageGeneration — defaults true, set false via
	//       other_config.allow_image_generation = false in the admin agent configuration)
	if l.allowImageGeneration {
		if aware, ok := l.provider.(providers.CapabilitiesAware); ok {
			if aware.Capabilities().ImageGeneration {
				toolDefs = append(toolDefs, imageGenToolDef)
			}
		}
	}

	return toolDefs, allowedTools, messages
}
