package agent

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/pipeline"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

func TestClassifySelfKnowledgeExplainIntent(t *testing.T) {
	t.Parallel()

	if !classifySelfKnowledgeExplainIntent("bạn xem cách design hệ thống của bạn có gì hay và nổi trội để hướng dẫn giải thích cho tôi hiểu") {
		t.Fatal("expected self-knowledge explain intent for architecture question")
	}
	if classifySelfKnowledgeExplainIntent("đọc repo này rồi check log production và inspect code giúp tôi") {
		t.Fatal("expected repo-inspection request to bypass self-knowledge explain intent")
	}
}

func TestApplySelfKnowledgeToolPolicy_FiltersRepoToolsAndInjectsHint(t *testing.T) {
	t.Parallel()

	state := &pipeline.RunState{
		Input: &pipeline.RunInput{SelfKnowledgeExplain: true},
	}
	defs := []providers.ToolDefinition{
		{Type: "function", Function: providers.ToolFunctionSchema{Name: "list_files"}},
		{Type: "function", Function: providers.ToolFunctionSchema{Name: "memory_search"}},
		{Type: "function", Function: providers.ToolFunctionSchema{Name: "vault_search"}},
		{Type: "function", Function: providers.ToolFunctionSchema{Name: "exec"}},
	}

	filtered, msg := applySelfKnowledgeToolPolicy(state, defs)
	if msg == nil {
		t.Fatal("expected self-knowledge hint message")
	}
	if len(filtered) != 2 {
		t.Fatalf("filtered len = %d, want 2", len(filtered))
	}
	for _, td := range filtered {
		if td.Function.Name == "list_files" || td.Function.Name == "exec" {
			t.Fatalf("unexpected repo tool %q after filtering", td.Function.Name)
		}
	}
}

func TestApplySelfKnowledgeToolPolicy_DirectAnswerOnlyStripsAllTools(t *testing.T) {
	t.Parallel()

	state := &pipeline.RunState{
		Input: &pipeline.RunInput{SelfKnowledgeExplain: true},
	}
	state.Tool.DirectAnswerOnly = true
	defs := []providers.ToolDefinition{
		{Type: "function", Function: providers.ToolFunctionSchema{Name: "memory_search"}},
	}

	filtered, msg := applySelfKnowledgeToolPolicy(state, defs)
	if msg != nil {
		t.Fatal("expected no extra hint when already in direct-answer mode")
	}
	if len(filtered) != 0 {
		t.Fatalf("filtered len = %d, want 0", len(filtered))
	}
}

func TestMaybeArmSelfKnowledgeDirectAnswer_OnRetrievalMiss(t *testing.T) {
	t.Parallel()

	loop := &Loop{id: "coder"}
	state := &pipeline.RunState{
		Input: &pipeline.RunInput{SelfKnowledgeExplain: true},
	}

	msg := loop.maybeArmSelfKnowledgeDirectAnswer(state, "memory_search", &tools.Result{
		ForLLM: "No memory results found for query: system design architecture",
	})
	if msg != nil {
		t.Fatal("expected first miss to defer direct-answer recovery")
	}
	if state.Tool.DirectAnswerOnly {
		t.Fatal("direct-answer mode should not arm on first narrow retrieval miss")
	}

	msg = loop.maybeArmSelfKnowledgeDirectAnswer(state, "vault_search", &tools.Result{
		ForLLM: "No results found. Try memory_search for memory-specific queries or kg_search for relationship traversal.",
	})
	if msg == nil {
		t.Fatal("expected recovery message after unified retrieval miss")
	}
	if !state.Tool.DirectAnswerOnly {
		t.Fatal("expected direct-answer mode to arm after retrieval miss")
	}
}

func TestMaybeArmSelfKnowledgeDirectAnswer_IgnoresNonSelfKnowledgeRuns(t *testing.T) {
	t.Parallel()

	loop := &Loop{id: "coder"}
	state := &pipeline.RunState{
		Input: &pipeline.RunInput{SelfKnowledgeExplain: false},
	}
	msg := loop.maybeArmSelfKnowledgeDirectAnswer(state, "vault_search", &tools.Result{
		ForLLM: "No results found. Try memory_search for memory-specific queries or kg_search for relationship traversal.",
	})
	if msg != nil {
		t.Fatal("expected nil recovery message for normal runs")
	}
	if state.Tool.DirectAnswerOnly {
		t.Fatal("expected normal runs to keep tools available")
	}
}
