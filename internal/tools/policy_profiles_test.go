package tools

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/config"
)

func TestResearchProfileIncludesResearchTooling(t *testing.T) {
	available := []string{
		"read_file",
		"write_file",
		"exec",
		"web_search",
		"web_fetch",
		"browser",
		"memory_search",
		"memory_get",
		"read_image",
		"read_document",
		"skill_search",
		"mcp_tool_search",
		"mcp_tavily__tavily_search",
		"mcp_perplexity__perplexity_research",
		"spawn",
	}

	RegisterToolGroup("mcp", []string{
		"mcp_tavily__tavily_search",
		"mcp_perplexity__perplexity_research",
	})
	defer UnregisterToolGroup("mcp")

	pe := NewPolicyEngine(&config.ToolsConfig{})
	got := pe.applyProfile(available, "research")

	expected := map[string]bool{
		"read_file":                           true,
		"write_file":                          true,
		"exec":                                true,
		"web_search":                          true,
		"web_fetch":                           true,
		"browser":                             true,
		"memory_search":                       true,
		"memory_get":                          true,
		"read_image":                          true,
		"read_document":                       true,
		"skill_search":                        true,
		"mcp_tool_search":                     true,
		"mcp_tavily__tavily_search":           true,
		"mcp_perplexity__perplexity_research": true,
	}

	if len(got) != len(expected) {
		t.Fatalf("research profile returned %d tools, want %d: %v", len(got), len(expected), got)
	}

	for _, name := range got {
		if !expected[name] {
			t.Fatalf("unexpected tool in research profile: %s", name)
		}
		delete(expected, name)
	}

	for missing := range expected {
		t.Fatalf("missing expected tool in research profile: %s", missing)
	}
}
