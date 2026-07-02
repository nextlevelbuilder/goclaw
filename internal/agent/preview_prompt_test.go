package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

func baseAgent() *store.AgentData {
	return &store.AgentData{
		BaseModel: store.BaseModel{ID: uuid.New()},
		AgentKey:  "test-agent",
		AgentType: store.AgentTypePredefined,
		Workspace: "/workspace",
	}
}

func TestBuildPreviewPrompt_NilDeps(t *testing.T) {
	r := BuildPreviewPrompt(context.Background(), baseAgent(), PromptFull, "", PreviewDeps{})
	if r.Prompt == "" {
		t.Fatal("expected non-empty prompt with nil deps")
	}
	if !strings.Contains(r.Prompt, "read_file") {
		t.Error("expected fallback tool names in prompt")
	}
	// No tool lister → no tool defs
	if len(r.ToolDefs) != 0 {
		t.Errorf("expected no tool defs with nil ToolLister, got %d", len(r.ToolDefs))
	}
}

func TestBuildPreviewPrompt_SkillsInline(t *testing.T) {
	r := BuildPreviewPrompt(context.Background(), baseAgent(), PromptFull, "", PreviewDeps{
		SkillsLoader: &mockSkillsLoader{
			summary: "<available_skills>\n<skill name=\"git\">Git operations</skill>\n</available_skills>",
		},
	})
	if !strings.Contains(r.Prompt, "<available_skills>") {
		t.Error("expected skills XML inlined in prompt")
	}
}

func TestBuildPreviewPrompt_SkillsSearchMode(t *testing.T) {
	bigSummary := strings.Repeat("x", 10000)
	r := BuildPreviewPrompt(context.Background(), baseAgent(), PromptFull, "", PreviewDeps{
		SkillsLoader: &mockSkillsLoader{summary: bigSummary},
	})
	if strings.Contains(r.Prompt, bigSummary) {
		t.Error("expected large summary to be excluded (search-only mode)")
	}
}

func TestBuildPreviewPrompt_PinnedSkillsHybrid(t *testing.T) {
	ag := baseAgent()
	ag.OtherConfig = []byte(`{"pinned_skills":["deploy"]}`)
	r := BuildPreviewPrompt(context.Background(), ag, PromptFull, "", PreviewDeps{
		SkillsLoader: &mockSkillsLoader{
			pinned:  "<skill name=\"deploy\">Deploy to prod</skill>",
			summary: "<available_skills>\n<skill name=\"git\">Git ops</skill>\n</available_skills>",
		},
	})
	if !strings.Contains(r.Prompt, "deploy") || !strings.Contains(r.Prompt, "Pinned skills") {
		t.Error("expected pinned skills section in prompt")
	}
}

func TestBuildPreviewPrompt_SkillAllowList(t *testing.T) {
	ag := baseAgent()
	loader := &mockSkillsLoader{
		summary: "<available_skills><skill name=\"allowed\">ok</skill></available_skills>",
	}
	r := BuildPreviewPrompt(context.Background(), ag, PromptFull, "user1", PreviewDeps{
		SkillsLoader: loader,
		SkillAccessStore: &mockSkillAccessStore{
			accessible: []store.SkillInfo{{Slug: "allowed-skill"}},
		},
	})
	if !strings.Contains(r.Prompt, "<available_skills>") {
		t.Error("expected filtered skills in prompt")
	}
	if len(loader.capturedAllow) != 1 || loader.capturedAllow[0] != "allowed-skill" {
		t.Errorf("expected allow list [allowed-skill], got %v", loader.capturedAllow)
	}
}

func TestBuildPreviewPrompt_SkillAccessStoreError(t *testing.T) {
	ag := baseAgent()
	loader := &mockSkillsLoader{
		summary: "<available_skills><skill name=\"s\">desc</skill></available_skills>",
	}
	r := BuildPreviewPrompt(context.Background(), ag, PromptFull, "user1", PreviewDeps{
		SkillsLoader:     loader,
		SkillAccessStore: &mockSkillAccessStore{err: errors.New("db error")},
	})
	if r.Prompt == "" {
		t.Fatal("expected non-empty prompt on SkillAccessStore error")
	}
	if loader.capturedAllow == nil || len(loader.capturedAllow) != 0 {
		t.Errorf("expected empty (non-nil) allow list on error, got %v", loader.capturedAllow)
	}
}

