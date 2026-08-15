package agent

import (
	"testing"

	"github.com/google/uuid"
)

// TestBuildMemoryFlushPromptConfig_AgentUUIDPopulated asserts the
// SystemPromptConfig returned by buildMemoryFlushPromptConfig carries the
// AgentUUID. loop_history.go always set it but memoryflush.go historically
// did not, which would have caused identity drift in downstream DomainEvents
// if AgentUUID ever reached the stable cache prefix.
func TestBuildMemoryFlushPromptConfig_AgentUUIDPopulated(t *testing.T) {
	u := uuid.New()
	cfg := buildMemoryFlushPromptConfig(
		"test-agent",
		u.String(),
		"claude-opus",
		"/workspace",
		[]string{"read_file", "write_file"},
		true,
		"anthropic",
	)

	if cfg.AgentUUID != u.String() {
		t.Errorf("AgentUUID = %q, want %q", cfg.AgentUUID, u.String())
	}
	if cfg.AgentID != "test-agent" {
		t.Errorf("AgentID = %q, want %q", cfg.AgentID, "test-agent")
	}
	if cfg.Model != "claude-opus" {
		t.Errorf("Model = %q, want %q", cfg.Model, "claude-opus")
	}
	if cfg.Workspace != "/workspace" {
		t.Errorf("Workspace = %q, want %q", cfg.Workspace, "/workspace")
	}
	if cfg.Mode != PromptMinimal {
		t.Errorf("Mode = %q, want %q", cfg.Mode, PromptMinimal)
	}
	if !cfg.HasMemory {
		t.Error("HasMemory should be true")
	}
	if cfg.ProviderType != "anthropic" {
		t.Errorf("ProviderType = %q, want %q", cfg.ProviderType, "anthropic")
	}
	if len(cfg.ToolNames) != 2 {
		t.Errorf("ToolNames len = %d, want 2", len(cfg.ToolNames))
	}
}

// TestBuildMemoryFlushPromptConfig_EmptyAgentUUID guards against a regression where
// a Loop with uuid.Nil (zero value) would still produce a non-empty AgentUUID string.
// The .String() of uuid.Nil is "00000000-0000-0000-0000-000000000000" — not empty,
// but it MUST be identifiable downstream so the publish-time observer in eventbus
// can still warn on it after Fix C wires validateAgentID into bus.Publish.
func TestBuildMemoryFlushPromptConfig_ZeroUUIDStringified(t *testing.T) {
	cfg := buildMemoryFlushPromptConfig(
		"zero-agent",
		uuid.Nil.String(),
		"m",
		"/w",
		nil,
		false,
		"p",
	)

	if cfg.AgentUUID != uuid.Nil.String() {
		t.Errorf("AgentUUID = %q, want %q", cfg.AgentUUID, uuid.Nil.String())
	}
}
