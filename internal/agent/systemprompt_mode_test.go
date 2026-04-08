package agent

import (
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
)

// fullTestConfig returns a SystemPromptConfig with all features enabled.
func fullTestConfig() SystemPromptConfig {
	return SystemPromptConfig{
		Mode:           PromptFull,
		AgentID:        "test-agent",
		ToolNames:      []string{"exec", "read_file", "memory_search", "memory_get", "spawn"},
		HasMemory:      true,
		HasSpawn:       true,
		HasSkillSearch: true,
		OwnerIDs:       []string{"user1"},
		ContextFiles: []bootstrap.ContextFile{
			{Path: "SOUL.md", Content: "# Fox\n## Style\nPlayful, curious\n## Lore\nLong backstory..."},
			{Path: "AGENTS.md", Content: "agent rules"},
			{Path: "USER.md", Content: "user profile"},
		},
	}
}

// --- Full mode tests ---

func TestFullModeAllSections(t *testing.T) {
	prompt := BuildSystemPrompt(fullTestConfig())
	for _, section := range []string{"## Tooling", "## Safety", "## Tool Call Style",
		"## Memory Recall", "## Workspace", "## Runtime", "## Execution Bias"} {
		if !strings.Contains(prompt, section) {
			t.Errorf("full mode missing: %s", section)
		}
	}
}

// --- Minimal mode tests ---

func TestMinimalModeExclusions(t *testing.T) {
	cfg := fullTestConfig()
	cfg.Mode = PromptMinimal
	prompt := BuildSystemPrompt(cfg)
	if !strings.Contains(prompt, "## Tooling") {
		t.Error("minimal should have Tooling")
	}
	if !strings.Contains(prompt, "## Workspace") {
		t.Error("minimal should have Workspace")
	}
	for _, dropped := range []string{"## Skills", "## User Identity", "## Execution Bias", "## Tool Call Style"} {
		if strings.Contains(prompt, dropped) {
			t.Errorf("minimal should not have: %s", dropped)
		}
	}
}

// --- Task mode tests ---

func TestTaskModeKeepsSections(t *testing.T) {
	cfg := fullTestConfig()
	cfg.Mode = PromptTask
	prompt := BuildSystemPrompt(cfg)
	for _, want := range []string{"## Tooling", "## Safety", "## Execution Bias", "## Workspace", "## Runtime"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("task mode missing: %s", want)
		}
	}
}

func TestTaskModeDropsSections(t *testing.T) {
	cfg := fullTestConfig()
	cfg.Mode = PromptTask
	cfg.SelfEvolve = true
	cfg.AgentType = "predefined"
	prompt := BuildSystemPrompt(cfg)
	for _, dropped := range []string{"## Self-Evolution", "## Tool Call Style", "## Sub-Agent Spawning",
		"Reminder: Follow AGENTS.md"} {
		if strings.Contains(prompt, dropped) {
			t.Errorf("task mode should not have: %s", dropped)
		}
	}
}

func TestTaskModePersonaSlim(t *testing.T) {
	cfg := fullTestConfig()
	cfg.Mode = PromptTask
	prompt := BuildSystemPrompt(cfg)
	// Style should be extracted as slim echo
	if !strings.Contains(prompt, "Playful, curious") {
		t.Error("task mode should include style echo from SOUL.md")
	}
	// Full lore content should NOT appear (full persona section dropped)
	if strings.Contains(prompt, "Long backstory") {
		t.Error("task mode should not include full SOUL.md body")
	}
}

func TestTaskModeSafetySlim(t *testing.T) {
	cfg := fullTestConfig()
	cfg.Mode = PromptTask
	prompt := BuildSystemPrompt(cfg)
	// Should have safety
	if !strings.Contains(prompt, "## Safety") {
		t.Error("task mode should have Safety section")
	}
	// Should NOT have identity anchoring verbose text
	if strings.Contains(prompt, "configuration files (SOUL.md, IDENTITY.md") {
		t.Error("task mode should not have identity anchoring")
	}
}

func TestTaskModeMemorySlim(t *testing.T) {
	cfg := fullTestConfig()
	cfg.Mode = PromptTask
	prompt := BuildSystemPrompt(cfg)
	// Should have slim memory instruction
	if !strings.Contains(prompt, "call memory_search") {
		t.Error("task mode should have slim memory instruction")
	}
	// Should NOT have verbose memory recall section
	if strings.Contains(prompt, "## Memory Recall") {
		t.Error("task mode should not have verbose Memory Recall section")
	}
}

// --- None mode tests ---

func TestNoneModeIdentityOnly(t *testing.T) {
	cfg := fullTestConfig()
	cfg.Mode = PromptNone
	prompt := BuildSystemPrompt(cfg)
	if len(prompt) > 200 {
		t.Errorf("none mode should be minimal, got %d chars", len(prompt))
	}
	if strings.Contains(prompt, "## Tooling") {
		t.Error("none mode should not have Tooling")
	}
}

// --- Mode resolution tests ---

func TestModeResolutionRuntimeWins(t *testing.T) {
	mode := resolvePromptMode(PromptTask, "session-1", PromptFull)
	if mode != PromptTask {
		t.Errorf("runtime should win, got %s", mode)
	}
}

func TestModeResolutionSubagentAutoDetect(t *testing.T) {
	mode := resolvePromptMode("", "agent:abc:subagent:xyz", PromptTask)
	if mode != PromptMinimal {
		t.Errorf("subagent should cap at minimal, got %s", mode)
	}
}

func TestModeResolutionConfigFallback(t *testing.T) {
	mode := resolvePromptMode("", "session-1", PromptTask)
	if mode != PromptTask {
		t.Errorf("config should be used, got %s", mode)
	}
}

func TestModeResolutionDefault(t *testing.T) {
	mode := resolvePromptMode("", "session-1", "")
	if mode != PromptFull {
		t.Errorf("default should be full, got %s", mode)
	}
}

func TestMinModeOrdering(t *testing.T) {
	if minMode(PromptTask, PromptMinimal) != PromptMinimal {
		t.Error("min(task, minimal) should be minimal")
	}
	if minMode(PromptFull, PromptTask) != PromptTask {
		t.Error("min(full, task) should be task")
	}
	if minMode(PromptNone, PromptFull) != PromptNone {
		t.Error("min(none, full) should be none")
	}
}