// TestBuildPreviewPrompt_MCPToolDescs proves that when MCP tools are present,
// the prompt text carries the behavioral note (prefer-MCP-over-core-tools,
// optional-parameter guidance) but no longer enumerates per-tool name+description
// lines — that's now pure duplication with the real schema in ToolDefs/`tools:`.
func TestBuildPreviewPrompt_MCPToolDescs(t *testing.T) {
	r := BuildPreviewPrompt(context.Background(), baseAgent(), PromptFull, "", PreviewDeps{
		ToolLister: &mockToolLister{
			tools: map[string]string{
				"read_file":    "Read a file",
				"mcp_pg_query": "Run PostgreSQL queries",
			},
		},
	})
	if !strings.Contains(r.Prompt, "## MCP Tools (prefer over core tools)") {
		t.Error("expected MCP Tools behavioral section header in prompt")
	}
	if !strings.Contains(r.Prompt, "always prefer the MCP tool") {
		t.Error("expected prefer-MCP-over-core-tools instruction in prompt")
	}
	if strings.Contains(r.Prompt, "mcp_pg_query:") {
		t.Error("expected per-tool MCP description enumeration to be removed from prompt text (now duplicated by ToolDefs schema)")
	}
}

func TestBuildPreviewPrompt_MCPToolSearchExcluded(t *testing.T) {
	r := BuildPreviewPrompt(context.Background(), baseAgent(), PromptFull, "", PreviewDeps{
		ToolLister: &mockToolLister{
			tools: map[string]string{
				"read_file":       "Read a file",
				"mcp_tool_search": "Search MCP tools",
			},
		},
	})
	if strings.Contains(r.Prompt, "Search MCP tools") {
		t.Error("mcp_tool_search should not appear in MCP tool descriptions")
	}
}

func TestBuildPreviewPrompt_AliasExclusion(t *testing.T) {
	r := BuildPreviewPrompt(context.Background(), baseAgent(), PromptFull, "", PreviewDeps{
		ToolLister: &mockToolLister{
			tools: map[string]string{
				"read_file": "Read a file",
				"Read":      "Alias for read_file",
				"exec":      "Execute shell",
				"Bash":      "Alias for exec",
			},
			aliases: map[string]string{
				"Read": "read_file",
				"Bash": "exec",
			},
		},
	})
	if strings.Contains(r.Prompt, "- Read\n") || strings.Contains(r.Prompt, "- Bash\n") {
		t.Error("aliases should be excluded from tool list")
	}
}

func TestBuildPreviewPrompt_SkillManageGating(t *testing.T) {
	ag := baseAgent()
	r := BuildPreviewPrompt(context.Background(), ag, PromptFull, "", PreviewDeps{
		ToolLister: &mockToolLister{
			tools: map[string]string{
				"read_file":    "Read a file",
				"skill_manage": "Manage skills",
			},
		},
	})
	if strings.Contains(r.Prompt, "skill_manage") {
		t.Error("skill_manage should be excluded when skill_evolve is off")
	}
}

func TestBuildPreviewPrompt_SkillManageEnabled(t *testing.T) {
	ag := baseAgent()
	ag.SkillEvolve = true
	r := BuildPreviewPrompt(context.Background(), ag, PromptFull, "", PreviewDeps{
		ToolLister: &mockToolLister{
			tools: map[string]string{
				"read_file":    "Read a file",
				"skill_manage": "Manage skills",
				"skill_search": "Search skills",
			},
		},
	})
	if !strings.Contains(r.Prompt, "skill_manage") {
		t.Error("skill_manage should be present when skill_evolve is on")
	}
}

func TestBuildPreviewPrompt_ToolPolicyDeny(t *testing.T) {
	ag := baseAgent()
	ag.ToolsConfig = []byte(`{"deny":["exec","web_fetch"]}`)
	r := BuildPreviewPrompt(context.Background(), ag, PromptFull, "", PreviewDeps{
		ToolLister: &mockToolLister{
			tools: map[string]string{
				"read_file": "Read a file",
				"exec":      "Execute shell",
				"web_fetch": "Fetch web page",
			},
		},
	})
	if strings.Contains(r.Prompt, "- exec\n") {
		t.Error("denied tool 'exec' should be excluded")
	}
	if strings.Contains(r.Prompt, "- web_fetch\n") {
		t.Error("denied tool 'web_fetch' should be excluded")
	}
	if !strings.Contains(r.Prompt, "read_file") {
		t.Error("non-denied tool 'read_file' should be present")
	}
}

