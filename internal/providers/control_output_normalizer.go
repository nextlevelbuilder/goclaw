package providers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
)

const (
	kimiToolSectionBegin = "<|tool_calls_section_begin|>"
	kimiToolSectionEnd   = "<|tool_calls_section_end|>"
	kimiToolCallBegin    = "<|tool_call_begin|>"
	kimiToolArgsBegin    = "<|tool_call_argument_begin|>"
	kimiToolCallEnd      = "<|tool_call_end|>"
)

type controlOutput struct {
	Content   string
	Thinking  string
	ToolCalls []ToolCall
}

type controlOutputNormalizer struct {
	tools   []ToolDefinition
	pending string
}

func newControlOutputNormalizer(tools []ToolDefinition) *controlOutputNormalizer {
	return &controlOutputNormalizer{tools: tools}
}

func (n *controlOutputNormalizer) Append(delta string) controlOutput {
	n.pending += delta
	return n.drain(false)
}

func (n *controlOutputNormalizer) Finish() controlOutput {
	return n.drain(true)
}

func (n *controlOutputNormalizer) drain(final bool) controlOutput {
	var out controlOutput
	for n.pending != "" {
		cut, wait := safeControlCut(n.pending, final)
		if wait {
			break
		}
		if cut == 0 {
			break
		}
		part := n.pending[:cut]
		n.pending = n.pending[cut:]
		normalized := normalizeControlOutput(part, "", n.tools)
		out.Content += normalized.Content
		out.Thinking = joinThinking(out.Thinking, normalized.Thinking)
		out.ToolCalls = append(out.ToolCalls, normalized.ToolCalls...)
	}
	return out
}

func normalizeControlOutput(content, thinking string, tools []ToolDefinition) controlOutput {
	out := controlOutput{Content: content, Thinking: thinking}
	out.Content, out.ToolCalls = extractKimiTextToolCalls(out.Content, tools)

	var extracted string
	out.Content, extracted = extractThinkingTags(out.Content)
	out.Thinking = joinThinking(out.Thinking, extracted)

	return out
}

func safeControlCut(text string, final bool) (int, bool) {
	if final {
		return len(text), false
	}

	start := firstControlStart(text)
	if start >= 0 {
		if start > 0 {
			return start, false
		}
		if end := completeControlEnd(text); end > 0 {
			return end, false
		}
		return 0, true
	}

	keep := controlPrefixSuffixLen(text)
	if keep > 0 {
		if keep == len(text) {
			return 0, true
		}
		return len(text) - keep, false
	}
	return len(text), false
}

func firstControlStart(text string) int {
	start := strings.Index(text, kimiToolSectionBegin)
	lower := strings.ToLower(text)
	for _, token := range thinkingStartTokens {
		if idx := strings.Index(lower, token); idx >= 0 && (start < 0 || idx < start) {
			start = idx
		}
	}
	return start
}

func completeControlEnd(text string) int {
	if strings.HasPrefix(text, kimiToolSectionBegin) {
		if idx := strings.Index(text, kimiToolSectionEnd); idx >= 0 {
			return idx + len(kimiToolSectionEnd)
		}
		return 0
	}

	lower := strings.ToLower(text)
	for _, tag := range thinkingTags {
		open := "<" + tag
		if strings.HasPrefix(lower, open) {
			closeStart := strings.Index(lower, "</"+tag)
			if closeStart < 0 {
				return 0
			}
			closeEnd := strings.Index(lower[closeStart:], ">")
			if closeEnd < 0 {
				return 0
			}
			return closeStart + closeEnd + 1
		}
	}
	return 0
}

