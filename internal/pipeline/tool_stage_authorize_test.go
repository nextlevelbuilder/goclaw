package pipeline

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// makeAuthorizeGate builds a PipelineDeps.AuthorizeToolCall callback backed by
// a static allowlist and an optional deferred-activator, mirroring the contract
// that makeAuthorizeToolCall in loop_pipeline_callbacks.go provides.
func makeAuthorizeGate(
	allowed map[string]bool, // nil = allow-all
	tryActivate func(name string) bool, // nil = no activator
	isDenied func(name string) bool, // nil = never denied
) func(ctx context.Context, state *RunState, tc providers.ToolCall) (bool, string) {
	return func(_ context.Context, state *RunState, tc providers.ToolCall) (bool, string) {
		a := state.Tool.AllowedTools
		if a == nil {
			return true, ""
		}
		if a[tc.Name] {
			return true, ""
		}
		if tryActivate != nil && tryActivate(tc.Name) {
			if isDenied != nil && isDenied(tc.Name) {
				return false, "tool not allowed by policy: " + tc.Name
			}
			a[tc.Name] = true
			return true, ""
		}
		return false, "tool not allowed by policy: " + tc.Name
	}
}

// makeMinimalDeps returns a PipelineDeps just sufficient for ToolStage tests.
func makeMinimalDeps(authorize func(context.Context, *RunState, providers.ToolCall) (bool, string)) *PipelineDeps {
	return &PipelineDeps{
		Config:            PipelineConfig{MaxToolCalls: 100},
		AuthorizeToolCall: authorize,
		ExecuteToolCall: func(_ context.Context, state *RunState, tc providers.ToolCall) ([]providers.Message, error) {
			return []providers.Message{{Role: "tool", Content: "ok", ToolCallID: tc.ID}}, nil
		},
	}
}

// stateWithAllowedTools returns a minimal RunState with AllowedTools pre-set.
func stateWithAllowedTools(allowed map[string]bool) *RunState {
	st := defaultState()
	st.Tool.AllowedTools = allowed
	return st
}

func toolCallFor(name string) providers.ToolCall {
	return providers.ToolCall{ID: "tc-" + name, Name: name}
}

// fakeResponse puts tool calls into the state's ThinkStage so ToolStage finds them.
func setLastResponse(state *RunState, calls []providers.ToolCall) {
	state.Think.LastResponse = &providers.ChatResponse{ToolCalls: calls}
}

// --- Tests ---

// TestAuthorizeGate_NilAllowlist_AllowsAll verifies that a nil AllowedTools map
// means no per-iteration restriction (allow-all semantics).
func TestAuthorizeGate_NilAllowlist_AllowsAll(t *testing.T) {
	t.Parallel()
	st := defaultState()
	st.Tool.AllowedTools = nil // explicit nil

	gate := makeAuthorizeGate(nil, nil, nil)
	tc := toolCallFor("exec")

	ok, reason := gate(context.Background(), st, tc)
	if !ok {
		t.Errorf("nil allowlist must allow all tools; got blocked with reason %q", reason)
	}
}

// TestAuthorizeGate_NamePresent_Allows checks that a tool explicitly in the
// allowlist passes through.
func TestAuthorizeGate_NamePresent_Allows(t *testing.T) {
	t.Parallel()
	st := stateWithAllowedTools(map[string]bool{"exec": true, "read_file": true})

	gate := makeAuthorizeGate(nil, nil, nil)
	for _, name := range []string{"exec", "read_file"} {
		ok, reason := gate(context.Background(), st, toolCallFor(name))
		if !ok {
			t.Errorf("tool %q should be allowed; got blocked: %q", name, reason)
		}
	}
}

// TestAuthorizeGate_NameAbsent_NoActivator_Denies confirms fail-closed: a name
// not in the allowlist with no activator is blocked.
func TestAuthorizeGate_NameAbsent_NoActivator_Denies(t *testing.T) {
	t.Parallel()
	st := stateWithAllowedTools(map[string]bool{"read_file": true})

	gate := makeAuthorizeGate(nil, nil, nil)
	ok, reason := gate(context.Background(), st, toolCallFor("exec"))
	if ok {
		t.Error("tool absent from allowlist with no activator must be denied")
	}
	if reason == "" {
		t.Error("denial must include a non-empty reason")
	}
}

