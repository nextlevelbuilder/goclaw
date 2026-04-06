# Phase 4: System Integration Design

## Context Links
- Brainstorm: `plans/reports/brainstorm-260406-1059-goclaw-v3-core-redesign.md` (sections 9, 12, 14, 7)
- Architecture report: `plans/reports/Explore-260406-thorough-v3-architecture.md` (sections 5, 6)
- Workspace forcing: `plans/reports/Explore-260406-workspace-forcing.md`
- Current system prompt: `internal/agent/systemprompt.go`, `internal/agent/systemprompt_sections.go`
- Current tool registry: `internal/tools/registry.go`, `internal/tools/policy.go`, `internal/tools/types.go`
- Current skills: `internal/skills/loader.go`, `internal/skills/search.go`, `internal/store/skill_store.go`

## Overview
- **Priority:** P1 (core dependency for pipeline loop + memory system)
- **Status:** Complete
- **Effort:** 6h design

Redesigns 4 tightly-coupled systems that sit between the agent loop and the LLM:
1. Template-based system prompt (replace string concatenation)
2. Tool registry with typed capabilities + declarative policy
3. Skills system with embedding search + ACL + dependency graph
4. Retrieval integration connecting auto-inject L0 with template + tools

---

## Key Insights

1. Current `BuildSystemPrompt()` is ~200 lines of conditional string building. 15+ sections toggled by booleans. Adding workspace enforcement language or memory context requires touching fragile concatenation logic.
2. Tool types use 12+ optional interfaces (`InterceptorAware`, `PathAllowable`, etc.) with type assertions at registration. No capability metadata on tools themselves.
3. PolicyEngine's 7-step pipeline works but all checks are string-based group matching. No typed capabilities (read-only vs mutating), no workspace-awareness.
4. Skills BM25-only search misses intent-based queries ("help me debug" won't find "debugger" skill). `EmbeddingSkillSearcher` interface exists in store but not wired to loader's `BuildSummary()`.
5. Workspace confusion root cause: system prompt text says "use relative paths" but doesn't state the enforcement rule. Template must carry WorkspaceContext enforcement language verbatim.

---

## Requirements

### Functional
- F1: System prompt built from Go `text/template` with embedded template file
- F2: Each template section independently toggleable via PromptConfig
- F3: WorkspaceContext generates explicit enforcement language (not suggestive)
- F4: Tools declare capabilities (read-only, mutating, async, mcp-bridged) at registration
- F5: Policy engine evaluates capabilities (e.g., "deny all mutating tools for viewer role")
- F6: Tools receive WorkspaceContext, not raw path strings
- F7: Skills searched by both BM25 (lexical) and embedding (semantic), results merged
- F8: Skills filtered by agent/tenant ACL before injection
- F9: Skill dependency graph: loader resolves + validates before activation
- F10: Auto-inject L0 memory summaries into template's Memory section
- F11: Provider-specific template variants (e.g., Codex needs different tool format)

### Non-Functional
- NF1: Template rendering <1ms (no LLM calls during prompt build)
- NF2: Skill embedding search <50ms at 1000 skills
- NF3: Backward compatible: v2 PromptConfig can produce identical v2 output
- NF4: Template testable in isolation (no DB dependency for rendering)

---

## Architecture

### A. Template-Based System Prompt

#### PromptConfig Interface Contract

```go
// PromptConfig controls which template sections to include.
// Each field maps to a named template block.
type PromptConfig struct {
    // Section toggles
    Identity     bool
    Persona      bool // SOUL.md content
    Instructions bool // AGENTS.md content
    Tools        bool
    Skills       bool
    Team         bool
    Workspace    bool
    Memory       bool // auto-inject L0 section
    Sandbox      bool
    Recency      bool // recency reminder

    // Data payloads (populated when section enabled)
    IdentityData     IdentityData
    PersonaContent   string           // raw SOUL.md
    InstructionContent string         // raw AGENTS.md
    ToolsData        ToolsSectionData
    SkillsData       SkillsSectionData
    TeamData         TeamSectionData
    WorkspaceData    WorkspaceSectionData
    MemoryData       MemorySectionData
    SandboxData      SandboxSectionData
    ExtraPrompt      string

    // Provider variant (selects template file)
    ProviderVariant  string // "" = default, "codex", "dashscope"
}
```

#### Section Data Types

```go
type IdentityData struct {
    AgentName    string
    Emoji        string
    Model        string
    Channel      string
    ChatTitle    string
    PeerKind     string // "direct" or "group"
}

type ToolsSectionData struct {
    ToolDefs     []ToolSummary       // name + one-line description
    MCPTools     []ToolSummary       // MCP bridge tools (separate listing)
    CoreSummary  map[string]string   // builtin tool summaries
}

type ToolSummary struct {
    Name        string
    Description string
    Capability  ToolCapability // read-only, mutating, async, mcp-bridged
}

type SkillsSectionData struct {
    Mode        string // "inline" (inject summaries) or "search" (search tool only)
    Summaries   []SkillSummaryEntry
}

type SkillSummaryEntry struct {
    Slug        string
    Name        string
    Description string
    Score       float64 // relevance score from search
}

type TeamSectionData struct {
    TeamWorkspace string
    Members       []TeamMemberEntry
    Guidance      string // edition-specific (Full vs Lite)
    TeamMDContent string // TEAM.md content
}

type TeamMemberEntry struct {
    AgentKey    string
    DisplayName string
    Skills      string
    Role        string // "lead" or "member"
}

type WorkspaceSectionData struct {
    ActivePath     string
    Scope          string // "personal" | "team-shared" | "team-isolated" | "delegate"
    Enforced       bool   // always true in v3
    ReadOnlyPaths  []string
    SharedPath     *string
    ContextFiles   []string // list of managed context files
    EnforcementMsg string   // pre-rendered enforcement text
}

type MemorySectionData struct {
    L0Summaries []L0Summary // auto-injected memory hints
    HasSearch   bool        // memory_search tool available
    HasKG       bool        // kg_search tool available
}

type L0Summary struct {
    Topic   string
    Summary string // 1-sentence abstract
    ID      string // for memory_expand(id) deep retrieval
}

type SandboxSectionData struct {
    ContainerDir string
    AccessLevel  string // "full" or "read-only"
}
```

#### Template Structure

Embedded template file at `internal/agent/prompt_template.go.tmpl`:

```
{{- /* System Prompt Template — v3 */ -}}

{{- if .Identity -}}
{{- template "identity" .IdentityData -}}
{{- end -}}

{{- if .Persona -}}
# Persona
{{ .PersonaContent }}
{{- end -}}

{{- if .Instructions -}}
# Instructions
{{ .InstructionContent }}
{{- end -}}

{{- if .Workspace -}}
{{- template "workspace" .WorkspaceData -}}
{{- end -}}

{{- if .Tools -}}
{{- template "tools" .ToolsData -}}
{{- end -}}

{{- if .Skills -}}
{{- template "skills" .SkillsData -}}
{{- end -}}

{{- if .Memory -}}
{{- template "memory" .MemoryData -}}
{{- end -}}

{{- if .Sandbox -}}
{{- template "sandbox" .SandboxData -}}
{{- end -}}

{{- if .Team -}}
{{- template "team" .TeamData -}}
{{- end -}}

{{- if .ExtraPrompt -}}
# Additional Context
{{ .ExtraPrompt }}
{{- end -}}
```

#### Workspace Sub-Template (Critical)

```
{{- define "workspace" -}}
# Workspace
- **Path:** {{ .ActivePath }}
- **Scope:** {{ .Scope }}
- **ENFORCED:** All file tool paths resolve relative to this directory. Absolute paths outside workspace are REJECTED.
{{- if .ReadOnlyPaths }}
- **Read-only access:** {{ range .ReadOnlyPaths }}{{ . }}, {{ end }}
{{- end }}
{{- if .SharedPath }}
- **Shared area:** {{ .SharedPath }} (read/write by both you and delegator)
{{- end }}
{{- if .ContextFiles }}
- **Managed files:** {{ range .ContextFiles }}{{ . }}, {{ end }}(system-managed, read/write intercepted to DB)
{{- end }}
{{- end -}}
```

#### Memory Sub-Template

```
{{- define "memory" -}}
# Memory Context
{{- if .L0Summaries }}
{{- range .L0Summaries }}
- **{{ .Topic }}:** {{ .Summary }} [use memory_search("{{ .ID }}") for details]
{{- end }}
{{- end }}
{{- if .HasSearch }}
Use `memory_search` for detailed retrieval. Use `kg_search` for relationship queries.
{{- end }}
{{- end -}}
```

#### PromptBuilder Interface

```go
// PromptBuilder renders system prompts from PromptConfig.
type PromptBuilder interface {
    Build(cfg PromptConfig) (string, error)
}

// TemplatePromptBuilder implements PromptBuilder using text/template.
type TemplatePromptBuilder struct {
    templates map[string]*template.Template // variant -> parsed template
}

// NewTemplatePromptBuilder loads embedded templates.
func NewTemplatePromptBuilder() (*TemplatePromptBuilder, error)

// Build renders prompt. Selects template variant from cfg.ProviderVariant.
// Returns rendered string. Error only on template parse failure (should not happen with embedded).
func (b *TemplatePromptBuilder) Build(cfg PromptConfig) (string, error)
```

#### Provider Variants

- **Default template:** Standard Anthropic/OpenAI format
- **Codex variant:** Tool descriptions formatted as docstrings (Codex prefers this)
- **DashScope variant:** No streaming+tools section (DashScope limitation)
- Variant selected by `cfg.ProviderVariant` which maps to provider capabilities

---

### B. Tool Registry Redesign

#### Tool Capability Types

```go
// ToolCapability describes what a tool can do.
type ToolCapability string

const (
    CapReadOnly   ToolCapability = "read-only"   // no side effects (read_file, list_files, memory_search)
    CapMutating   ToolCapability = "mutating"     // modifies state (write_file, exec, edit)
    CapAsync      ToolCapability = "async"        // returns immediately, result via callback
    CapMCPBridged ToolCapability = "mcp-bridged"  // proxied to external MCP server
)

// ToolMetadata describes a tool's capabilities and requirements.
type ToolMetadata struct {
    Name         string
    Capabilities []ToolCapability
    Group        string // "fs", "web", "runtime", "memory", "team", etc.
    RequiresWorkspace bool // tool needs WorkspaceContext
    ProviderHints map[string]any // provider-specific hints (e.g., cache_control)
}
```

#### Enhanced Tool Interface

```go
// Tool is the base interface all tools must implement.
// V3 adds Metadata() for capability declaration.
type Tool interface {
    Name() string
    Description() string
    Parameters() map[string]any
    Execute(ctx context.Context, args map[string]any) *Result
    Metadata() ToolMetadata // NEW: declarative capability info
}
```

> **Migration note:** Existing tools that don't implement `Metadata()` get a wrapper
> that returns default metadata (capabilities inferred from tool group membership).

#### WorkspaceContext in Tool Context

```go
// Tools access WorkspaceContext via context, not struct fields.
// Replaces: ToolWorkspaceFromCtx(), ToolTeamWorkspaceFromCtx()

func WorkspaceContextFromCtx(ctx context.Context) *WorkspaceContext
func WithWorkspaceContext(ctx context.Context, wc *WorkspaceContext) context.Context
```

All file tools use `WorkspaceContextFromCtx(ctx)` instead of separate workspace/team-workspace context keys. WorkspaceContext carries ActivePath, ReadOnlyPaths, SharedPath — one struct, one lookup.

#### Enhanced PolicyEngine

```go
// PolicyRule is a declarative access rule.
type PolicyRule struct {
    // Match conditions (all must match)
    Capabilities []ToolCapability // match tools with these capabilities
    Groups       []string         // match tools in these groups
    Names        []string         // match specific tool names

    // Action
    Action       PolicyAction // allow or deny
    Reason       string       // audit log reason
}

type PolicyAction string
const (
    PolicyAllow PolicyAction = "allow"
    PolicyDeny  PolicyAction = "deny"
)

// PolicyEngine evaluates tool access via ordered rule list.
// V3 adds capability-based rules alongside existing group/name rules.
type PolicyEngine struct {
    globalPolicy  *config.ToolsConfig
    rules         []PolicyRule // declarative rules (evaluated in order, first match wins)
}

// FilterTools returns allowed tools. Now evaluates both legacy 7-step pipeline
// AND declarative rules. Declarative rules take precedence.
func (pe *PolicyEngine) FilterTools(
    registry ToolExecutor,
    agentID string,
    providerName string,
    agentToolPolicy *config.ToolPolicySpec,
    groupToolAllow []string,
    isSubagent bool,
    isLeafAgent bool,
    role string, // NEW: RBAC role ("admin", "operator", "viewer")
) []providers.ToolDefinition
```

#### Tool Definition Generation for Providers

```go
// ToolDefAdapter generates provider-specific tool definitions.
// Replaces direct ToProviderDef() calls.
type ToolDefAdapter interface {
    // ToProviderDefs converts internal tool list to provider wire format.
    // Handles provider-specific quirks (DashScope tool format, Codex docstrings, etc.)
    ToProviderDefs(tools []Tool, caps ProviderCapabilities) []providers.ToolDefinition
}
```

#### Pipeline Integration

ToolStage in pipeline receives registry + policy engine:
```go
type ToolStage struct {
    registry     *Registry
    policyEngine *PolicyEngine
    adapter      ToolDefAdapter
}

// Execute runs tool, records metrics, returns result for ObserveStage.
func (s *ToolStage) Execute(ctx context.Context, state *RunState) error
```

ObserveStage processes tool results:
```go
type ObserveStage struct {
    loopDetector  *LoopDetector
    metricsStore  EvolutionMetricsStore // record tool effectiveness
}

// Execute checks for tool loops, records metrics, decides continue/stop.
func (s *ObserveStage) Execute(ctx context.Context, state *RunState) error
```

---

### C. Skills System Upgrade

#### Hybrid Search Interface

```go
// SkillSearcher provides both lexical and semantic skill search.
type SkillSearcher interface {
    // Search returns skills matching query, merging BM25 + embedding results.
    // Returns at most limit results, deduplicated by slug.
    Search(ctx context.Context, query string, limit int) ([]SkillSearchResult, error)

    // SearchForAgent filters by ACL before search.
    SearchForAgent(ctx context.Context, agentID uuid.UUID, userID string, query string, limit int) ([]SkillSearchResult, error)
}

// HybridSkillSearcher implements SkillSearcher with BM25 + embedding fusion.
type HybridSkillSearcher struct {
    bm25Index     *skills.Index          // existing BM25 index
    embSearcher   store.EmbeddingSkillSearcher // existing PG embedding search
    accessStore   store.SkillAccessStore       // ACL filtering
    bm25Weight    float64 // default 0.4
    embedWeight   float64 // default 0.6
}
```

#### Score Fusion Strategy

```
1. BM25 search → top 2*limit results, normalize scores to [0,1]
2. Embedding search → top 2*limit results, normalize scores to [0,1]
3. Merge by slug: combined_score = bm25Weight * bm25_score + embedWeight * embed_score
4. Sort by combined_score descending
5. Return top limit
```

#### ACL Model

```go
// SkillVisibility controls who can see/use a skill.
type SkillVisibility string

const (
    SkillVisibilityPublic  SkillVisibility = "public"  // all agents in tenant
    SkillVisibilityPrivate SkillVisibility = "private" // only granted agents
    SkillVisibilitySystem  SkillVisibility = "system"  // system-managed, immutable
)

// ACL check flow:
// 1. If skill.Visibility == "public" → allow
// 2. If skill.Visibility == "private" → check skill_agent_grants table
// 3. If skill.Visibility == "system" → allow (system skills always accessible)
// Existing SkillAccessStore.ListAccessible() already implements this.
```

No new DB tables needed. Existing `managed_skills.visibility` + `skill_agent_grants` + `SkillAccessStore` interface covers ACL. The change is wiring ACL into the search path (currently BM25 search ignores ACL).

#### Dependency Graph

```go
// SkillDependency represents skill A requires skill B.
// Stored in skill frontmatter: `depends: [slug-b, slug-c]`
type SkillDependency struct {
    Slug     string   // this skill
    Requires []string // slugs of required skills
}

// DepChecker validates skill dependencies are satisfied.
// Existing internal/skills/dep_checker.go already handles this.
// V3 enhancement: block activation of skill with unresolved deps.
type DepChecker interface {
    // Check returns missing dependencies for a skill.
    Check(slug string, available []string) []string

    // ResolveOrder returns activation order respecting dependency graph.
    // Error if circular dependency detected.
    ResolveOrder(slugs []string) ([]string, error)
}
```

No new DB schema. Dependencies stored in SKILL.md frontmatter `depends:` field. `dep_checker.go` already parses this. V3 adds `ResolveOrder()` for topological sort + circular detection.

#### Hot-Reload Enhancement

```go
// SkillLoader manages skill discovery, loading, and hot-reload.
// V3 adds ACL-aware loading + hybrid search integration.
type SkillLoader interface {
    // LoadAll discovers and loads all skills from all tiers.
    LoadAll(ctx context.Context) error

    // BuildSummary returns skill summary for system prompt injection.
    // V3: uses HybridSkillSearcher instead of BM25-only.
    BuildSummary(ctx context.Context, agentID uuid.UUID, userID string, query string) string

    // Version returns current reload version (atomic counter).
    Version() int64

    // OnReload registers callback for hot-reload events.
    OnReload(fn func())
}
```

---

### D. Retrieval Integration

#### Auto-Inject L0 Flow

```
Per turn (in ContextStage):
1. Extract keywords from latest user message (simple tokenization, same as BM25)
2. Check relevance against memory index:
   a. BM25 score against episodic summaries
   b. Embedding similarity against semantic memory (KG entities)
3. If max_score > agent.retrieval_threshold (default 0.3):
   → Build L0Summaries for PromptConfig.MemoryData
   → Cap at max_l0_tokens (default 200 tokens, counted via TokenCounter)
4. PromptBuilder renders Memory section with L0 hints
5. Agent sees hints, can call memory_search(id) for L1/L2 detail
```

#### Retrieval Config

```go
// RetrievalConfig controls auto-inject behavior. Per-agent configurable.
type RetrievalConfig struct {
    Enabled           bool    `json:"enabled"`             // default true
    RelevanceThreshold float64 `json:"relevance_threshold"` // default 0.3
    MaxL0Tokens       int     `json:"max_l0_tokens"`       // default 200
    MaxL0Items        int     `json:"max_l0_items"`        // default 5
    BM25Weight        float64 `json:"bm25_weight"`         // default 0.4
    EmbeddingWeight   float64 `json:"embedding_weight"`    // default 0.6
}
```

Stored in agent settings JSON (`agents.settings` column, existing JSONB field). No new DB columns.

#### Retrieval Interface

```go
// Retriever performs auto-inject L0 retrieval for ContextStage.
type Retriever interface {
    // RetrieveL0 returns L0 summaries relevant to the query.
    // Called once per turn in ContextStage.
    RetrieveL0(ctx context.Context, agentID uuid.UUID, userID string, query string, cfg RetrievalConfig) ([]L0Summary, error)
}

// HybridRetriever implements Retriever using memory + KG stores.
type HybridRetriever struct {
    memoryStore  store.MemoryStore
    kgStore      store.KnowledgeGraphStore
    tokenCounter TokenCounter
    embProvider  store.EmbeddingProvider
}
```

#### memory_search / kg_search Tool Updates

```go
// memory_search tool gains L1/L2 mode parameter.
// Args: { "query": "...", "depth": "l1"|"l2" }
// L1: returns structured overview (episodic summaries, ~200 tokens each)
// L2: returns full detail (original content, KG traversal)
// Default: "l1" (backward compatible — current behavior is L1-equivalent)

// kg_search tool gains temporal parameter.
// Args: { "query": "...", "as_of": "2026-01-01" }
// as_of: returns facts valid at that date. Omit = current facts only.
```

---

## Related Code Files

### Files to Modify
| File | Change |
|------|--------|
| `internal/agent/systemprompt.go` | Replace BuildSystemPrompt() with PromptBuilder.Build() |
| `internal/agent/systemprompt_sections.go` | Extract section data builders, remove string concat |
| `internal/agent/loop_history.go` | Use PromptBuilder in buildMessages() |
| `internal/tools/types.go` | Add Metadata() to Tool interface |
| `internal/tools/registry.go` | Store ToolMetadata, add capability queries |
| `internal/tools/policy.go` | Add capability-based rules, role parameter |
| `internal/tools/context_keys.go` | Replace dual workspace keys with WorkspaceContext |
| `internal/tools/filesystem.go` | Use WorkspaceContextFromCtx() |
| `internal/tools/filesystem_write.go` | Use WorkspaceContextFromCtx() |
| `internal/tools/filesystem_list.go` | Use WorkspaceContextFromCtx() |
| `internal/tools/shell.go` | Use WorkspaceContextFromCtx() |
| `internal/skills/loader.go` | Wire HybridSkillSearcher into BuildSummary() |
| `internal/skills/search.go` | Keep BM25, add fusion logic |
| `internal/tools/memory_search.go` | Add depth parameter (l1/l2) |
| `internal/tools/knowledge_graph_search.go` | Add as_of temporal parameter |

### Files to Create
| File | Purpose |
|------|---------|
| `internal/agent/prompt_builder.go` | TemplatePromptBuilder implementation |
| `internal/agent/prompt_template.go.tmpl` | Embedded template file (default variant) |
| `internal/agent/prompt_template_codex.go.tmpl` | Codex variant |
| `internal/agent/prompt_config_types.go` | PromptConfig, section data types |
| `internal/tools/capability.go` | ToolCapability, ToolMetadata types |
| `internal/tools/workspace_context.go` | WorkspaceContext struct + context helpers |
| `internal/tools/tool_def_adapter.go` | ToolDefAdapter interface + default impl |
| `internal/skills/hybrid_searcher.go` | HybridSkillSearcher |
| `internal/agent/retriever.go` | Retriever interface + HybridRetriever |

---

## Design Steps

1. Define PromptConfig + all section data types in `prompt_config_types.go`
2. Write default template file with all section sub-templates
3. Implement TemplatePromptBuilder with variant loading
4. Define ToolCapability + ToolMetadata types
5. Add Metadata() to Tool interface with backward-compat wrapper
6. Define WorkspaceContext struct with context helpers
7. Update PolicyEngine: add capability rules + role parameter
8. Define ToolDefAdapter for provider-specific tool def generation
9. Implement HybridSkillSearcher with score fusion
10. Wire ACL into skill search path
11. Define Retriever interface + RetrievalConfig
12. Implement HybridRetriever for L0 auto-inject
13. Update memory_search tool with depth parameter design
14. Update kg_search tool with as_of temporal parameter design
15. Write provider variant templates (Codex, DashScope)

---

## Todo List

- [x] A1: PromptConfig + section data types — `internal/agent/prompt_config_types.go`
- [x] A2: Default template file — documented in phase design
- [x] A3: TemplatePromptBuilder interface — `internal/agent/prompt_config_types.go`
- [x] A4: Provider variant templates — documented in phase design
- [x] A5: Workspace sub-template — documented in phase design
- [x] B1: ToolCapability + ToolMetadata — `internal/tools/capability.go`
- [x] B2: Backward-compat wrapper — documented in phase design
- [x] B3: WorkspaceContext + context key — done in Phase 1 (`internal/workspace/`)
- [x] B4: PolicyEngine capability rules — documented in phase design
- [x] B5: ToolDefAdapter interface — documented in phase design
- [x] C1: HybridSkillSearcher — documented in phase design
- [x] C2: ACL wiring into search path — documented in phase design
- [x] C3: DepChecker.ResolveOrder() — documented in phase design
- [x] D1: Retriever + RetrievalConfig — `internal/agent/retriever.go`
- [x] D2: L0 auto-inject flow — documented in phase design
- [x] D3: memory_search depth param — documented in phase design
- [x] D4: kg_search as_of temporal param — documented in phase design

---

## Success Criteria

1. PromptConfig can reproduce exact v2 system prompt output (regression test)
2. All template sections independently toggleable + testable
3. WorkspaceContext carries all info needed by file tools (no separate context keys)
4. Every tool declares capabilities via Metadata()
5. PolicyEngine can express "deny mutating tools for viewer role" in one rule
6. HybridSkillSearcher returns better results than BM25-only on intent queries
7. L0 auto-inject adds <200 tokens per turn, only when relevant
8. Template renders in <1ms (no LLM calls)

---

## Risk Assessment

| Risk | Severity | Mitigation |
|------|----------|------------|
| Template engine too rigid for edge cases | MEDIUM | Keep ExtraPrompt escape hatch. Provider variants handle outliers. |
| Metadata() breaks all existing tool implementations | HIGH | Backward-compat wrapper: `defaultMetadata(tool)` infers from group/name. Phase implementation. |
| Hybrid skill search slower than BM25-only | LOW | BM25 <1ms, embedding <50ms. Parallel execution. Cache embeddings. |
| WorkspaceContext migration touches every file tool | HIGH | Single context key replaces 2. Grep for `ToolWorkspaceFromCtx` — 15 call sites. Mechanical refactor. |
| L0 auto-inject adds latency to every turn | MEDIUM | Memory index query <10ms. Skip if no memory (hasMemory flag). Threshold prevents injection for low-relevance messages. |
| Provider template variants diverge over time | LOW | Variants inherit from default, override only specific blocks. Test all variants in CI. |

---

## Security Considerations

- **Template injection:** PromptConfig data (PersonaContent, InstructionContent) comes from DB. Template uses `{{ }}` not `{{ | html }}`. Since output goes to LLM (not browser), HTML escaping not needed. But ensure no Go template directives in user content — use `{{ printf "%s" .Content }}` for raw pass-through.
- **Workspace enforcement:** WorkspaceSectionData.EnforcementMsg must be generated server-side, not from user input. Template renders it verbatim.
- **ACL bypass:** HybridSkillSearcher MUST filter AFTER search, not before (embedding search doesn't support WHERE clauses). Verify: `ListAccessible()` called on search results.
- **Tool capability lying:** Tools self-declare capabilities. PolicyEngine should cross-check: tools in "fs" group claiming "read-only" but actually writing. Add assertion in tests.
