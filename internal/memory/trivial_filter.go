package memory

import (
	"strings"
	"unicode/utf8"
)

// trivialStopwords are common filler words that don't carry search intent.
var trivialStopwords = map[string]bool{
	"hi": true, "hello": true, "hey": true, "ok": true, "okay": true,
	"yes": true, "no": true, "thanks": true, "thank": true, "you": true,
	"sure": true, "right": true, "got": true, "it": true, "the": true,
	"a": true, "an": true, "is": true, "are": true, "was": true, "i": true,
	"me": true, "my": true, "we": true, "do": true, "did": true, "please": true,
	"good": true, "great": true, "nice": true, "hmm": true, "ah": true,
	"oh": true, "um": true, "well": true, "so": true, "and": true,
	"but": true, "or": true, "that": true, "this": true,
}

// isTrivialMessage returns true if the message has fewer than 3 meaningful words.
// Skips memory injection for greetings, acknowledgments, and single-word responses.
func isTrivialMessage(msg string) bool {
	words := strings.Fields(strings.ToLower(msg))
	meaningful := 0
	for _, w := range words {
		w = strings.Trim(w, ".,!?;:'\"()-")
		if len(w) > 0 && !trivialStopwords[w] {
			meaningful++
			if meaningful >= 3 {
				return false
			}
		}
	}
	return true
}

// broadSignals are Vietnamese and English phrases indicating the user wants a
// cross-project or comprehensive listing. Matched case-insensitively.
var broadSignals = []string{
	// Vietnamese
	"tất cả", "toàn bộ", "all project", "mọi dự án", "tổng hợp",
	"liệt kê hết", "liệt kê tất", "across project", "mỗi project",
	"từng project", "các dự án", "các project", "every project",
	"all task", "all dự án", "toàn hệ thống",
	// English
	"all projects", "every project", "across all", "comprehensive",
	"full list", "everything", "all tasks", "all work",
	"cross-project", "cross project", "overview of all",
}

// isBroadQuery returns true when the user message looks like a multi-project or
// comprehensive listing request. Used to bump auto-inject limits and add a hint
// that encourages the LLM to call memory_search for completeness.
func isBroadQuery(msg string) bool {
	if utf8.RuneCountInString(msg) < 6 {
		return false
	}
	lower := strings.ToLower(msg)
	for _, signal := range broadSignals {
		if strings.Contains(lower, signal) {
			return true
		}
	}
	return false
}

// taskSignals are phrases indicating the user is asking about tasks, work items,
// deadlines, or project status — queries that need deep memory recall.
var taskSignals = []string{
	// Vietnamese
	"task", "công việc", "deadline", "tiến độ", "dự án",
	"liệt k��", "chi tiết", "action item", "việc cần làm",
	"status", "overdue", "bàn giao", "chốt", "meeting",
	"sprint", "backlog", "priority", "blocker",
	// English
	"tasks", "work items", "deliverables", "milestones",
	"progress", "assigned", "pending", "completed",
}

// isTaskQuery returns true when the message asks about tasks, deliverables,
// deadlines, or project status — queries where auto-injected memory alone is
// likely insufficient and explicit memory_search + vault_search should be called.
func isTaskQuery(msg string) bool {
	if utf8.RuneCountInString(msg) < 4 {
		return false
	}
	lower := strings.ToLower(msg)
	for _, signal := range taskSignals {
		if strings.Contains(lower, signal) {
			return true
		}
	}
	return false
}
