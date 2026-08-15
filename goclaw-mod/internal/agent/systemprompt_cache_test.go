package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// TestToolNamesAreSorted verifies that BuildSystemPrompt produces identical
// output regardless of input tool order — critical for Anthropic prompt caching.
func TestToolNamesAreSorted(t *testing.T) {
	cfg1 := SystemPromptConfig{
		Mode:      PromptFull,
		ToolNames: []string{"exec", "read_file", "browser", "memory_search"},
	}
	cfg2 := SystemPromptConfig{
		Mode:      PromptFull,
		ToolNames: []string{"memory_search", "browser", "read_file", "exec"},
	}
	p1 := BuildSystemPrompt(cfg1)
	p2 := BuildSystemPrompt(cfg2)
	if p1 != p2 {
		t.Error("BuildSystemPrompt output differs for same tool names in different order")
	}
}

// TestTimeSectionContainsDate verifies the time section includes today's date.
func TestTimeSectionContainsDate(t *testing.T) {
	prompt := BuildSystemPrompt(SystemPromptConfig{Mode: PromptFull})
	today := time.Now().UTC().Format("2006-01-02")
	if !strings.Contains(prompt, today) {
		t.Errorf("prompt missing today's date %s", today)
	}
}

// TestTimeSectionDateOnly verifies the time section uses date+weekday only,
// not HH:MM:SS which would bust the cache every second.
func TestTimeSectionDateOnly(t *testing.T) {
	lines := buildTimeSection()
	if len(lines) == 0 {
		t.Fatal("buildTimeSection returned empty")
	}
	// The date line should contain "Current date:" and a weekday, but no colon-separated time.
	dateLine := lines[0]
	if !strings.Contains(dateLine, "Current date:") {
		t.Fatalf("unexpected date line: %s", dateLine)
	}
	// Strip the "Current date: " prefix, then check no HH:MM pattern remains.
	after := strings.TrimPrefix(dateLine, "Current date: ")
	// Remove "(UTC)" suffix for clean check.
	after = strings.TrimSuffix(after, " (UTC)")
	after = strings.TrimSpace(after)
	// Format is "2006-01-02 Monday" — no ":" should appear in the date/weekday part.
	parts := strings.FieldsSeq(after)
	for p := range parts {
		if strings.Count(p, ":") >= 2 {
			t.Errorf("time section contains time component: %s", dateLine)
		}
	}
}

// TestCacheBoundaryMarkerPresent verifies the boundary marker is in the prompt output.
func TestCacheBoundaryMarkerPresent(t *testing.T) {
	prompt := BuildSystemPrompt(SystemPromptConfig{Mode: PromptFull})
	if !strings.Contains(prompt, CacheBoundaryMarker) {
		t.Error("prompt missing cache boundary marker")
	}
}

// TestCacheBoundaryBeforeTime verifies the boundary marker appears before the time section.
func TestCacheBoundaryBeforeTime(t *testing.T) {
	prompt := BuildSystemPrompt(SystemPromptConfig{Mode: PromptFull})
	boundaryIdx := strings.Index(prompt, CacheBoundaryMarker)
	timeIdx := strings.Index(prompt, "Current date:")
	if boundaryIdx < 0 || timeIdx < 0 {
		t.Fatal("missing boundary or time section")
	}
	if boundaryIdx >= timeIdx {
		t.Error("cache boundary must appear before time section")
	}
}

// TestCacheBoundaryConstantConsistency ensures agent and providers packages
// use the same boundary marker string (H1: prevents silent drift).
func TestCacheBoundaryConstantConsistency(t *testing.T) {
	if CacheBoundaryMarker != providers.CacheBoundaryMarker {
		t.Errorf("agent.CacheBoundaryMarker=%q != providers.CacheBoundaryMarker=%q",
			CacheBoundaryMarker, providers.CacheBoundaryMarker)
	}
}

// --- Phase 1 tests ---

// TestCacheBoundaryPosition verifies stable sections above boundary, dynamic below.
func TestCacheBoundaryPosition(t *testing.T) {
	cfg := SystemPromptConfig{
		Mode:      PromptFull,
		ToolNames: []string{"exec", "read_file", "memory_search", "memory_get"},
		HasMemory: true,
		OwnerIDs:  []string{"user1"},
	}
	prompt := BuildSystemPrompt(cfg)
	parts := strings.SplitN(prompt, CacheBoundaryMarker, 2)
	if len(parts) != 2 {
		t.Fatal("expected 2 parts split at boundary")
	}
	stable, dynamic := parts[0], parts[1]
	// Stable should contain these sections
	for _, want := range []string{"## Tooling", "## Safety", "## Memory Recall", "## User Identity"} {
		if !strings.Contains(stable, want) {
			t.Errorf("stable prefix missing %q", want)
		}
	}
	// Dynamic should contain these sections
	for _, want := range []string{"Current date:", "## Runtime"} {
		if !strings.Contains(dynamic, want) {
			t.Errorf("dynamic suffix missing %q", want)
		}
	}
	// Time must NOT be in stable
	if strings.Contains(stable, "Current date:") {
		t.Error("stable prefix should not contain time section")
	}
}

// TestExecutionBiasInFullMode verifies Execution Bias present in full mode.
func TestExecutionBiasInFullMode(t *testing.T) {
	prompt := BuildSystemPrompt(SystemPromptConfig{Mode: PromptFull})
	if !strings.Contains(prompt, "## Execution Bias") {
		t.Error("full mode missing Execution Bias section")
	}
}

// TestExecutionBiasAbsentInMinimal verifies Execution Bias absent in minimal mode.
func TestExecutionBiasAbsentInMinimal(t *testing.T) {
	prompt := BuildSystemPrompt(SystemPromptConfig{Mode: PromptMinimal})
	if strings.Contains(prompt, "## Execution Bias") {
		t.Error("minimal mode should not have Execution Bias section")
	}
}

// TestExecutionBiasAbsentInBootstrap verifies Execution Bias suppressed during bootstrap.
func TestExecutionBiasAbsentInBootstrap(t *testing.T) {
	prompt := BuildSystemPrompt(SystemPromptConfig{Mode: PromptFull, IsBootstrap: true})
	if strings.Contains(prompt, "## Execution Bias") {
		t.Error("bootstrap mode should not have Execution Bias section")
	}
}

// TestStableFilesAboveBoundary verifies AGENTS.md lands above boundary,
// USER.md lands below boundary.
func TestStableFilesAboveBoundary(t *testing.T) {
	cfg := SystemPromptConfig{
		Mode: PromptFull,
		ContextFiles: []bootstrap.ContextFile{
			{Path: "AGENTS.md", Content: "agent rules here"},
			{Path: "USER.md", Content: "user profile here"},
		},
	}
	prompt := BuildSystemPrompt(cfg)
	parts := strings.SplitN(prompt, CacheBoundaryMarker, 2)
	if len(parts) != 2 {
		t.Fatal("expected 2 parts")
	}
	// AGENTS.md is stable → above boundary
	if !strings.Contains(parts[0], "agent rules here") {
		t.Error("AGENTS.md should be above cache boundary")
	}
	// USER.md is dynamic → below boundary
	if !strings.Contains(parts[1], "user profile here") {
		t.Error("USER.md should be below cache boundary")
	}
}
