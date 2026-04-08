package agent

import (
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/skills"
)

func TestGenerateSkillDraft_ContainsValidFrontmatter(t *testing.T) {
	draft := GenerateSkillDraft("web_search", 150, 0.85)

	name, desc, _, _ := skills.ParseSkillFrontmatter(draft)
	if name == "" {
		t.Fatal("draft frontmatter missing 'name' field")
	}
	if !strings.Contains(name, "web_search") {
		t.Errorf("name = %q, want to contain 'web_search'", name)
	}
	if desc == "" {
		t.Fatal("draft frontmatter missing 'description' field")
	}
}

func TestGenerateSkillDraft_IncludesToolMetrics(t *testing.T) {
	draft := GenerateSkillDraft("exec", 200, 0.92)

	if !strings.Contains(draft, "exec") {
		t.Error("draft missing tool name 'exec'")
	}
	if !strings.Contains(draft, "200") {
		t.Error("draft missing call count '200'")
	}
	if !strings.Contains(draft, "92%") {
		t.Error("draft missing success rate '92%'")
	}
}