func TestBuildPreviewPrompt_ToolDefs(t *testing.T) {
	r := BuildPreviewPrompt(context.Background(), baseAgent(), PromptFull, "", PreviewDeps{
		ToolLister: &mockToolLister{
			tools: map[string]string{
				"read_file": "Read a file",
				"exec":      "Execute shell",
			},
			aliases: map[string]string{
				"Read": "read_file",
			},
		},
	})
	// Should have canonical tools + aliases in tool defs
	if len(r.ToolDefs) != 3 { // read_file + exec + Read alias
		t.Errorf("expected 3 tool defs (2 canonical + 1 alias), got %d", len(r.ToolDefs))
	}
	// Verify alias is included in defs even though excluded from system prompt
	found := false
	for _, td := range r.ToolDefs {
		if td.Function.Name == "Read" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected alias 'Read' in tool defs")
	}
}

// TestBuildPreviewPrompt_DeniedToolAliasExcludedFromDefs proves that when a
// canonical tool is denied via tools_config, its alias must NOT reappear in
// ToolDefs (regression test for the alias-reinjection bug: aliases were
// previously appended unconditionally regardless of the deny filter).
func TestBuildPreviewPrompt_DeniedToolAliasExcludedFromDefs(t *testing.T) {
	ag := baseAgent()
	ag.ToolsConfig = []byte(`{"deny":["exec"]}`)
	r := BuildPreviewPrompt(context.Background(), ag, PromptFull, "", PreviewDeps{
		ToolLister: &mockToolLister{
			tools: map[string]string{
				"read_file": "Read a file",
				"exec":      "Execute shell",
			},
			aliases: map[string]string{
				"Bash": "exec",
			},
		},
	})
	for _, td := range r.ToolDefs {
		if td.Function.Name == "Bash" || td.Function.Name == "exec" {
			t.Errorf("denied tool %q (or its alias) should not appear in ToolDefs, got defs: %+v", td.Function.Name, r.ToolDefs)
		}
	}
	found := false
	for _, td := range r.ToolDefs {
		if td.Function.Name == "read_file" {
			found = true
		}
	}
	if !found {
		t.Error("expected non-denied tool 'read_file' in ToolDefs")
	}
}

// TestBuildPreviewPrompt_MCPToolInDefs proves that store-based MCP tool
// descriptions (from MCPLister, no live registry entry) are converted into
// ToolDefinition entries with at least name+description populated, and that
// when the MCPLister entry has NO cached parameter schema, the genuine
// unknown-schema placeholder is used as a fallback.
func TestBuildPreviewPrompt_MCPToolInDefs(t *testing.T) {
	ag := baseAgent()
	r := BuildPreviewPrompt(context.Background(), ag, PromptFull, "u1", PreviewDeps{
		ToolLister: &mockToolLister{
			tools: map[string]string{
				"read_file": "Read a file",
			},
		},
		MCPLister: &mockMCPLister{
			tools: []MCPToolPreviewInfo{
				{RegisteredName: "mcp_pg_query", Description: "Run PostgreSQL queries"},
			},
		},
	})
	var found *providers.ToolDefinition
	for i := range r.ToolDefs {
		if r.ToolDefs[i].Function.Name == "mcp_pg_query" {
			found = &r.ToolDefs[i]
		}
	}
	if found == nil {
		t.Fatalf("expected mcp_pg_query in ToolDefs, got: %+v", r.ToolDefs)
	}
	if found.Function.Description != "Run PostgreSQL queries" {
		t.Errorf("expected description to be populated, got %q", found.Function.Description)
	}
	if found.Function.Parameters == nil {
		t.Error("expected placeholder parameters schema, got nil")
	}
}

// TestBuildPreviewPrompt_MCPToolRealSchemaInDefs proves that when the
// MCPLister provides a real cached parameter schema (captured from a live
// MCP connection, see buildCachedToolInfo in internal/mcp/manager_connect.go),
// BuildPreviewPrompt's ToolDefs uses that REAL schema instead of the honest
// "unknown schema" placeholder — closing the gap between preview and the
// live conversation path (which always sends real schemas via
// BridgeTool.Parameters()).
func TestBuildPreviewPrompt_MCPToolRealSchemaInDefs(t *testing.T) {
	ag := baseAgent()
	realSchema := `{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`
	r := BuildPreviewPrompt(context.Background(), ag, PromptFull, "u1", PreviewDeps{
		MCPLister: &mockMCPLister{
			tools: []MCPToolPreviewInfo{
				{
					RegisteredName: "mcp_pg_query",
					Description:    "Run PostgreSQL queries",
					Parameters:     json.RawMessage(realSchema),
				},
			},
		},
	})
	var found *providers.ToolDefinition
	for i := range r.ToolDefs {
		if r.ToolDefs[i].Function.Name == "mcp_pg_query" {
			found = &r.ToolDefs[i]
		}
	}
	if found == nil {
		t.Fatalf("expected mcp_pg_query in ToolDefs, got: %+v", r.ToolDefs)
	}
	params := found.Function.Parameters
	props, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected real schema properties, got %+v", params)
	}
	if _, ok := props["query"]; !ok {
		t.Errorf("expected real schema property 'query', got %+v", props)
	}
}