// TestAuthorizeGate_DeferredActivate_ThenDenied ensures that lazy activation
// followed by a positive IsDenied check still blocks the tool.
func TestAuthorizeGate_DeferredActivate_ThenDenied(t *testing.T) {
	t.Parallel()
	st := stateWithAllowedTools(map[string]bool{})

	activated := false
	tryActivate := func(name string) bool {
		activated = true
		return name == "mcp_svc__exec_cmd"
	}
	isDenied := func(name string) bool {
		return name == "mcp_svc__exec_cmd" // explicitly denied
	}

	gate := makeAuthorizeGate(nil, tryActivate, isDenied)
	ok, reason := gate(context.Background(), st, toolCallFor("mcp_svc__exec_cmd"))

	if !activated {
		t.Error("expected TryActivateDeferred to be called")
	}
	if ok {
		t.Error("tool allowed by activator but denied by policy must still be blocked")
	}
	if reason == "" {
		t.Error("denial must include a non-empty reason")
	}
	if st.Tool.AllowedTools["mcp_svc__exec_cmd"] {
		t.Error("denied tool must not be added to AllowedTools")
	}
}

// TestAuthorizeGate_PrefixedName_CanonicalLookup is the regression guard for the
// toolCallPrefix bug: when an agent has toolCallPrefix set, the model emits
// "proxy_exec" but AllowedTools is keyed by canonical name "exec". The gate must
// resolve the name before lookup, otherwise every prefixed call is wrongly blocked.
//
// This test simulates the resolution by calling the gate with a *pre-resolved*
// canonical name, matching what makeAuthorizeToolCall does after calling
// resolveToolCallName(tc.Name). The pipeline layer always receives the resolved
// name because AuthorizeToolCall in the real callback operates on the canonical
// form.
func TestAuthorizeGate_PrefixedName_CanonicalLookup(t *testing.T) {
	t.Parallel()
	// AllowedTools uses canonical name "exec" (as ThinkStage builds it from FilterTools).
	st := stateWithAllowedTools(map[string]bool{"exec": true})

	gate := makeAuthorizeGate(nil, nil, nil)

	// Simulate what resolveToolCallName("proxy_exec") returns with prefix "proxy_":
	// the canonical name "exec" is passed to the gate.
	resolvedName := "exec" // prefix already stripped by makeAuthorizeToolCall
	tc := providers.ToolCall{ID: "tc-prefix", Name: resolvedName}

	ok, reason := gate(context.Background(), st, tc)
	if !ok {
		t.Errorf("canonical name after prefix resolution must be allowed; reason: %q", reason)
	}
}

// TestToolStage_AllBlocked_CallsCheckExitConditions verifies that when all tool
// calls in the parallel path are blocked, ToolStage still runs checkExitConditions
// (so MaxToolCalls budget is enforced). Regression for early-return-nil bug.
func TestToolStage_AllBlocked_CallsCheckExitConditions(t *testing.T) {
	t.Parallel()

	// Allow-all deps but with MaxToolCalls = 1 and AuthorizeToolCall that blocks.
	denyAll := func(_ context.Context, _ *RunState, tc providers.ToolCall) (bool, string) {
		return false, "blocked: " + tc.Name
	}
	deps := &PipelineDeps{
		Config: PipelineConfig{MaxToolCalls: 1},
		AuthorizeToolCall: denyAll,
		ExecuteToolCall: func(_ context.Context, state *RunState, tc providers.ToolCall) ([]providers.Message, error) {
			return nil, nil
		},
		ExecuteToolRaw: func(_ context.Context, tc providers.ToolCall) (providers.Message, any, error) {
			return providers.Message{}, nil, nil
		},
		ProcessToolResult: func(_ context.Context, state *RunState, tc providers.ToolCall, rawMsg providers.Message, rawData any) []providers.Message {
			return nil
		},
	}

	stage := NewToolStage(deps)
	state := defaultState()
	state.Tool.AllowedTools = map[string]bool{} // non-nil allowlist → gate is active
	state.Tool.TotalToolCalls = 1               // already at MaxToolCalls

	// Two tool calls — parallel path is triggered (len > 1).
	setLastResponse(state, []providers.ToolCall{
		toolCallFor("exec"),
		toolCallFor("write_file"),
	})

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	// checkExitConditions must have fired: TotalToolCalls >= MaxToolCalls → BreakLoop.
	if stage.Result() != BreakLoop {
		t.Errorf("expected BreakLoop after all-blocked + MaxToolCalls reached, got %v", stage.Result())
	}
}
