package agent

import "github.com/nextlevelbuilder/goclaw/internal/memory"

// PromptConfig controls which template sections to include in the system prompt.
// Each bool field maps to a named template block.
type PromptConfig struct {
	// Section toggles
	Identity      bool
	Persona       bool // SOUL.md content
	Instructions  bool // AGENTS.md content
	Tools         bool
	Skills        bool
	Team          bool
	Workspace     bool
	Memory        bool // auto-inject L0 section
	Sandbox       bool
	Orchestration bool // v3 orchestration delegation targets

	// Data payloads (populated when section enabled)
	IdentityData       IdentityData
	PersonaContent     string
	InstructionContent string
	ToolsData          ToolsSectionData
	SkillsData         SkillsSectionData
	TeamData           TeamSectionData
	WorkspaceData      WorkspaceSectionData
	MemoryData         MemorySectionData
	SandboxData        SandboxSectionData
	OrchestrationData  OrchestrationSectionData
	ExtraPrompt        string

	// Prompt mode: full, task, minimal, none
	Mode PromptMode

	// Provider variant (selects template file)
	ProviderVariant string // "" = default, "codex", "dashscope"
}

// IdentityData populates the identity template section.
type IdentityData struct {
	AgentName string
	Emoji     string
	Model     string
	Channel   string
	ChatID    string // current reply target chat id
	ChatTitle string
	PeerKind  string // "direct" or "group"
}

// ToolsSectionData populates the tools template section.
type ToolsSectionData struct {
	ToolDefs    []ToolSummary
	MCPTools    []ToolSummary
	CoreSummary map[string]string
}

// ToolSummary is a single tool entry for prompt injection.
type ToolSummary struct {
	Name        string
	Description string
	Capability  string // "read-only", "mutating", "async", "mcp-bridged"
}

// SkillsSectionData populates the skills template section.
type SkillsSectionData struct {
	Mode      string // "inline" or "search"
	Summaries []SkillSummaryEntry
}

// SkillSummaryEntry is a single skill entry.
type SkillSummaryEntry struct {
	Slug        string
	Name        string
	Description string
	Score       float64
}

// TeamSectionData populates the team template section.
type TeamSectionData struct {
	TeamWorkspace string
	Members       []TeamMemberEntry
	Guidance      string
	TeamMDContent string
}

// TeamMemberEntry is a single team member.
type TeamMemberEntry struct {
	AgentKey    string
	DisplayName string
	Skills      string
	Role        string // "lead" or "member"
}

// WorkspaceSectionData populates the workspace template section.
type WorkspaceSectionData struct {
	ActivePath     string
	Scope          string
	Enforced       bool
	ReadOnlyPaths  []string
	SharedPath     *string
	ContextFiles   []string
	EnforcementMsg string
}

// MemorySectionData populates the memory template section.
type MemorySectionData struct {
	L0Summaries []memory.L0Summary
	HasSearch   bool
	HasKG       bool
	VaultDocs   []VaultDocSummary `json:"vault_docs,omitempty"`
	HasVault    bool              `json:"has_vault,omitempty"`
}

// VaultDocSummary is a brief vault document entry for prompt injection.
type VaultDocSummary struct {
	Title   string `json:"title"`
	Path    string `json:"path"`
	DocType string `json:"doc_type"`
}

// SandboxSectionData populates the sandbox template section.
type SandboxSectionData struct {
	ContainerDir string
	AccessLevel  string // "full" or "read-only"
}

// PromptBuilder renders system prompts from PromptConfig.
type PromptBuilder interface {
	Build(cfg PromptConfig) (string, error)
}
