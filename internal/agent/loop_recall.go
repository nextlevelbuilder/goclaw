package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// reFromPrefix matches channel-injected sender metadata: [From: @user (name)]
var reFromPrefix = regexp.MustCompile(`(?s)^\[From:[^\]]*\]\s*`)

// reBotMention matches Telegram/Discord bot mentions: @bot_username
var reBotMention = regexp.MustCompile(`@\S+bot\S*`)

// reReplyTag matches [Replying to ...] and [/Replying] metadata tags (keep content between them).
var reReplyTag = regexp.MustCompile(`(?m)^\[/?Replying[^\]]*\]\s*$`)

// reLeadingRecallFiller trims common chat preambles that hurt recall quality.
var reLeadingRecallFiller = regexp.MustCompile(`(?i)^\s*(?:alo(?:\s+em)?|hello|hi|hey|em\s+ơi|bot\s+ơi)\s*[-,:]?\s*`)

// reTrailingRecallFiller trims generic "check again" suffixes that add noise to recall queries.
var reTrailingRecallFiller = regexp.MustCompile(`(?i)\s*(?:check lại|xem lại|coi lại|kiểm tra lại)\s*[.!?]*\s*$`)

// cleanRecallQuery strips channel metadata, bot mentions, and reply tags from the user message
// to produce a cleaner search query for memory_search and knowledge_graph_search.
func cleanRecallQuery(msg string) string {
	q := reFromPrefix.ReplaceAllString(msg, "")
	q = reBotMention.ReplaceAllString(q, "")
	q = reReplyTag.ReplaceAllString(q, "")
	q = reLeadingRecallFiller.ReplaceAllString(q, "")
	q = reTrailingRecallFiller.ReplaceAllString(q, "")
	q = strings.TrimSpace(q)
	// Collapse multiple spaces and newlines left by stripping.
	for strings.Contains(q, "  ") {
		q = strings.ReplaceAll(q, "  ", " ")
	}
	for strings.Contains(q, "\n\n\n") {
		q = strings.ReplaceAll(q, "\n\n\n", "\n")
	}
	return strings.Trim(q, " \t\r\n-:,.")
}

// autoRecallMaxQuery is the maximum length of the user message used as a recall query.
const autoRecallMaxQuery = 200

// recallIntentType classifies the type of user query for optimized recall strategy.
type recallIntentType string

const (
	intentProjectOverview recallIntentType = "project_overview"
	intentRecentActivity  recallIntentType = "recent_activity"
	intentEntityLookup    recallIntentType = "entity_lookup"
	intentGeneral         recallIntentType = "general"
)

// recallIntent captures the detected intent and optional temporal/scope modifiers.
type recallIntent struct {
	Type     recallIntentType
	Temporal string // "today", "this_week", "recent", ""
	Scope    string // "active", "all"
}

// recallMeta holds metadata about the recall operation.
type recallMeta struct {
	intent          recallIntent
	projectCount    int
	projectNames    []string
	statusBreakdown map[string]int
	memoryHits      int
}

// memoryRecallPayload is the structured output from memory_search.
type memoryRecallPayload struct {
	Count        int      `json:"count"`
	HasResults   bool     `json:"has_results"`
	MatchedPaths []string `json:"matched_paths"`
}

// kgRecallPayload is the structured output from knowledge_graph_search.
type kgRecallPayload struct {
	Count           int            `json:"count"`
	HasResults      bool           `json:"has_results"`
	HasProjectMatch bool           `json:"has_project_match"`
	ProjectNames    []string       `json:"project_names"`
	StatusBreakdown map[string]int `json:"status_breakdown"`
	Summary         string         `json:"summary"`
	Entities        []struct {
		Name         string            `json:"name"`
		EntityType   string            `json:"entity_type"`
		Description  string            `json:"description"`
		Properties   map[string]string `json:"properties"`
		TopRelations []string          `json:"top_relations"`
	} `json:"entities"`
}

func parseMemoryRecallPayload(raw string) (memoryRecallPayload, bool) {
	var payload memoryRecallPayload
	if raw == "" {
		return payload, false
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return payload, false
	}
	return payload, true
}

func parseKGRecallPayload(raw string) (kgRecallPayload, bool) {
	var payload kgRecallPayload
	if raw == "" {
		return payload, false
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return payload, false
	}
	return payload, true
}

