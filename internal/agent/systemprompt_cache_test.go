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

// TestTimeSectionWithTimezone verifies local time appears when DefaultTimezone is set.
func TestTimeSectionWithTimezone(t *testing.T) {
	prompt := BuildSystemPrompt(SystemPromptConfig{Mode: PromptFull, DefaultTimezone: "Asia/Ho_Chi_Minh"})
	if !strings.Contains(prompt, "Asia/Ho_Chi_Minh") {
		t.Error("prompt should contain timezone name when DefaultTimezone is set")
	}
	if !strings.Contains(prompt, "(UTC) /") {
		t.Error("prompt should contain both UTC and local time")
	}
}

// TestTimeSectionWithoutTimezone verifies UTC-only format when no timezone is set.
func TestTimeSectionWithoutTimezone(t *testing.T) {
	lines := buildTimeSection("")
	if len(lines) == 0 {
		t.Fatal("buildTimeSection returned empty")
	}
	dateLine := lines[0]
	if !strings.Contains(dateLine, "(UTC)") {
		t.Errorf("UTC-only format should contain (UTC): %s", dateLine)
	}
	// " / " is the separator between UTC and local time — should not appear when no timezone set.
	if strings.Contains(dateLine, " / ") {
		t.Errorf("UTC-only format should not contain local time separator: %s", dateLine)
	}
}

// TestTimeSectionFormat verifies the time section includes HH:MM (not HH:MM:SS).
func TestTimeSectionFormat(t *testing.T) {
	lines := buildTimeSection("Asia/Ho_Chi_Minh")
	if len(lines) == 0 {
		t.Fatal("buildTimeSection returned empty")
	}
	dateLine := lines[0]
	if !strings.Contains(dateLine, "Current date/time:") {
		t.Fatalf("unexpected date line: %s", dateLine)
	}
	// Should contain HH:MM format (e.g. "15:04") but NOT HH:MM:SS
	if strings.Contains(dateLine, ":") && strings.Count(dateLine, ":") > 2 {
		// Check for seconds pattern (HH:MM:SS) — should not be present
		for _, part := range strings.Fields(dateLine) {
			if strings.Count(part, ":") == 2 && len(part) == 8 {
				t.Errorf("time section should not include seconds: %s", dateLine)
			}
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
	timeIdx := strings.Index(prompt, "Current date/time:")
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
	for _, want := range []string{"Current date/time:", "## Runtime"} {
		if !strings.Contains(dynamic, want) {
			t.Errorf("dynamic suffix missing %q", want)
		}
	}
	// Time must NOT be in stable
	if strings.Contains(stable, "Current date/time:") {
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
