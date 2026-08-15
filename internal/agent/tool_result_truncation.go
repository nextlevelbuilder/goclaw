package agent

import "regexp"

// importantTailRe matches keywords in the tail of output that indicate
// the tail contains important information (errors, summaries, results).
// Used by pruning.go's hasImportantTail() for tail-aware soft trim.
var importantTailRe = regexp.MustCompile(`(?i)(error|exception|failed|fatal|traceback|panic|stack trace|exit code|total|summary|result|complete|finished|done)\b`)