func memoryResultHasContent(result *tools.Result) bool {
	if result == nil || result.IsError || result.ForLLM == "" {
		return false
	}
	if payload, ok := parseMemoryRecallPayload(result.ForLLM); ok {
		return payload.HasResults
	}
	return !strings.HasPrefix(result.ForLLM, "No memory results found")
}

func kgResultHasContent(result *tools.Result) bool {
	if result == nil || result.IsError || result.ForLLM == "" {
		return false
	}
	if payload, ok := parseKGRecallPayload(result.ForLLM); ok {
		return payload.HasResults
	}
	return !strings.HasPrefix(result.ForLLM, "Knowledge graph is not enabled") &&
		!strings.HasPrefix(result.ForLLM, "No entities found")
}

// detectRecallIntent classifies the user query to determine optimal recall strategy.
func detectRecallIntent(query string) recallIntent {
	lower := strings.ToLower(strings.TrimSpace(query))
	if lower == "" {
		return recallIntent{Type: intentGeneral}
	}

	hasProjectTerm := containsAnyPhrase(lower, "dự án", "project", "projects")
	hasListIntent := containsAnyPhrase(lower,
		"đang có", "có những", "dự án nào", "liệt kê", "list",
		"tổng hợp", "overview", "tất cả", "hiện tại", "active",
		"bao nhiêu", "tình hình", "projects nào", "active projects")

	hasRecentTerm := containsAnyPhrase(lower,
		"hôm nay", "today", "mới", "recent", "gần đây",
		"updates", "what's new", "tiến độ", "progress",
		"có gì mới", "what changed")

	// Temporal detection
	temporal := ""
	if containsAnyPhrase(lower, "hôm nay", "today") {
		temporal = "today"
	} else if containsAnyPhrase(lower, "tuần này", "this week") {
		temporal = "this_week"
	} else if hasRecentTerm {
		temporal = "recent"
	}

	// Priority: project_overview > recent_activity > entity_lookup > general
	if hasProjectTerm && hasListIntent {
		return recallIntent{Type: intentProjectOverview, Temporal: temporal, Scope: "all"}
	}
	if hasRecentTerm {
		return recallIntent{Type: intentRecentActivity, Temporal: temporal}
	}

	// Entity lookup: look for potential entity names (words > 3 chars with uppercase)
	words := strings.FieldsSeq(query)
	for word := range words {
		if len(word) > 3 && strings.ContainsFunc(word, unicode.IsUpper) {
			return recallIntent{Type: intentEntityLookup}
		}
	}

	return recallIntent{Type: intentGeneral}
}

// buildTemporalQuery constructs a memory search query based on temporal scope.
func buildTemporalQuery(temporal string) string {
	switch temporal {
	case "today":
		return "hôm nay today updates progress tiến độ changes mới"
	case "this_week":
		return "tuần này this week updates tiến độ progress"
	default:
		return "mới nhất recent latest updates tiến độ"
	}
}

func containsAnyPhrase(s string, phrases ...string) bool {
	for _, phrase := range phrases {
		if strings.Contains(s, phrase) {
			return true
		}
	}
	return false
}

var reProjectCount = regexp.MustCompile(`(?i)(?:tổng cộng|có|đang có|hiện có|only|total)?\s*(\d+)\s*(?:dự án|projects)\b`)

// responseMentionsProjectInventory checks if the LLM response adequately reflects
// the project inventory (requires ≥3 project names or explicit count).
func responseMentionsProjectInventory(resp string, projectNames []string, projectCount int) bool {
	if resp == "" {
		return false
	}
	lower := strings.ToLower(resp)

	matches := 0
	for _, name := range projectNames {
		if name == "" {
			continue
		}
		if strings.Contains(lower, strings.ToLower(name)) {
			matches++
			if matches >= 3 {
				return true
			}
		}
	}

	if m := reProjectCount.FindStringSubmatch(lower); len(m) == 2 {
		if n, err := strconv.Atoi(m[1]); err == nil && n >= 2 {
			return true
		}
	}

	if projectCount >= 2 && strings.Contains(lower, "nhiều dự án") {
		return true
	}

	return false
}

