package methods

import (
	"encoding/json"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

func TestBuildConfigDefaults_NilCfg(t *testing.T) {
	resp := buildConfigDefaults(nil)

	p := agent.DefaultPruningValues()
	if resp.Agents.ContextPruning.KeepLastAssistants != p.KeepLastAssistants {
		t.Errorf("keepLastAssistants = %d, want %d",
			resp.Agents.ContextPruning.KeepLastAssistants, p.KeepLastAssistants)
	}
	if resp.Agents.ContextPruning.SoftTrim.MaxChars != p.SoftTrimMaxChars {
		t.Errorf("softTrim.maxChars = %d, want %d",
			resp.Agents.ContextPruning.SoftTrim.MaxChars, p.SoftTrimMaxChars)
	}
	if resp.Agents.ContextPruning.TTL != p.TTL {
		t.Errorf("ttl = %q, want %q", resp.Agents.ContextPruning.TTL, p.TTL)
	}

	s := tools.DefaultSubagentConfig()
	if resp.Agents.Subagents.MaxConcurrent != s.MaxConcurrent {
		t.Errorf("subagents.maxConcurrent = %d, want %d",
			resp.Agents.Subagents.MaxConcurrent, s.MaxConcurrent)
	}
	if resp.Agents.Subagents.MaxRetries != s.MaxRetries {
		t.Errorf("subagents.maxRetries = %d, want %d",
			resp.Agents.Subagents.MaxRetries, s.MaxRetries)
	}
	if resp.Agents.Subagents.ArchiveAfterMinutes != s.ArchiveAfterMinutes {
		t.Errorf("subagents.archiveAfterMinutes = %d, want %d",
			resp.Agents.Subagents.ArchiveAfterMinutes, s.ArchiveAfterMinutes)
	}
}

func TestBuildConfigDefaults_EmptyCfg(t *testing.T) {
	cfg := &config.Config{}
	resp := buildConfigDefaults(cfg)

	// Empty cfg (no overlay) should match bare defaults.
	p := agent.DefaultPruningValues()
	if resp.Agents.ContextPruning.SoftTrimRatio != p.SoftTrimRatio {
		t.Errorf("softTrimRatio = %v, want %v",
			resp.Agents.ContextPruning.SoftTrimRatio, p.SoftTrimRatio)
	}
}

func TestBuildConfigDefaults_PartialOverlay(t *testing.T) {
	enabledFalse := false
	cfg := &config.Config{}
	cfg.Agents.Defaults.ContextPruning = &config.ContextPruningConfig{
		KeepLastAssistants: 7,
		SoftTrim: &config.ContextPruningSoftTrim{
			MaxChars: 9999,
		},
		HardClear: &config.ContextPruningHardClear{
			Enabled:     &enabledFalse,
			Placeholder: "custom",
		},
		TTL: "10m",
	}
	cfg.Agents.Defaults.Subagents = &config.SubagentsConfig{
		MaxConcurrent: 16,
		MaxRetries:    5,
	}

	resp := buildConfigDefaults(cfg)

	if got := resp.Agents.ContextPruning.KeepLastAssistants; got != 7 {
		t.Errorf("keepLastAssistants = %d, want 7", got)
	}
	if got := resp.Agents.ContextPruning.SoftTrim.MaxChars; got != 9999 {
		t.Errorf("softTrim.maxChars = %d, want 9999", got)
	}
	// HeadChars not overlaid — should keep default
	p := agent.DefaultPruningValues()
	if got := resp.Agents.ContextPruning.SoftTrim.HeadChars; got != p.SoftTrimHeadChars {
		t.Errorf("softTrim.headChars = %d, want default %d", got, p.SoftTrimHeadChars)
	}
	if got := resp.Agents.ContextPruning.HardClear.Enabled; got != false {
		t.Errorf("hardClear.enabled = %v, want false", got)
	}
	if got := resp.Agents.ContextPruning.HardClear.Placeholder; got != "custom" {
		t.Errorf("hardClear.placeholder = %q, want %q", got, "custom")
	}
	if got := resp.Agents.ContextPruning.TTL; got != "10m" {
		t.Errorf("ttl = %q, want 10m", got)
	}
	if got := resp.Agents.Subagents.MaxConcurrent; got != 16 {
		t.Errorf("subagents.maxConcurrent = %d, want 16", got)
	}
	if got := resp.Agents.Subagents.MaxRetries; got != 5 {
		t.Errorf("subagents.maxRetries = %d, want 5", got)
	}
	// ArchiveAfterMinutes not overlaid — should keep default
	s := tools.DefaultSubagentConfig()
	if got := resp.Agents.Subagents.ArchiveAfterMinutes; got != s.ArchiveAfterMinutes {
		t.Errorf("subagents.archiveAfterMinutes = %d, want default %d", got, s.ArchiveAfterMinutes)
	}
}

func TestBuildConfigDefaults_JSONShape(t *testing.T) {
	// JSON roundtrip: UI expects nested keys softTrim.maxChars, hardClear.enabled, etc.
	resp := buildConfigDefaults(nil)
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	agentsMap, _ := parsed["agents"].(map[string]any)
	if agentsMap == nil {
		t.Fatal("missing agents key")
	}
	pruning, _ := agentsMap["contextPruning"].(map[string]any)
	if pruning == nil {
		t.Fatal("missing agents.contextPruning")
	}
	softTrim, _ := pruning["softTrim"].(map[string]any)
	if softTrim == nil {
		t.Fatal("missing agents.contextPruning.softTrim")
	}
	if _, ok := softTrim["maxChars"]; !ok {
		t.Error("missing softTrim.maxChars")
	}
	hardClear, _ := pruning["hardClear"].(map[string]any)
	if hardClear == nil {
		t.Fatal("missing agents.contextPruning.hardClear")
	}
	if _, ok := hardClear["enabled"]; !ok {
		t.Error("missing hardClear.enabled")
	}
	subs, _ := agentsMap["subagents"].(map[string]any)
	if subs == nil {
		t.Fatal("missing agents.subagents")
	}
	for _, k := range []string{"maxConcurrent", "maxSpawnDepth", "maxChildrenPerAgent", "archiveAfterMinutes", "maxRetries"} {
		if _, ok := subs[k]; !ok {
			t.Errorf("missing subagents.%s", k)
		}
	}
}
