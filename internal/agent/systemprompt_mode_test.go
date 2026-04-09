package agent

import (
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/store"
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

func TestTaskModePersonaFull(t *testing.T) {
	cfg := fullTestConfig()
	cfg.Mode = PromptTask
	prompt := BuildSystemPrompt(cfg)
	// Task mode now gets full persona (SOUL.md + IDENTITY.md)
	if !strings.Contains(prompt, "Persona & Identity") {
		t.Error("task mode should have full Persona section")
	}
	if !strings.Contains(prompt, "Playful, curious") {
		t.Error("task mode should include SOUL.md content")
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

func TestNoneModeSections(t *testing.T) {
	cfg := fullTestConfig()
	cfg.Mode = PromptNone
	cfg.PinnedSkillsSummary = "<available_skills><skill><name>weather</name></skill></available_skills>"
	cfg.HasMCPToolSearch = true
	cfg.ExtraPrompt = "extra context here"
	// None mode only gets TOOLS.md via ModeAllowlist (filtering happens in pipeline)
	cfg.ContextFiles = []bootstrap.ContextFile{
		{Path: "TOOLS.md", Content: "tool notes"},
	}
	prompt := BuildSystemPrompt(cfg)

	// Should have: Tooling, Workspace, Runtime, Pinned Skills, MCP search, Extra prompt, Safety slim
	for _, want := range []string{"## Tooling", "## Workspace", "## Runtime", "## Pinned Skills", "mcp_tool_search", "extra context here", "## Safety"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("none mode missing: %s", want)
		}
	}
	// Should NOT have: Persona, Memory Recall, Self-Evolution, Tool Call Style, Execution Bias, Sub-Agent, Recency
	for _, dropped := range []string{"# Persona", "## Memory Recall", "## Self-Evolution", "## Tool Call Style", "## Execution Bias", "## Sub-Agent Spawning", "Reminder: Follow AGENTS.md"} {
		if strings.Contains(prompt, dropped) {
			t.Errorf("none mode should not have: %s", dropped)
		}
	}
	// Size check: should be under 3000 chars (~750 tokens)
	if len(prompt) > 3000 {
		t.Errorf("none mode too large: %d chars", len(prompt))
	}
}

func TestNoneModeNoTime(t *testing.T) {
	cfg := fullTestConfig()
	cfg.Mode = PromptNone
	prompt := BuildSystemPrompt(cfg)
	if strings.Contains(prompt, "Current date:") {
		t.Error("none mode should not have time section")
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
	if mode != PromptTask {
		t.Errorf("subagent should cap at task, got %s", mode)
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

// --- Phase 4: Pinned Skills tests ---

func TestPinnedSkillsHybridSection(t *testing.T) {
	cfg := SystemPromptConfig{
		Mode:                PromptFull,
		HasSkillSearch:      true,
		PinnedSkillsSummary: "<available_skills><skill><name>github</name></skill></available_skills>",
	}
	prompt := BuildSystemPrompt(cfg)
	if !strings.Contains(prompt, "Pinned skills") {
		t.Error("hybrid section should have 'Pinned skills' header")
	}
	if !strings.Contains(prompt, "<available_skills>") {
		t.Error("hybrid section should include pinned skills XML")
	}
	if !strings.Contains(prompt, "skill_search") {
		t.Error("hybrid section should mention skill_search for other skills")
	}
}

func TestTaskModePinnedSkillsHybrid(t *testing.T) {
	cfg := fullTestConfig()
	cfg.Mode = PromptTask
	cfg.PinnedSkillsSummary = "<available_skills><skill><name>github</name></skill></available_skills>"
	prompt := BuildSystemPrompt(cfg)
	if !strings.Contains(prompt, "<available_skills>") {
		t.Error("task mode should include pinned skills XML")
	}
	if !strings.Contains(prompt, "skill_search") {
		t.Error("task mode should have skill_search for non-pinned")
	}
}

func TestFullModeNoPinnedNoChange(t *testing.T) {
	cfg := fullTestConfig()
	cfg.PinnedSkillsSummary = "" // no pinned skills
	prompt := BuildSystemPrompt(cfg)
	// Standard search mode (fullTestConfig has HasSkillSearch=true, no SkillsSummary)
	if !strings.Contains(prompt, "skill_search") {
		t.Error("full mode without pinned should use search mode")
	}
}

func TestPinnedSkillsMax10(t *testing.T) {
	raw := []byte(`{"pinned_skills":["a","b","c","d","e","f","g","h","i","j","k","l"]}`)
	ag := store.AgentData{OtherConfig: raw}
	pinned := ag.ParsePinnedSkills()
	if len(pinned) != 10 {
		t.Errorf("expected 10 pinned skills, got %d", len(pinned))
	}
}

// --- Phase 1 TDD: New behavior tests ---

func TestModeResolutionCronTask(t *testing.T) {
	// Cron sessions should cap at task (not minimal)
	mode := resolvePromptMode("", "agent:abc:cron:daily-report", "")
	if mode != PromptTask {
		t.Errorf("cron should resolve to task, got %s", mode)
	}
}

func TestModeResolutionCronConfigCapped(t *testing.T) {
	// Cron with config=full should be capped at task
	mode := resolvePromptMode("", "agent:abc:cron:daily", PromptFull)
	if mode != PromptTask {
		t.Errorf("cron with config=full should cap at task, got %s", mode)
	}
}

func TestModeResolutionHeartbeatMinimal(t *testing.T) {
	// Heartbeat stays minimal (no change from current)
	mode := resolvePromptMode("", "agent:abc:heartbeat", "")
	if mode != PromptMinimal {
		t.Errorf("heartbeat should stay minimal, got %s", mode)
	}
}

func TestModeResolutionHeartbeatConfigCapped(t *testing.T) {
	mode := resolvePromptMode("", "agent:abc:heartbeat", PromptFull)
	if mode != PromptMinimal {
		t.Errorf("heartbeat with config=full should cap at minimal, got %s", mode)
	}
}

func TestModeResolutionDelegateUsesConfig(t *testing.T) {
	// Delegate key starts with "delegate:", not "agent:" → sessionRest returns ""
	// → falls through to config mode, no auto-detect cap
	mode := resolvePromptMode("", "delegate:abc:target-agent", PromptFull)
	if mode != PromptFull {
		t.Errorf("delegate should use config mode (full), got %s", mode)
	}
}

func TestModeResolutionSubagentTaskCap(t *testing.T) {
	// Subagent with config=full should cap at task (not minimal)
	mode := resolvePromptMode("", "agent:abc:subagent:xyz", PromptFull)
	if mode != PromptTask {
		t.Errorf("subagent with config=full should cap at task, got %s", mode)
	}
}

func TestPinnedSkillsMinimalMode(t *testing.T) {
	cfg := SystemPromptConfig{
		Mode:                PromptMinimal,
		PinnedSkillsSummary: "<available_skills><skill><name>weather</name></skill></available_skills>",
		ToolNames:           []string{"exec", "read_file"},
	}
	prompt := BuildSystemPrompt(cfg)
	if !strings.Contains(prompt, "<available_skills>") {
		t.Error("minimal mode should include pinned skills XML")
	}
}

func TestMinimalAllowlistIncludesUserPredefined(t *testing.T) {
	files := []bootstrap.File{
		{Name: "AGENTS.md", Content: "rules"},
		{Name: "TOOLS.md", Content: "tools"},
		{Name: "USER_PREDEFINED.md", Content: "user rules"},
		{Name: "SOUL.md", Content: "persona"},
	}
	filtered := bootstrap.FilterForSession(files, "agent:abc:subagent:xyz")
	found := false
	for _, f := range filtered {
		if f.Name == "USER_PREDEFINED.md" {
			found = true
		}
	}
	if !found {
		t.Error("USER_PREDEFINED.md should be in minimal allowlist")
	}
	// SOUL.md should NOT be included
	for _, f := range filtered {
		if f.Name == "SOUL.md" {
			t.Error("SOUL.md should not be in minimal allowlist")
		}
	}
}

// --- Phase 2 TDD: Context file restructuring tests ---

func TestCapabilitiesInStableContextFiles(t *testing.T) {
	files := []bootstrap.ContextFile{
		{Path: "AGENTS.md", Content: "rules"},
		{Path: "CAPABILITIES.md", Content: "expertise"},
		{Path: "USER.md", Content: "user"},
	}
	stable, dynamic := splitStableDynamicContextFiles(files)
	found := false
	for _, f := range stable {
		if f.Path == "CAPABILITIES.md" {
			found = true
		}
	}
	if !found {
		t.Error("CAPABILITIES.md should be in stable context files")
	}
	for _, f := range dynamic {
		if f.Path == "CAPABILITIES.md" {
			t.Error("CAPABILITIES.md should not be in dynamic files")
		}
	}
}

func TestCapabilitiesInMinimalAllowlist(t *testing.T) {
	files := []bootstrap.File{
		{Name: "AGENTS.md", Content: "rules"},
		{Name: "CAPABILITIES.md", Content: "expertise"},
		{Name: "SOUL.md", Content: "persona"},
	}
	filtered := bootstrap.FilterForSession(files, "agent:abc:subagent:xyz")
	found := false
	for _, f := range filtered {
		if f.Name == "CAPABILITIES.md" {
			found = true
		}
	}
	if !found {
		t.Error("CAPABILITIES.md should pass minimal allowlist")
	}
}

func TestHeartbeatUsesAgentsCore(t *testing.T) {
	// Heartbeat resolves to minimal mode → ModeAllowlist("minimal") = {AGENTS_CORE.md, CAPABILITIES.md}
	mode := resolvePromptMode("", "agent:abc:heartbeat", "")
	if mode != PromptMinimal {
		t.Fatalf("heartbeat should resolve to minimal, got %s", mode)
	}
	allowlist := bootstrap.ModeAllowlist(string(mode))
	if allowlist == nil {
		t.Fatal("minimal allowlist should not be nil")
	}
	if !allowlist[bootstrap.AgentsCoreFile] {
		t.Error("minimal allowlist should include AGENTS_CORE.md")
	}
	if allowlist[bootstrap.AgentsFile] {
		t.Error("minimal allowlist should NOT include full AGENTS.md")
	}
}

func TestSubagentKeepsFullAgents(t *testing.T) {
	files := []bootstrap.File{
		{Name: "AGENTS.md", Content: "full rules"},
		{Name: "TOOLS.md", Content: "tools"},
	}
	filtered := bootstrap.FilterForSession(files, "agent:abc:subagent:xyz")
	hasFullAgents := false
	for _, f := range filtered {
		if f.Name == "AGENTS.md" {
			hasFullAgents = true
		}
	}
	if !hasFullAgents {
		t.Error("subagent (now task mode) should keep full AGENTS.md")
	}
}

func TestCapabilitiesNotPersonaFile(t *testing.T) {
	files := []bootstrap.ContextFile{
		{Path: "SOUL.md", Content: "persona"},
		{Path: "CAPABILITIES.md", Content: "expertise"},
		{Path: "AGENTS.md", Content: "rules"},
	}
	persona, other := splitPersonaFiles(files)
	for _, f := range persona {
		if f.Path == "CAPABILITIES.md" {
			t.Error("CAPABILITIES.md should not be a persona file")
		}
	}
	found := false
	for _, f := range other {
		if f.Path == "CAPABILITIES.md" {
			found = true
		}
	}
	if !found {
		t.Error("CAPABILITIES.md should be in other files (not persona)")
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

func TestTaskModeIdentityIncluded(t *testing.T) {
	cfg := SystemPromptConfig{
		Mode:      PromptTask,
		AgentID:   "tieu-thong",
		AgentType: store.AgentTypePredefined,
		ToolNames: []string{"exec", "read_file"},
		ContextFiles: []bootstrap.ContextFile{
			{Path: "SOUL.md", Content: "You are tieu-thong."},
			{Path: "IDENTITY.md", Content: "# IDENTITY.md\n- Name: Tieu Thong\n- Purpose: Knowledge agent"},
			{Path: "AGENTS_TASK.md", Content: "task rules"},
		},
	}
	prompt := BuildSystemPrompt(cfg)
	if !strings.Contains(prompt, "IDENTITY.md") {
		t.Error("task mode should include IDENTITY.md header")
	}
	if !strings.Contains(prompt, "Tieu Thong") {
		t.Error("task mode should include IDENTITY.md content")
	}
}