// TestBuildPreviewPrompt_GlobalDenyApplied proves that a globally-denied tool
// (configured via the web UI's global Tools policy, with NO per-agent deny
// config) is excluded from preview's tool list. This mirrors the bug where
// preview hand-rolled a deny-only reimplementation that only consulted
// per-agent deny and never the PolicyEngine's global deny.
func TestBuildPreviewPrompt_GlobalDenyApplied(t *testing.T) {
	ag := baseAgent() // tools_config is nil — no per-agent deny
	pe := tools.NewPolicyEngine(&config.ToolsConfig{
		Deny: []string{"cron", "delegate"},
	})
	r := BuildPreviewPrompt(context.Background(), ag, PromptFull, "", PreviewDeps{
		ToolLister: &mockToolLister{
			tools: map[string]string{
				"read_file": "Read a file",
				"cron":      "Schedule tasks",
				"delegate":  "Delegate to sub-agent",
			},
		},
		ToolPolicy: pe,
	})
	if strings.Contains(r.Prompt, "cron") {
		t.Error("expected globally-denied tool 'cron' to be excluded from preview prompt")
	}
	if strings.Contains(r.Prompt, "delegate") {
		t.Error("expected globally-denied tool 'delegate' to be excluded from preview prompt")
	}
	if !strings.Contains(r.Prompt, "read_file") {
		t.Error("expected non-denied tool 'read_file' to remain in preview prompt")
	}
}

// TestBuildPreviewPrompt_MCPToolGlobalDenyApplied proves that MCP tools
// injected from deps.MCPLister (store-based, not live registry) are also
// subject to a literal-name deny check via deps.ToolPolicy.IsDenied. A
// globally-denied MCP tool must be excluded from both the prompt text and
// ToolDefs, while other MCP tools from the same server remain included.
// This closes the gap where the MCPLister supplement path added MCP tools
// without consulting ToolPolicy at all.
func TestBuildPreviewPrompt_MCPToolGlobalDenyApplied(t *testing.T) {
	ag := baseAgent() // tools_config is nil — no per-agent deny
	pe := tools.NewPolicyEngine(&config.ToolsConfig{
		Deny: []string{"mcp_cloudflare__delete_dns_record"},
	})
	r := BuildPreviewPrompt(context.Background(), ag, PromptFull, "u1", PreviewDeps{
		MCPLister: &mockMCPLister{
			tools: []MCPToolPreviewInfo{
				{RegisteredName: "mcp_cloudflare__delete_dns_record", Description: "Delete a DNS record"},
				{RegisteredName: "mcp_cloudflare__list_dns_records", Description: "List DNS records"},
			},
		},
		ToolPolicy: pe,
	})

	// Prompt text no longer enumerates per-tool names/descriptions (removed as
	// pure duplication of the real schema now carried in ToolDefs/`tools:`) —
	// so allow/deny is asserted against ToolDefs below, not prompt text.
	if strings.Contains(r.Prompt, "mcp_cloudflare__delete_dns_record") {
		t.Error("expected globally-denied MCP tool to be excluded from preview prompt")
	}

	for _, def := range r.ToolDefs {
		if def.Function != nil && def.Function.Name == "mcp_cloudflare__delete_dns_record" {
			t.Errorf("expected globally-denied MCP tool to be excluded from ToolDefs, got: %+v", r.ToolDefs)
		}
	}
	var foundAllowed bool
	for _, def := range r.ToolDefs {
		if def.Function != nil && def.Function.Name == "mcp_cloudflare__list_dns_records" {
			foundAllowed = true
		}
	}
	if !foundAllowed {
		t.Errorf("expected non-denied MCP tool in ToolDefs, got: %+v", r.ToolDefs)
	}
}

