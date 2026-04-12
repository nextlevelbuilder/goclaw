package agent

import (
	"log/slog"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/pipeline"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

var selfKnowledgeIntentWords = []string{
	"architecture", "kiến trúc", "design", "thiết kế", "system", "hệ thống",
	"capabilities", "khả năng", "how you work", "cách bạn", "cách hoạt động",
	"workflow", "nổi trội", "điểm hay", "permissions", "quyền gì",
}

var selfKnowledgeActorWords = []string{
	"your", "you", "bạn", "của bạn", "agent này", "agent nay", "goclaw", "codex",
}

var selfKnowledgeExplainWords = []string{
	"explain", "giải thích", "describe", "mô tả", "walk through", "hướng dẫn", "làm rõ",
}

var explicitRepoInspectionWords = []string{
	"repo", "codebase", "source code", "mã nguồn", "xem code", "inspect code",
	"đọc file", "read file", "file", "files", "log", "trace", "session", "production",
}

var selfKnowledgeRepoTools = map[string]bool{
	"list_files": true,
	"read_file":  true,
	"search":     true,
	"glob":       true,
	"exec":       true,
	"bash":       true,
}

const selfKnowledgeFirstTurnInstruction = "[System] This request is asking about your own architecture, capabilities, or how you work. Answer from your built-in system context first. Use semantic recall tools only if they can add grounding. Do not inspect repo files or run shell commands unless the user explicitly asked for file/code evidence."

const selfKnowledgeDirectAnswerRecoveryInstruction = "[System] Semantic retrieval found no relevant records for this self-knowledge request. Stop searching and answer directly from your built-in system context plus the current conversation. Do not call more tools unless the user explicitly asks for code or file evidence."

func classifySelfKnowledgeExplainIntent(message string) bool {
	lower := normalizeSelfKnowledgeText(message)
	if lower == "" {
		return false
	}
	if containsAny(lower, explicitRepoInspectionWords) {
		return false
	}
	if !containsAny(lower, selfKnowledgeActorWords) {
		return false
	}
	if containsAny(lower, selfKnowledgeIntentWords) {
		return true
	}
	return containsAny(lower, selfKnowledgeExplainWords)
}

func normalizeSelfKnowledgeText(message string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(message))), " ")
}

func containsAny(text string, terms []string) bool {
	for _, term := range terms {
		if strings.Contains(text, term) {
			return true
		}
	}
	return false
}

func filterSelfKnowledgeRepoTools(toolDefs []providers.ToolDefinition) []providers.ToolDefinition {
	if len(toolDefs) == 0 {
		return nil
	}
	filtered := make([]providers.ToolDefinition, 0, len(toolDefs))
	for _, td := range toolDefs {
		if selfKnowledgeRepoTools[td.Function.Name] {
			continue
		}
		filtered = append(filtered, td)
	}
	return filtered
}

func applySelfKnowledgeToolPolicy(state *pipeline.RunState, toolDefs []providers.ToolDefinition) ([]providers.ToolDefinition, *providers.Message) {
	if state == nil || state.Input == nil || !state.Input.SelfKnowledgeExplain {
		return toolDefs, nil
	}
	if state.Tool.DirectAnswerOnly {
		return nil, nil
	}
	filtered := filterSelfKnowledgeRepoTools(toolDefs)
	if state.Tool.SelfKnowledgeHintInjected {
		return filtered, nil
	}
	state.Tool.SelfKnowledgeHintInjected = true
	return filtered, &providers.Message{
		Role:    "system",
		Content: selfKnowledgeFirstTurnInstruction,
	}
}

func isSelfKnowledgeRetrievalTool(toolName string) bool {
	switch toolName {
	case "memory_search", "vault_search", "knowledge_graph_search":
		return true
	default:
		return false
	}
}

func isSelfKnowledgeRetrievalMiss(toolName, content string) bool {
	lower := strings.ToLower(strings.TrimSpace(content))
	switch toolName {
	case "memory_search":
		return strings.HasPrefix(lower, "no memory results found")
	case "vault_search":
		return strings.HasPrefix(lower, "no results found.")
	case "knowledge_graph_search":
		return strings.HasPrefix(lower, "knowledge graph is empty.") ||
			strings.HasPrefix(lower, "no entities found matching") ||
			strings.HasPrefix(lower, "no entities of type ") ||
			strings.HasPrefix(lower, "no connected entities found")
	default:
		return false
	}
}

func (l *Loop) maybeArmSelfKnowledgeDirectAnswer(state *pipeline.RunState, toolName string, result *tools.Result) *providers.Message {
	if state == nil || state.Input == nil || result == nil || result.IsError {
		return nil
	}
	if !state.Input.SelfKnowledgeExplain || state.Tool.DirectAnswerOnly {
		return nil
	}
	if !isSelfKnowledgeRetrievalTool(toolName) {
		return nil
	}
	if isSelfKnowledgeRetrievalMiss(toolName, result.ForLLM) {
		state.Tool.RetrievalMisses++
		if state.Tool.RetrievalHits == 0 && (toolName == "vault_search" || state.Tool.RetrievalMisses >= 2) {
			state.Tool.DirectAnswerOnly = true
			slog.Info("self-knowledge direct-answer recovery armed",
				"agent", l.id,
				"tool", toolName,
				"misses", state.Tool.RetrievalMisses)
			return &providers.Message{
				Role:    "system",
				Content: selfKnowledgeDirectAnswerRecoveryInstruction,
			}
		}
		return nil
	}
	state.Tool.RetrievalHits++
	return nil
}