func controlPrefixSuffixLen(text string) int {
	lower := strings.ToLower(text)
	maxKeep := 0
	for _, token := range controlStartTokens {
		candidate := text
		if strings.HasPrefix(token, "<|") {
			candidate = text
		} else {
			candidate = lower
		}
		limit := len(token) - 1
		if len(candidate) < limit {
			limit = len(candidate)
		}
		for n := limit; n > maxKeep; n-- {
			if strings.HasPrefix(token, candidate[len(candidate)-n:]) {
				maxKeep = n
				break
			}
		}
	}
	return maxKeep
}

var (
	thinkingTags        = []string{"redacted_thinking", "antthinking", "thinking", "thought", "think"}
	thinkingStartTokens = []string{"<redacted_thinking", "<antthinking", "<thinking", "<thought", "<think"}
	controlStartTokens  = append([]string{kimiToolSectionBegin}, thinkingStartTokens...)
)

var (
	kimiToolSectionRe = regexp.MustCompile(`(?s)<\|tool_calls_section_begin\|>(.*?)<\|tool_calls_section_end\|>`)
	kimiToolCallRe    = regexp.MustCompile(`(?s)<\|tool_call_begin\|>(.*?)<\|tool_call_argument_begin\|>(.*?)<\|tool_call_end\|>`)
	thinkingTagRes    = []*regexp.Regexp{
		regexp.MustCompile(`(?is)<redacted_thinking\b[^>]*>(.*?)</redacted_thinking\s*>`),
		regexp.MustCompile(`(?is)<antthinking\b[^>]*>(.*?)</antthinking\s*>`),
		regexp.MustCompile(`(?is)<thinking\b[^>]*>(.*?)</thinking\s*>`),
		regexp.MustCompile(`(?is)<thought\b[^>]*>(.*?)</thought\s*>`),
		regexp.MustCompile(`(?is)<think\b[^>]*>(.*?)</think\s*>`),
	}
)

func extractKimiTextToolCalls(content string, tools []ToolDefinition) (string, []ToolCall) {
	if !strings.Contains(content, kimiToolSectionBegin) {
		return content, nil
	}

	var calls []ToolCall
	cleaned := kimiToolSectionRe.ReplaceAllStringFunc(content, func(section string) string {
		matches := kimiToolCallRe.FindAllStringSubmatch(section, -1)
		for _, match := range matches {
			call, ok := parseKimiTextToolCall(match[1], match[2], tools, len(calls))
			if ok {
				calls = append(calls, call)
			}
		}
		return ""
	})
	return cleaned, calls
}

func parseKimiTextToolCall(rawHeader, rawArgs string, tools []ToolDefinition, index int) (ToolCall, bool) {
	args := make(map[string]any)
	var parseErr string
	trimmedArgs := strings.TrimSpace(rawArgs)
	if err := json.Unmarshal([]byte(trimmedArgs), &args); err != nil && trimmedArgs != "" {
		parseErr = fmt.Sprintf("malformed JSON (%d chars): %v", len(rawArgs), err)
	}

	name, ok := resolveTextToolName(rawHeader, args, parseErr == "", tools)
	if !ok {
		slog.Warn("provider_control: text tool call stripped without deterministic tool match",
			"header", strings.TrimSpace(rawHeader), "tools", len(functionToolNames(tools)))
		return ToolCall{}, false
	}

	if parseErr != "" {
		slog.Warn("provider_control: failed to parse text tool call arguments",
			"tool", name, "raw_len", len(rawArgs), "error", parseErr)
	}

	id := textToolCallID(rawHeader, index)
	return ToolCall{
		ID:         truncateToolCallID(id),
		Name:       name,
		Arguments:  args,
		ParseError: parseErr,
	}, true
}

func resolveTextToolName(rawHeader string, args map[string]any, argsValid bool, tools []ToolDefinition) (string, bool) {
	header := strings.TrimSpace(rawHeader)
	names := functionToolNames(tools)
	for _, name := range names {
		if header == name {
			return name, true
		}
	}
	if len(names) == 1 {
		return names[0], true
	}
	if argsValid {
		matches := matchingToolNamesByArgs(args, tools)
		if len(matches) == 1 {
			return matches[0], true
		}
	}
	return "", false
}