// TestBuildPreviewPrompt_MCPToolAllowedViaGroupExpansion proves that an MCP
// tool granted only through a "group:mcp" AlsoAllow spec — the exact pattern
// production uses (see agentToolPolicyWithMCP in resolver_helpers.go, which
// injects AlsoAllow: ["group:mcp"] for every agent) — is correctly INCLUDED
// in BuildPreviewPrompt's ToolDefs and tool names.
//
// This is a regression test for the reg=nil bug: WouldAllow(nil, ...) cannot
// expand "group:mcp" (group expansion requires a concrete *tools.Registry to
// look up group membership), so with a restrictive Allow list that excludes
// the tool by literal name, the AlsoAllow group grant would silently fail to
// restore it — exactly what happened for every MCP tool in production preview.
// Using a *tools.Registry (not the narrower mockToolLister) here exercises the
// real reg-resolution path added by ResolveConcreteRegistry.
func TestBuildPreviewPrompt_MCPToolAllowedViaGroupExpansion(t *testing.T) {
	ag := baseAgent()
	// Mirrors production: Allow restricts to a literal core-tool name only,
	// and AlsoAllow additively grants the whole "group:mcp" set (the pattern
	// injected for every agent by resolver_helpers.go's agentToolPolicyWithMCP).
	ag.ToolsConfig = []byte(`{"allow":["read_file"],"alsoAllow":["group:mcp"]}`)

	reg := tools.NewRegistry()
	reg.Register(&mockTool{name: "read_file", desc: "Read a file"})
	reg.Register(&mockTool{name: "mcp_pg_query", desc: "Run PostgreSQL queries"})
	reg.RegisterToolGroup("mcp", []string{"mcp_pg_query"})

	pe := tools.NewPolicyEngine(&config.ToolsConfig{})

	r := BuildPreviewPrompt(context.Background(), ag, PromptFull, "u1", PreviewDeps{
		ToolLister: reg,
		ToolPolicy: pe,
	})

	var found *providers.ToolDefinition
	for i := range r.ToolDefs {
		if r.ToolDefs[i].Function.Name == "mcp_pg_query" {
			found = &r.ToolDefs[i]
		}
	}
	if found == nil {
		t.Fatalf("expected mcp_pg_query (granted via group:mcp AlsoAllow) in ToolDefs, got: %+v", r.ToolDefs)
	}
}

// TestBuildPreviewPrompt_MCPToolNotDeniedIncludedWithoutGroupExpansion proves
// that MCP tools supplied via deps.MCPLister are included in preview purely
// because they are NOT literally denied — inclusion does NOT depend on
// group-expansion machinery (e.g. "group:mcp" resolving against a registry)
// at all. This is a regression test for the preview-vs-live tool-visibility
// gap: MCP tools are only ever registered into ephemeral per-agent registry
// clones at live-connection time (internal/mcp/manager_connect.go), never
// into the shared/global registry used by preview, so any check that
// requires "group:mcp" to expand against a registry can never work correctly
// here. The agent's tools_config below deliberately has a restrictive Allow
// list (excluding the MCP tool by literal name) and no AlsoAllow group grant
// at all — under the old WouldAllow-based gate this MCP tool would have been
// incorrectly excluded, since group:mcp cannot resolve against the (empty)
// global registry. With the fix, since the tool is not in any Deny list, it
// must still appear.
func TestBuildPreviewPrompt_MCPToolNotDeniedIncludedWithoutGroupExpansion(t *testing.T) {
	ag := baseAgent()
	// Restrictive Allow list that does NOT include the MCP tool by literal
	// name, and NO AlsoAllow group grant — the exact scenario where
	// group-expansion-dependent gating would previously have failed.
	ag.ToolsConfig = []byte(`{"allow":["read_file"]}`)
	pe := tools.NewPolicyEngine(&config.ToolsConfig{
		Deny: []string{"mcp_cloudflare__delete_dns_record"}, // unrelated tool, different name
	})

	r := BuildPreviewPrompt(context.Background(), ag, PromptFull, "u1", PreviewDeps{
		MCPLister: &mockMCPLister{
			tools: []MCPToolPreviewInfo{
				{RegisteredName: "mcp_pg_query", Description: "Run PostgreSQL queries"},
			},
		},
		ToolPolicy: pe,
	})

	var found *providers.ToolDefinition
	for i := range r.ToolDefs {
		if r.ToolDefs[i].Function != nil && r.ToolDefs[i].Function.Name == "mcp_pg_query" {
			found = &r.ToolDefs[i]
		}
	}
	if found == nil {
		t.Fatalf("expected mcp_pg_query (not literally denied) in ToolDefs regardless of group-expansion resolution, got: %+v", r.ToolDefs)
	}
}
