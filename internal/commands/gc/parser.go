// Package gc implements the parser, registry, and executor for /gc: commands
// (plan, fix, cook, review, test, debug, docs, architect, uiux). The commands
// run natively through the agent loop — they are intercepted by Workstream B —
// while this package owns the command parser, skill registry, and
// system-prompt executor.
//
// Command kinds map one-to-one to skill slugs in the go-claw-engineer kit.
// The prefix is /gc: (case-insensitive); flags (--deep, --fast, --hard, --strict)
// are extracted from the input and kept in Command.Flags so downstream stages
// can tune execution.
package gc

import (
	"strings"
)

// gcPrefix is the /gc: command prefix, matched case-insensitively.
const gcPrefix = "/gc:"

// CommandKind identifies the recognized /gc: command kinds.
type CommandKind string

const (
	KindPlan      CommandKind = "plan"
	KindFix       CommandKind = "fix"
	KindCook      CommandKind = "cook"
	KindReview    CommandKind = "review"
	KindTest      CommandKind = "test"
	KindDebug     CommandKind = "debug"
	KindDocs      CommandKind = "docs"
	KindArchitect CommandKind = "architect"
	KindUIUX      CommandKind = "uiux"
	KindMission   CommandKind = "mission"
)

// knownKinds lists the recognized command kinds in a stable order.
var knownKinds = []CommandKind{
	KindPlan, KindFix, KindCook, KindReview,
	KindTest, KindDebug, KindDocs, KindArchitect, KindUIUX, KindMission,
}

// gcFlagSet is the set of flags extracted from the input. Flags are surfaced
// to the executor and downstream stages but are not interpreted by the parser.
var gcFlagSet = map[string]struct{}{
	"--deep":   {},
	"--fast":   {},
	"--hard":   {},
	"--strict": {},
}

// Command is a parsed /gc: command.
type Command struct {
	Kind  CommandKind
	Input string   // input after the command word and flags
	Flags []string // extracted flags, e.g. ["--deep", "--fast"]
}

// String returns the canonical /gc:<kind> spelling (lowercase).
func (k CommandKind) String() string {
	return string(k)
}

// Valid reports whether k is one of the recognized command kinds.
func (k CommandKind) Valid() bool {
	for _, known := range knownKinds {
		if k == known {
			return true
		}
	}
	return false
}

// Parse parses message as a /gc:<kind> command. Returns (cmd, true) when the
// message is a recognized /gc: command; (Command{}, false) for anything else
// (passthrough). The prefix and kind are matched case-insensitively; flags
// are extracted from the input and removed from it.
func Parse(message string) (Command, bool) {
	msg := strings.TrimSpace(message)
	if msg == "" || !strings.HasPrefix(strings.ToLower(msg), gcPrefix) {
		return Command{}, false
	}

	// The command word must start immediately after the colon (strict /gc:<cmd>).
	rest := msg[len(gcPrefix):]
	if rest == "" || rest[0] == ' ' || rest[0] == '\t' {
		return Command{}, false
	}
	after := strings.TrimSpace(rest)
	if after == "" {
		return Command{}, false
	}

	word, rest, _ := strings.Cut(after, " ")
	kind := CommandKind(strings.ToLower(strings.TrimSpace(word)))
	if !kind.Valid() {
		return Command{}, false
	}

	remaining := strings.TrimSpace(rest)
	flags, input := extractFlags(remaining)
	return Command{Kind: kind, Input: input, Flags: flags}, true
}

// extractFlags pulls known --flags out of the input and returns the remaining
// input with them removed. Flags may appear anywhere in the input. Unknown
// tokens that merely start with "--" are left in the input untouched.
func extractFlags(input string) ([]string, string) {
	if input == "" {
		return nil, ""
	}
	fields := strings.Fields(input)
	var flags []string
	var kept []string
	for _, f := range fields {
		if _, ok := gcFlagSet[f]; ok {
			flags = append(flags, f)
			continue
		}
		kept = append(kept, f)
	}
	return flags, strings.Join(kept, " ")
}
