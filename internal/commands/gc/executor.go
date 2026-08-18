package gc

import (
	"context"
	"fmt"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/skills"
)

// CommandDispatcher resolves a /gc: command from a raw user message.
// Workstream B consumes this interface to intercept /gc:* messages inside the
// agent loop. Its signature is a fixed contract — do not change without
// coordinating with the agent-loop wiring.
type CommandDispatcher interface {
	// Resolve parses msg; returns a Dispatch when it is a recognized /gc: command,
	// or (nil, false) for passthrough.
	Resolve(ctx context.Context, msg string) (*Dispatch, bool)
}

// Dispatch is the resolved /gc: command: the parsed kind, the skill slug it
// maps to, the full SKILL.md content loaded via skills.Loader.LoadSkill, the
// input remaining after the command word and flags, and the extracted flags.
type Dispatch struct {
	Kind      CommandKind
	Skill     string   // skill slug
	Content   string   // full SKILL.md content loaded via skills.Loader.LoadSkill
	Remaining string   // input after the command word + flags
	Flags     []string // extracted --flags from the input
}

// Executor resolves /gc: commands to their kit skill and builds the
// system-prompt section that drives the agent loop.
type Executor struct {
	loader *skills.Loader
	reg    *Registry
}

// NewExecutor creates an executor backed by the given skills loader and
// command-to-skill registry. A nil loader or registry is tolerated: Resolve
// falls back to passthrough when the skill cannot be loaded.
func NewExecutor(loader *skills.Loader, reg *Registry) *Executor {
	return &Executor{loader: loader, reg: reg}
}

// Resolve parses msg as a /gc: command, looks up the mapped skill slug, and
// loads the skill content. Returns a Dispatch when the command is recognized
// and its skill content can be loaded; (nil, false) otherwise.
func (e *Executor) Resolve(ctx context.Context, msg string) (*Dispatch, bool) {
	cmd, ok := Parse(msg)
	if !ok {
		return nil, false
	}
	if e.reg == nil {
		return nil, false
	}
	slug, ok := e.reg.Lookup(cmd.Kind)
	if !ok || slug == "" {
		return nil, false
	}
	if e.loader == nil {
		return nil, false
	}
	content, ok := e.loader.LoadSkill(ctx, slug)
	if !ok {
		return nil, false
	}
	return &Dispatch{
		Kind:      cmd.Kind,
		Skill:     slug,
		Content:   content,
		Remaining: cmd.Input,
		Flags:     cmd.Flags,
	}, true
}

// BuildSystemPrompt builds the system-prompt section that instructs the agent
// to execute the resolved command using the skill workflow. It returns the
// skill content plus a short directive naming the command kind and requiring
// verification before completion is claimed.
func (e *Executor) BuildSystemPrompt(d *Dispatch) string {
	if d == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("## /gc: Command Execution\n\n")
	b.WriteString(fmt.Sprintf("You are executing /gc:%s. Follow the skill workflow below and its quality gates. Do not claim completion until verification passes.\n\n", d.Kind.String()))
	b.WriteString("### Skill: " + d.Skill + "\n\n")
	b.WriteString(d.Content)
	if len(d.Flags) > 0 {
		b.WriteString("\n\nExecution flags: " + strings.Join(d.Flags, " "))
	}
	return b.String()
}