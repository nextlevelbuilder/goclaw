package agent

import (
	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/memory"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// BridgePromptBuilder implements PromptBuilder by delegating to the existing
// BuildSystemPrompt() function. This provides the v3 PromptBuilder interface
// while reusing the battle-tested v2 prompt construction logic.
//
// When a full text/template engine is needed (provider variants, A/B testing),
// replace this with TemplatePromptBuilder — the interface stays the same.
type BridgePromptBuilder struct{}

// NewBridgePromptBuilder creates a PromptBuilder that wraps BuildSystemPrompt.
func NewBridgePromptBuilder() PromptBuilder {
	return &BridgePromptBuilder{}
}

// Build converts PromptConfig into a SystemPromptConfig and delegates to BuildSystemPrompt.
func (b *BridgePromptBuilder) Build(cfg PromptConfig) (string, error) {
	// Map PromptConfig toggles + data to existing SystemPromptConfig fields.
	spc := SystemPromptConfig{
		Mode: PromptFull,
	}

	if cfg.Identity {
		spc.AgentID = cfg.IdentityData.AgentName
		spc.Model = cfg.IdentityData.Model
		spc.Channel = cfg.IdentityData.Channel
		spc.ChatTitle = cfg.IdentityData.ChatTitle
		spc.PeerKind = cfg.IdentityData.PeerKind
	}

	if cfg.Persona {
		spc.ContextFiles = append(spc.ContextFiles, bootstrap.ContextFile{
			Path:    bootstrap.SoulFile,
			Content: cfg.PersonaContent,
		})
	}

	if cfg.Tools {
		names := make([]string, 0, len(cfg.ToolsData.ToolDefs))
		for _, td := range cfg.ToolsData.ToolDefs {
			names = append(names, td.Name)
		}
		spc.ToolNames = names
	}

	if cfg.Skills {
		if cfg.SkillsData.Mode == "search" {
			spc.HasSkillSearch = true
		}
		// Inline summaries handled by SkillsSummary string
	}

	if cfg.Team {
		spc.IsTeamContext = true
		spc.TeamWorkspace = cfg.TeamData.TeamWorkspace
		spc.TeamGuidance = cfg.TeamData.Guidance
		members := make([]store.TeamMemberData, len(cfg.TeamData.Members))
		for i, m := range cfg.TeamData.Members {
			members[i] = store.TeamMemberData{
				AgentKey:    m.AgentKey,
				DisplayName: m.DisplayName,
				Role:        m.Role,
				Frontmatter: m.Skills,
			}
		}
		spc.TeamMembers = members
	}

	if cfg.Workspace {
		spc.Workspace = cfg.WorkspaceData.ActivePath
	}

	if cfg.Sandbox {
		spc.SandboxEnabled = true
		spc.SandboxContainerDir = cfg.SandboxData.ContainerDir
	}

	if cfg.ExtraPrompt != "" {
		spc.ExtraPrompt = cfg.ExtraPrompt
	}

	spc.ProviderType = cfg.ProviderVariant

	// Build the prompt using existing logic.
	prompt := BuildSystemPrompt(spc)

	// Append L0 memory section if present (v3 auto-inject).
	if cfg.Memory && len(cfg.MemoryData.L0Summaries) > 0 {
		prompt += "\n\n" + formatMemorySection(cfg.MemoryData)
	}

	return prompt, nil
}

// formatMemorySection renders L0 memory summaries for prompt injection.
func formatMemorySection(data MemorySectionData) string {
	section := "## Memory Context\n\nRelevant memories from past sessions:\n"
	for _, s := range data.L0Summaries {
		section += "- " + s.Summary
		if s.ID != "" {
			section += " [use memory_search(\"" + s.ID + "\") for details]"
		}
		section += "\n"
	}
	if data.HasSearch {
		section += "\nUse `memory_search` for detailed retrieval."
	}
	if data.HasKG {
		section += " Use `knowledge_graph_search` for relationship queries."
	}
	return section
}

// formatVaultSection renders vault document summaries for prompt injection.
func formatVaultSection(docs []VaultDocSummary) string {
	if len(docs) == 0 {
		return ""
	}
	section := "## Knowledge Vault\n\nAvailable documents:\n"
	for _, d := range docs {
		section += "- " + d.Title + " (" + d.DocType + ")\n"
	}
	return section
}

// memoryL0ToStrings is a helper for tests.
func memoryL0ToStrings(summaries []memory.L0Summary) []string {
	out := make([]string, len(summaries))
	for i, s := range summaries {
		out[i] = s.Summary
	}
	return out
}