// capitalizeFirst converts the first character to uppercase.
func capitalizeFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// formatProjectInventory formats the knowledge graph project inventory as a structured summary.
func formatProjectInventory(result *tools.Result) string {
	if result == nil || result.IsError || result.ForLLM == "" {
		return "No project inventory available.\n"
	}

	payload, ok := parseKGRecallPayload(result.ForLLM)
	if !ok {
		return result.ForLLM // fallback to raw output
	}

	var sb strings.Builder

	// Status breakdown header if available
	if len(payload.StatusBreakdown) > 0 {
		parts := make([]string, 0)
		for status, count := range payload.StatusBreakdown {
			parts = append(parts, fmt.Sprintf("%s: %d", status, count))
		}
		sort.Strings(parts)
		sb.WriteString(fmt.Sprintf("Status breakdown: %s\n\n", strings.Join(parts, ", ")))
	}

	// Group entities by status
	byStatus := make(map[string][]struct {
		name, desc, owner string
		rels              []string
	})
	for _, e := range payload.Entities {
		status := "unknown"
		owner := ""
		if e.Properties != nil {
			if s := e.Properties["status"]; s != "" {
				status = s
			}
			owner = e.Properties["owner"]
		}
		entry := struct {
			name, desc, owner string
			rels              []string
		}{e.Name, e.Description, owner, e.TopRelations}
		byStatus[status] = append(byStatus[status], entry)
	}

	// Print active first, then others
	statusOrder := []string{"active", "in_progress", "completed", "paused", "cancelled", "unknown"}
	for _, status := range statusOrder {
		entries, ok := byStatus[status]
		if !ok || len(entries) == 0 {
			continue
		}
		sb.WriteString(fmt.Sprintf("%s (%d):\n", capitalizeFirst(status), len(entries)))
		for _, e := range entries {
			sb.WriteString(fmt.Sprintf("  • %s", e.name))
			if e.owner != "" {
				sb.WriteString(fmt.Sprintf(" (owner: %s)", e.owner))
			}
			sb.WriteString("\n")
			if e.desc != "" {
				// Truncate long descriptions
				desc := e.desc
				if len(desc) > 120 {
					desc = desc[:117] + "..."
				}
				sb.WriteString(fmt.Sprintf("    %s\n", desc))
			}
			for _, rel := range e.rels {
				sb.WriteString(fmt.Sprintf("    → %s\n", rel))
			}
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// buildRecallContext assembles the context string based on detected intent.
func buildRecallContext(intent recallIntent, memResult, kgResult, projectInventoryResult *tools.Result) string {
	var sb strings.Builder

	switch intent.Type {
	case intentProjectOverview:
		sb.WriteString("[System: IMPORTANT — Authoritative data from memory and knowledge graph. Use as primary source of truth.]\n")
		sb.WriteString("\n=== PROJECT INVENTORY ===\n")
		if kgResultHasContent(projectInventoryResult) {
			sb.WriteString(formatProjectInventory(projectInventoryResult))
		} else {
			sb.WriteString("No project inventory available.\n")
		}

		if memoryResultHasContent(memResult) {
			sb.WriteString("\n=== RECENT CONTEXT FROM MEMORY ===\n")
			sb.WriteString(memResult.ForLLM)
			sb.WriteString("\n")
		} else {
			sb.WriteString("\n=== RECENT CONTEXT FROM MEMORY ===\n")
			sb.WriteString("No recent memory entries found.\n")
		}

		sb.WriteString("\nINSTRUCTIONS: You MUST list project names from the inventory. Mention the total count. If status properties exist, group by status (active/completed/etc). If status is unknown, say so.\n")

	case intentRecentActivity:
		sb.WriteString("[System: IMPORTANT — Authoritative data.]\n")
		sb.WriteString("\n=== ACTIVITY LOG ===\n")
		if memoryResultHasContent(memResult) {
			sb.WriteString(memResult.ForLLM)
			sb.WriteString("\n")
		} else {
			sb.WriteString("No recent activity found.\n")
		}

		if kgResultHasContent(kgResult) {
			sb.WriteString("\n=== RELATED ENTITIES ===\n")
			sb.WriteString(kgResult.ForLLM)
			sb.WriteString("\n")
		}

		sb.WriteString("\nINSTRUCTIONS: Summarize the activity from the log above. Reference entity names for context.\n")

	default: // intentGeneral, intentEntityLookup
		sb.WriteString("[System: IMPORTANT — Authoritative data from memory and knowledge graph.]\n")

		if memoryResultHasContent(memResult) {
			sb.WriteString("\n=== Memory Results ===\n")
			sb.WriteString(memResult.ForLLM)
			sb.WriteString("\n")
		}

		if kgResultHasContent(kgResult) {
			sb.WriteString("\n=== Knowledge Graph Results ===\n")
			sb.WriteString(kgResult.ForLLM)
			sb.WriteString("\n")
		}

		sb.WriteString("\nINSTRUCTIONS: Base your answer on the data above. Do not mention tool names.\n")
	}

	return sb.String()
}

// buildRecallValidationFeedback generates validation feedback for project_overview intent.
func buildRecallValidationFeedback(meta *recallMeta) string {
	if meta == nil {
		return ""
	}
	switch meta.intent.Type {
	case intentProjectOverview:
		if len(meta.projectNames) == 0 {
			return ""
		}
		limit := min(len(meta.projectNames), 15)
		list := strings.Join(meta.projectNames[:limit], ", ")
		count := meta.projectCount
		if count <= 0 {
			count = len(meta.projectNames)
		}
		var statusHint string
		if len(meta.statusBreakdown) > 0 {
			parts := make([]string, 0)
			for status, c := range meta.statusBreakdown {
				parts = append(parts, fmt.Sprintf("%s: %d", status, c))
			}
			sort.Strings(parts)
			statusHint = fmt.Sprintf(" Status breakdown: %s.", strings.Join(parts, ", "))
		}
		return fmt.Sprintf(
			"[System] Your answer did not reflect the project inventory.\n"+
				"There are %d projects.%s You MUST list at least 5 project names and mention the total count.\n"+
				"Project inventory: %s.\n"+
				"Group by status if available. Answer in the user's language. Do not mention internal tools.",
			count, statusHint, list,
		)
	default:
		return ""
	}
}

// autoRecall executes memory_search and knowledge_graph_search before the first LLM
// iteration and injects the results as an ephemeral "user" message. This ensures the
// LLM always has prior context regardless of model compliance with prompt instructions.
//
// The injected message is placed just before the last user message so the user's actual
// request remains the final message. Results are ephemeral — not persisted to session history.
func (l *Loop) autoRecall(ctx context.Context, userMessage string, messages []providers.Message) ([]providers.Message, *recallMeta) {
	if !l.hasMemory {
		return messages, nil
	}

	memTool, hasMemory := l.tools.Get("memory_search")
	if !hasMemory {
		return messages, nil
	}

	// Strip channel metadata ([From: ...]) and bot mentions before searching.
	query := cleanRecallQuery(userMessage)

	if isTrivialMessage(query) {
		return messages, nil
	}

	if utf8.RuneCountInString(query) > autoRecallMaxQuery {
		query = string([]rune(query)[:autoRecallMaxQuery])
	}

	kgTool, hasKG := l.tools.Get("knowledge_graph_search")

	// Detect intent to drive query strategy
	intent := detectRecallIntent(query)
	meta := &recallMeta{intent: intent}

	// Execute queries based on intent
	var memResult, kgResult, projectInventoryResult *tools.Result
	var wg sync.WaitGroup

	switch intent.Type {
	case intentProjectOverview:
		// Query 1: Project inventory
		if hasKG {
			wg.Go(func() {
				start := time.Now().UTC()
				args := map[string]any{"query": "*", "entity_type": "project"}
				spanID := l.emitToolSpanStart(ctx, start, "auto_recall:knowledge_graph_projects", "auto_recall_kg_projects", `{"query":"*","entity_type":"project"}`)
				projectInventoryResult = kgTool.Execute(ctx, args)
				l.emitToolSpanEnd(ctx, spanID, start, projectInventoryResult)
			})
		}
		// Query 2: Memory context for projects
		wg.Go(func() {
			start := time.Now().UTC()
			contextQuery := "dự án project status tiến độ"
			spanID := l.emitToolSpanStart(ctx, start, "auto_recall:memory_search", "auto_recall_mem", fmt.Sprintf(`{"query":%q}`, contextQuery))
			memResult = memTool.Execute(ctx, map[string]any{"query": contextQuery})
			l.emitToolSpanEnd(ctx, spanID, start, memResult)
		})

	case intentRecentActivity:
		// Query 1: Temporal memory search
		wg.Go(func() {
			start := time.Now().UTC()
			temporalQuery := buildTemporalQuery(intent.Temporal)
			spanID := l.emitToolSpanStart(ctx, start, "auto_recall:memory_search", "auto_recall_mem", fmt.Sprintf(`{"query":%q}`, temporalQuery))
			memResult = memTool.Execute(ctx, map[string]any{"query": temporalQuery})
			l.emitToolSpanEnd(ctx, spanID, start, memResult)
		})
		// Query 2: KG for related entities
		if hasKG {
			wg.Go(func() {
				start := time.Now().UTC()
				spanID := l.emitToolSpanStart(ctx, start, "auto_recall:knowledge_graph_search", "auto_recall_kg", fmt.Sprintf(`{"query":%q}`, query))
				kgResult = kgTool.Execute(ctx, map[string]any{"query": query})
				l.emitToolSpanEnd(ctx, spanID, start, kgResult)
			})
		}

	case intentEntityLookup:
		// Query 1: KG search
		if hasKG {
			wg.Go(func() {
				start := time.Now().UTC()
				spanID := l.emitToolSpanStart(ctx, start, "auto_recall:knowledge_graph_search", "auto_recall_kg", fmt.Sprintf(`{"query":%q}`, query))
				kgResult = kgTool.Execute(ctx, map[string]any{"query": query})
				l.emitToolSpanEnd(ctx, spanID, start, kgResult)
			})
		}
		// Query 2: Memory search
		wg.Go(func() {
			start := time.Now().UTC()
			spanID := l.emitToolSpanStart(ctx, start, "auto_recall:memory_search", "auto_recall_mem", fmt.Sprintf(`{"query":%q}`, query))
			memResult = memTool.Execute(ctx, map[string]any{"query": query})
			l.emitToolSpanEnd(ctx, spanID, start, memResult)
		})

	default: // intentGeneral
		// Query 1: Memory search
		wg.Go(func() {
			start := time.Now().UTC()
			spanID := l.emitToolSpanStart(ctx, start, "auto_recall:memory_search", "auto_recall_mem", fmt.Sprintf(`{"query":%q}`, query))
			memResult = memTool.Execute(ctx, map[string]any{"query": query})
			l.emitToolSpanEnd(ctx, spanID, start, memResult)
		})
		// Query 2: KG search
		if hasKG {
			wg.Go(func() {
				start := time.Now().UTC()
				spanID := l.emitToolSpanStart(ctx, start, "auto_recall:knowledge_graph_search", "auto_recall_kg", fmt.Sprintf(`{"query":%q}`, query))
				kgResult = kgTool.Execute(ctx, map[string]any{"query": query})
				l.emitToolSpanEnd(ctx, spanID, start, kgResult)
			})
		}
	}

	wg.Wait()

	// Parse project inventory metadata
	if projectInventoryResult != nil && !projectInventoryResult.IsError {
		var inventoryPayload struct {
			Count           int            `json:"count"`
			ProjectNames    []string       `json:"project_names"`
			StatusBreakdown map[string]int `json:"status_breakdown"`
			Entities        []struct {
				Name       string `json:"name"`
				EntityType string `json:"entity_type"`
			} `json:"entities"`
		}
		if err := json.Unmarshal([]byte(projectInventoryResult.ForLLM), &inventoryPayload); err == nil {
			meta.projectCount = inventoryPayload.Count
			meta.statusBreakdown = inventoryPayload.StatusBreakdown
			if len(inventoryPayload.ProjectNames) > 0 {
				meta.projectNames = inventoryPayload.ProjectNames
			} else {
				for _, e := range inventoryPayload.Entities {
					if e.EntityType == "project" && e.Name != "" {
						meta.projectNames = append(meta.projectNames, e.Name)
					}
				}
			}
		}
		if meta.projectCount == 0 {
			meta.projectCount = len(meta.projectNames)
		}
	}

	// Count memory hits
	if memResult != nil && !memResult.IsError {
		if payload, ok := parseMemoryRecallPayload(memResult.ForLLM); ok {
			meta.memoryHits = payload.Count
		}
	}

	// Build the recall context message based on intent
	contextContent := buildRecallContext(intent, memResult, kgResult, projectInventoryResult)

	// Check if we have any content
	hasContent := memoryResultHasContent(memResult) ||
		kgResultHasContent(kgResult) ||
		kgResultHasContent(projectInventoryResult)

	if !hasContent {
		slog.Debug("auto-recall: no results", "query", query, "intent", intent.Type)
		return messages, meta
	}

	// Build an ack message that summarizes what was found
	ackContent := buildRecallAck(memResult, kgResult, projectInventoryResult)

	// Insert the recall message just before the last user message.
	recallMsg := providers.Message{
		Role:    "user",
		Content: contextContent,
	}

	// Find the last user message and insert before it.
	lastUserIdx := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			lastUserIdx = i
			break
		}
	}

	if lastUserIdx < 0 {
		// No user message found — append recall then return.
		return append(messages, recallMsg), meta
	}

	// Insert before last user message: [...prefix, recallMsg, assistantAck, userMsg]
	// We need an assistant message between two user messages to maintain valid alternation.
	ackMsg := providers.Message{
		Role:    "assistant",
		Content: ackContent,
	}

	result := make([]providers.Message, 0, len(messages)+2)
	result = append(result, messages[:lastUserIdx]...)
	result = append(result, recallMsg, ackMsg)
	result = append(result, messages[lastUserIdx:]...)
	return result, meta
}

// trivialMessages is the set of messages that don't need memory recall.
var trivialMessages = map[string]bool{
	"ok": true, "ок": true, "oke": true, "okie": true,
	"hi": true, "hello": true, "hey": true, "hola": true,
	"thanks": true, "thank you": true, "thx": true, "ty": true,
	"cảm ơn": true, "cám ơn": true, "cam on": true,
	"chào": true, "xin chào": true,
	"vâng": true, "ừ": true, "uh": true, "uhm": true,
	"yep": true, "yes": true, "yeah": true, "yea": true,
	"no": true, "nope": true, "không": true,
	"bye": true, "goodbye": true, "tạm biệt": true,
	"good": true, "great": true, "nice": true, "cool": true,
	"👍": true, "👌": true, "🙏": true, "❤️": true, "🤝": true,
	"got it": true, "understood": true, "roger": true,
	"k": true, "kk": true, "lol": true, "haha": true,
}

// buildRecallAck builds an assistant-role acknowledgment that summarizes what
// the recall found. By placing key facts in an assistant message, the model
// treats them as its own prior knowledge — countering anchoring bias from
// earlier conversation turns.
func buildRecallAck(memResult *tools.Result, kgResults ...*tools.Result) string {
	var parts []string

	if memoryResultHasContent(memResult) {
		if payload, ok := parseMemoryRecallPayload(memResult.ForLLM); ok && len(payload.MatchedPaths) > 0 {
			parts = append(parts, fmt.Sprintf("memory files with relevant data (%s)", strings.Join(payload.MatchedPaths[:min(len(payload.MatchedPaths), 3)], ", ")))
		} else {
			parts = append(parts, "memory files with relevant data")
		}
	}

	seen := make(map[string]struct{}, 6)
	var entities []string
	for _, kgResult := range kgResults {
		if !kgResultHasContent(kgResult) {
			continue
		}
		for _, name := range extractEntityNames(kgResult.ForLLM) {
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			entities = append(entities, name)
			if len(entities) >= 5 {
				break
			}
		}
		if len(entities) >= 5 {
			break
		}
	}
	if len(entities) > 0 {
		parts = append(parts, fmt.Sprintf("knowledge graph entities: %s", strings.Join(entities, ", ")))
	} else if len(kgResults) > 0 {
		if slices.ContainsFunc(kgResults, kgResultHasContent) {
			parts = append(parts, "knowledge graph entities")
		}
	}

	if len(parts) == 0 {
		return "I've reviewed the pre-loaded context."
	}

	return fmt.Sprintf("I have relevant data from my recall: %s. I will use this to answer accurately.", strings.Join(parts, "; "))
}

// extractEntityNames pulls entity display names from KG search output.
// Expects lines like "- Entity Name [type] (id: ...)"
func extractEntityNames(kgOutput string) []string {
	if payload, ok := parseKGRecallPayload(kgOutput); ok {
		if len(payload.ProjectNames) > 0 {
			return payload.ProjectNames[:min(len(payload.ProjectNames), 5)]
		}
		names := make([]string, 0, min(len(payload.Entities), 5))
		for _, entity := range payload.Entities {
			if entity.Name == "" {
				continue
			}
			names = append(names, entity.Name)
			if len(names) >= 5 {
				break
			}
		}
		if len(names) > 0 {
			return names
		}
	}

	var names []string
	for line := range strings.SplitSeq(kgOutput, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		line = line[2:]
		// Find the [type] bracket — name is everything before it.
		if idx := strings.Index(line, " ["); idx > 0 {
			name := strings.TrimSpace(line[:idx])
			if name != "" && len(names) < 5 {
				names = append(names, name)
			}
		}
	}
	return names
}

// isTrivialMessage returns true if the message is a greeting, acknowledgment,
// or short response that doesn't need memory recall context.
func isTrivialMessage(msg string) bool {
	s := strings.TrimSpace(msg)
	if s == "" {
		return true
	}
	lower := strings.ToLower(s)
	if trivialMessages[lower] {
		return true
	}
	// Very short messages (≤3 runes) are typically trivial.
	if utf8.RuneCountInString(s) <= 3 {
		return true
	}
	return false
}
