package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func delegationArtifactTestContext() context.Context {
	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)
	ctx = WithDelegationID(ctx, "11111111-1111-1111-1111-111111111111")
	return WithDelegationArtifactInputs(ctx, "/runtime/delegations/input")
}

func TestDelegationArtifactRunRejectsAsyncChildModes(t *testing.T) {
	ctx := delegationArtifactTestContext()
	for _, mode := range []string{"async", "spawn", "background"} {
		if err := validateDelegationChildRunMode(ctx, "delegate", mode); err == nil {
			t.Fatalf("mode %q was accepted", mode)
		}
	}
	if err := validateDelegationChildRunMode(ctx, "delegate", "sync"); err != nil {
		t.Fatalf("sync mode rejected: %v", err)
	}
	if err := validateDelegationChildRunMode(context.Background(), "delegate", "async"); err != nil {
		t.Fatalf("normal async run rejected: %v", err)
	}
}

func TestSpawnToolRejectsDefaultAsyncInsideDelegationArtifactRun(t *testing.T) {
	tool := NewSpawnTool(nil, "parent", 0)
	result := tool.Execute(delegationArtifactTestContext(), map[string]any{
		"task": "must not escape the delegation lifetime",
	})
	if result == nil || !strings.Contains(result.ForLLM, "mode=\"sync\"") {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestDelegateToolRejectsAsyncInsideDelegationArtifactRunBeforeLookup(t *testing.T) {
	tool := &DelegateTool{}
	result := tool.Execute(delegationArtifactTestContext(), map[string]any{
		"agent_key": "child",
		"task":      "must not escape the delegation lifetime",
		"mode":      "async",
	})
	if result == nil || !strings.Contains(result.ForLLM, "mode=\"sync\"") {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestDelegateToolSchemaKeepsBoundedInputsOptional(t *testing.T) {
	schema := (&DelegateTool{}).Parameters()
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties = %#v", schema["properties"])
	}
	inputs, ok := properties["inputs"].(map[string]any)
	if !ok {
		t.Fatalf("inputs schema = %#v", properties["inputs"])
	}
	if got := inputs["type"]; got != "array" {
		t.Fatalf("inputs type = %#v, want array", got)
	}
	if got := inputs["maxItems"]; got != DelegationArtifactMaxFiles {
		t.Fatalf("inputs maxItems = %#v, want %d", got, DelegationArtifactMaxFiles)
	}
	if required, ok := schema["required"].([]string); ok {
		for _, name := range required {
			if name == "inputs" {
				t.Fatal("inputs unexpectedly became required")
			}
		}
	}
	if _, ok := properties["delegation_id"]; !ok {
		t.Fatal("schema omitted durable delegation result lookup")
	}
}
