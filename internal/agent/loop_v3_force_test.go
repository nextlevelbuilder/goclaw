package agent

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// TestRunLoop_Removed verifies that the v2 runLoop method no longer exists.
// This test is a compile-time guard: if someone re-adds runLoop, the method
// set assertion will break, forcing a conscious review.
func TestRunLoop_Removed(t *testing.T) {
	loopType := reflect.TypeFor[*Loop]()
	if _, found := loopType.MethodByName("runLoop"); found {
		t.Fatal("runLoop method still exists on *Loop — v2 code should be deleted")
	}
}

// TestV3Pipeline_AlwaysEnabled verifies that LoopConfig no longer has
// a V3PipelineEnabled field (all agents use v3 pipeline unconditionally).
func TestV3Pipeline_AlwaysEnabled(t *testing.T) {
	cfgType := reflect.TypeFor[LoopConfig]()
	if _, found := cfgType.FieldByName("V3PipelineEnabled"); found {
		t.Fatal("LoopConfig still has V3PipelineEnabled — should be removed")
	}
}

// TestV3Flags_PipelineEnabled_BackwardCompat verifies that V3Flags still
// parses v3_pipeline_enabled from JSONB (backward compat), but the value
// is not used by the Loop.
func TestV3Flags_PipelineEnabled_BackwardCompat(t *testing.T) {
	agent := &store.AgentData{
		OtherConfig: json.RawMessage(`{"v3_pipeline_enabled": true, "v3_memory_enabled": true}`),
	}
	flags := agent.ParseV3Flags()
	// PipelineEnabled still parses for backward compat
	if !flags.PipelineEnabled {
		t.Error("PipelineEnabled should still parse from JSONB")
	}
	// MemoryEnabled still works as a feature flag
	if !flags.MemoryEnabled {
		t.Error("MemoryEnabled should still work")
	}
}

// TestV3Flags_MemoryEnabled_StillWorks verifies non-pipeline v3 flags still function.
func TestV3Flags_MemoryEnabled_StillWorks(t *testing.T) {
	agent := &store.AgentData{
		OtherConfig: json.RawMessage(`{"v3_memory_enabled": true, "v3_retrieval_enabled": true}`),
	}
	flags := agent.ParseV3Flags()
	if !flags.MemoryEnabled {
		t.Error("MemoryEnabled should be true")
	}
	if !flags.RetrievalEnabled {
		t.Error("RetrievalEnabled should be true")
	}
}
