package agent

// Tests for the two-tier image_generation gate in buildFilteredTools.
//
// Gate conditions (ALL must be true to inject the native tool):
//   (1) provider implements CapabilitiesAware and Capabilities().ImageGeneration == true
//   (2) Loop.allowImageGeneration == true  (agent config, defaults true; admin-only control)
//
// Additionally: final-iteration stripping takes priority — all tools removed.

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// imageCapableProvider is a stub provider that also implements CapabilitiesAware
// and can toggle ImageGeneration on/off.
type imageCapableProvider struct {
	stubProvider
	imageGen bool
}

func (p *imageCapableProvider) Capabilities() providers.ProviderCapabilities {
	return providers.ProviderCapabilities{
		Streaming:       true,
		ToolCalling:     true,
		ImageGeneration: p.imageGen,
	}
}

// buildImageGenLoop constructs a minimal Loop for gate testing.
// Uses the stubExecutor already defined in loop_pipeline_tool_callbacks_test.go.
func buildImageGenLoop(allowImageGen bool, prov providers.Provider) *Loop {
	return &Loop{
		provider:             prov,
		allowImageGeneration: allowImageGen,
		tools:                &stubExecutor{},
	}
}

// hasImageGenTool returns true if the slice contains the image_generation sentinel.
func hasImageGenTool(defs []providers.ToolDefinition) bool {
	for _, d := range defs {
		if d.Type == "image_generation" {
			return true
		}
	}
	return false
}

// ─── Gate: all conditions true → tool present ─────────────────────────────

func TestImageGenGate_AllTrue_ToolPresent(t *testing.T) {
	prov := &imageCapableProvider{imageGen: true}
	l := buildImageGenLoop(true, prov)

	defs, _, _ := l.buildFilteredTools(&RunRequest{}, false, 1, 10, nil)

	if !hasImageGenTool(defs) {
		t.Error("expected image_generation tool when all gate conditions are true")
	}
}

// ─── Gate: provider capability false → tool absent ────────────────────────

func TestImageGenGate_ProviderNoCapability_ToolAbsent(t *testing.T) {
	prov := &imageCapableProvider{imageGen: false}
	l := buildImageGenLoop(true, prov)

	defs, _, _ := l.buildFilteredTools(&RunRequest{}, false, 1, 10, nil)

	if hasImageGenTool(defs) {
		t.Error("image_generation must NOT be in tools when provider does not advertise ImageGeneration")
	}
}

// ─── Gate: provider not CapabilitiesAware → tool absent ──────────────────

func TestImageGenGate_ProviderNotCapabilitiesAware_ToolAbsent(t *testing.T) {
	// stubProvider (from intent_classify_test.go) does NOT implement CapabilitiesAware.
	prov := &stubProvider{}
	l := buildImageGenLoop(true, prov)

	defs, _, _ := l.buildFilteredTools(&RunRequest{}, false, 1, 10, nil)

	if hasImageGenTool(defs) {
		t.Error("image_generation must NOT be in tools when provider is not CapabilitiesAware")
	}
}

// ─── Gate: agent config disables → tool absent ───────────────────────────

func TestImageGenGate_AgentConfigDisabled_ToolAbsent(t *testing.T) {
	prov := &imageCapableProvider{imageGen: true}
	l := buildImageGenLoop(false, prov) // allowImageGeneration = false

	defs, _, _ := l.buildFilteredTools(&RunRequest{}, false, 1, 10, nil)

	if hasImageGenTool(defs) {
		t.Error("image_generation must NOT be in tools when agent config disables it")
	}
}

// ─── Final iteration strips all tools including image_generation ──────────

func TestImageGenGate_FinalIteration_AllToolsStripped(t *testing.T) {
	prov := &imageCapableProvider{imageGen: true}
	l := buildImageGenLoop(true, prov)

	// iteration == maxIter → final stripping path; gate never reached
	defs, _, _ := l.buildFilteredTools(&RunRequest{}, false, 5, 5, nil)

	if len(defs) != 0 {
		t.Errorf("final iteration must strip all tools; got %d: %v", len(defs), defs)
	}
}