func functionToolNames(tools []ToolDefinition) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		if tool.Type == "function" && tool.Function != nil && strings.TrimSpace(tool.Function.Name) != "" {
			names = append(names, strings.TrimSpace(tool.Function.Name))
		}
	}
	return names
}

func matchingToolNamesByArgs(args map[string]any, tools []ToolDefinition) []string {
	if len(args) == 0 {
		return nil
	}
	matches := make([]string, 0, 1)
	for _, tool := range tools {
		if tool.Type != "function" || tool.Function == nil || strings.TrimSpace(tool.Function.Name) == "" {
			continue
		}
		if toolSchemaMatchesArgs(tool.Function.Parameters, args) {
			matches = append(matches, strings.TrimSpace(tool.Function.Name))
		}
	}
	return matches
}

func toolSchemaMatchesArgs(parameters map[string]any, args map[string]any) bool {
	required := schemaStringList(parameters["required"])
	if len(required) == 0 {
		return false
	}
	properties := schemaProperties(parameters["properties"])
	if len(properties) == 0 {
		return false
	}
	for _, key := range required {
		if _, ok := args[key]; !ok {
			return false
		}
	}
	for key := range args {
		if _, ok := properties[key]; !ok {
			return false
		}
	}
	return true
}

func schemaStringList(value any) []string {
	switch v := value.(type) {
	case []string:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if item = strings.TrimSpace(item); item != "" {
				out = append(out, item)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				if s = strings.TrimSpace(s); s != "" {
					out = append(out, s)
				}
			}
		}
		return out
	default:
		return nil
	}
}

func schemaProperties(value any) map[string]struct{} {
	props := make(map[string]struct{})
	switch v := value.(type) {
	case map[string]any:
		for key := range v {
			if key = strings.TrimSpace(key); key != "" {
				props[key] = struct{}{}
			}
		}
	}
	return props
}

func textToolCallID(rawHeader string, index int) string {
	header := strings.TrimSpace(rawHeader)
	if strings.HasPrefix(header, "call_") {
		return header
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", header, index)))
	return "call_" + hex.EncodeToString(sum[:])[:16]
}

func extractThinkingTags(content string) (string, string) {
	lower := strings.ToLower(content)
	if !strings.Contains(lower, "<think") &&
		!strings.Contains(lower, "<thought") &&
		!strings.Contains(lower, "<antthinking") &&
		!strings.Contains(lower, "<redacted_thinking") {
		return content, ""
	}

	var thinkingParts []string
	cleaned := content
	for _, re := range thinkingTagRes {
		matches := re.FindAllStringSubmatch(cleaned, -1)
		for _, match := range matches {
			if len(match) > 1 && strings.TrimSpace(match[1]) != "" {
				thinkingParts = append(thinkingParts, strings.TrimSpace(match[1]))
			}
		}
		cleaned = re.ReplaceAllString(cleaned, "")
	}

	cleaned, dangling := extractDanglingThinkingTag(cleaned)
	if dangling != "" {
		thinkingParts = append(thinkingParts, dangling)
	}

	return cleaned, strings.Join(thinkingParts, "\n")
}

func extractDanglingThinkingTag(content string) (string, string) {
	lower := strings.ToLower(content)
	idx := -1
	for _, token := range thinkingStartTokens {
		if found := strings.Index(lower, token); found >= 0 && (idx < 0 || found < idx) {
			idx = found
		}
	}
	if idx < 0 {
		return content, ""
	}
	return content[:idx], strings.TrimSpace(content[idx:])
}

func joinThinking(existing, added string) string {
	existing = strings.TrimSpace(existing)
	added = strings.TrimSpace(added)
	switch {
	case existing == "":
		return added
	case added == "":
		return existing
	default:
		return existing + "\n" + added
	}
}
