// Package cliagent describes HOW to run a connected coding CLI (Claude Code,
// Codex, Aider, Gemini CLI, or an arbitrary "generic" binary) and how to read
// back what it produced.
//
// The point of the package is that an invocation is DATA, not code: every
// provider ships a built-in default Spec, and any part of it can be overridden
// per connection through the connection's `config` JSON
// (store.CLIConnection.Config). A CLI that renames a flag — or a default we got
// wrong — is therefore fixed by editing one connection's config, not by a code
// change and a redeploy.
//
// The package is deliberately self-contained (stdlib only, no store/sandbox
// imports) so it can be unit-tested and wired from anywhere.
package cliagent

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// OutputFormat says how the CLI's stdout should be parsed into normalised
// Events (see events.go), so one UI renders every provider's progress.
type OutputFormat string

const (
	// OutputClaudeStreamJSON is Claude Code's `--output-format stream-json`
	// (one Anthropic-shaped event per line, incl. token deltas).
	OutputClaudeStreamJSON OutputFormat = "claude_stream_json"
	// OutputJSONL is a generic "one JSON object per line" stream, parsed
	// best-effort.
	OutputJSONL OutputFormat = "jsonl"
	// OutputText is plain text: every non-empty line is narration.
	OutputText OutputFormat = "text"
)

// TaskPlaceholder is substituted with the delegated task inside Spec.TaskArgs.
const TaskPlaceholder = "{{task}}"

// Canonical provider identifiers. These match store.CLIConnection.Provider.
const (
	ProviderClaudeCode = "claude_code"
	ProviderCodex      = "codex"
	ProviderAider      = "aider"
	ProviderGeminiCLI  = "gemini_cli"
	ProviderGeneric    = "generic"
)

// ErrUnknownProvider is returned by Resolve when the provider has no built-in
// default and the connection config does not supply a full invocation.
var ErrUnknownProvider = errors.New("unknown CLI provider")

// Spec is the complete recipe for one run of a connected CLI: what to execute,
// with which arguments and environment, where its credential goes, and how to
// read its output.
//
// The final argv is Binary + TaskArgs(with TaskPlaceholder substituted) +
// ExtraArgs + the permission-mode args — see Spec.Command.
type Spec struct {
	Provider  string            // "claude_code" | "codex" | "aider" | "gemini_cli" | "generic"
	Binary    string            // executable name, resolved on the sandbox PATH
	TaskArgs  []string          // argv template; TaskPlaceholder is substituted with the task
	ExtraArgs []string          // appended verbatim (output-format / quiet flags …)
	CredEnv   []string          // env var names accepted for the secret, in PREFERENCE order
	Env       map[string]string // static env (e.g. HOME=/tmp for a read-only sandbox root)
	Output    OutputFormat

	// AutoApproveArgs / ManualApproveArgs: argv added for each permission mode.
	// Empty ManualApproveArgs means the provider cannot do manual approval —
	// callers must surface that honestly rather than silently running auto.
	AutoApproveArgs   []string
	ManualApproveArgs []string
}

// providerAliases maps the spellings seen in the wild (and in the legacy
// per-agent connected_agents JSONB) onto canonical provider ids.
var providerAliases = map[string]string{
	"claude":        ProviderClaudeCode,
	"claudecode":    ProviderClaudeCode,
	"claude-code":   ProviderClaudeCode,
	"claude_code":   ProviderClaudeCode,
	"codex":         ProviderCodex,
	"codex_cli":     ProviderCodex,
	"codex-cli":     ProviderCodex,
	"openai_codex":  ProviderCodex,
	"aider":         ProviderAider,
	"aider_cli":     ProviderAider,
	"gemini":        ProviderGeminiCLI,
	"gemini_cli":    ProviderGeminiCLI,
	"gemini-cli":    ProviderGeminiCLI,
	"google_gemini": ProviderGeminiCLI,
	"generic":       ProviderGeneric,
	"custom":        ProviderGeneric,
}

// CanonicalProvider normalises a provider string (case/spacing/alias) to one of
// the canonical ids. Unrecognised values are returned lower-cased and trimmed so
// error messages echo what the caller actually asked for.
func CanonicalProvider(provider string) string {
	p := strings.ToLower(strings.TrimSpace(provider))
	if c, ok := providerAliases[p]; ok {
		return c
	}
	return p
}

// sandboxBaseEnv is the env every CLI needs inside our sandbox: the container
// root is READ-ONLY, so anything that wants a config/cache dir must be pointed
// at the writable tmpfs. HOME=/tmp lets the CLI persist its own state between
// runs in a warm container, and the Go vars let a delegated `go build`/test loop
// work (the toolchain otherwise writes to ~/.cache and ~/go, both read-only).
// Verified in production for Claude Code; the same constraint applies to any CLI.
func sandboxBaseEnv() map[string]string {
	return map[string]string{
		"HOME":       "/tmp",
		"GOCACHE":    "/tmp/go-build",
		"GOPATH":     "/tmp/go",
		"GOMODCACHE": "/tmp/go/pkg/mod",
	}
}

// Defaults returns the built-in Spec per provider, freshly allocated on every
// call so a caller can mutate the result without corrupting anyone else's.
//
// IMPORTANT: except for claude_code these are BEST-KNOWN GUESSES, not verified
// invocations — see the per-provider comments. They exist so a connection works
// out of the box, and every field is overridable via the connection config, so a
// wrong flag is a config edit rather than a redeploy.
func Defaults() map[string]Spec {
	return map[string]Spec{
		// claude_code reproduces EXACTLY what internal/tools/delegate_external_tool.go
		// runCLI does today: headless (-p), stream-json progress events (which
		// requires --verbose), token-level deltas for word-by-word narration, and
		// bypassPermissions as the auto mode. Credential preference is
		// CLAUDE_CODE_OAUTH_TOKEN (subscription billing) before ANTHROPIC_API_KEY;
		// exactly one is injected because the CLI's own precedence is the reverse.
		// Note: `--bare` must NOT be added — bare mode ignores the OAuth token.
		ProviderClaudeCode: {
			Provider:        ProviderClaudeCode,
			Binary:          "claude",
			TaskArgs:        []string{"-p", TaskPlaceholder},
			ExtraArgs:       []string{"--output-format", "stream-json", "--verbose", "--include-partial-messages"},
			CredEnv:         []string{"CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_API_KEY"},
			Env:             sandboxBaseEnv(),
			Output:          OutputClaudeStreamJSON,
			AutoApproveArgs: []string{"--permission-mode", "bypassPermissions"},
			// `default` is the CLI's ask-before-acting mode. Headless runs have no
			// interactive channel, so this yields refusals rather than prompts until
			// an approval transport exists — kept non-empty because the flag is real
			// and overridable, e.g. to acceptEdits/plan.
			ManualApproveArgs: []string{"--permission-mode", "default"},
		},

		// codex — UNVERIFIED DEFAULTS. `codex exec` is the non-interactive mode and
		// `--json` is believed to emit one JSON object per line. The auto flag name
		// has changed across releases (--full-auto /
		// --dangerously-bypass-approvals-and-sandbox); bypassing Codex's own sandbox
		// is correct here because we already run inside our locked-down container.
		// ManualApproveArgs is intentionally EMPTY: non-interactive exec cannot
		// prompt, so manual mode must fail loudly instead of silently auto-approving.
		// Override any of this via the connection config.
		ProviderCodex: {
			Provider:          ProviderCodex,
			Binary:            "codex",
			TaskArgs:          []string{"exec", TaskPlaceholder},
			ExtraArgs:         []string{"--json"},
			CredEnv:           []string{"CODEX_API_KEY", "OPENAI_API_KEY"},
			Env:               sandboxBaseEnv(),
			Output:            OutputJSONL,
			AutoApproveArgs:   []string{"--dangerously-bypass-approvals-and-sandbox"},
			ManualApproveArgs: nil,
		},

		// aider — UNVERIFIED DEFAULTS. `--message` runs one non-interactive turn and
		// `--yes-always` auto-confirms; `--no-pretty` disables the TUI redraw so
		// stdout is line-oriented. Aider prints prose, not JSON, hence OutputText.
		// No manual-approval flag exists for a one-shot --message run.
		ProviderAider: {
			Provider:          ProviderAider,
			Binary:            "aider",
			TaskArgs:          []string{"--message", TaskPlaceholder},
			ExtraArgs:         []string{"--no-pretty"},
			CredEnv:           []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY"},
			Env:               sandboxBaseEnv(),
			Output:            OutputText,
			AutoApproveArgs:   []string{"--yes-always"},
			ManualApproveArgs: nil,
		},

		// gemini_cli — UNVERIFIED DEFAULTS. `-p` runs a single non-interactive
		// prompt and `--yolo` auto-approves tool calls. Output is prose today; if a
		// JSON stream mode is available on the installed version, set
		// {"extra_args":[...],"output":"jsonl"} on the connection.
		ProviderGeminiCLI: {
			Provider:          ProviderGeminiCLI,
			Binary:            "gemini",
			TaskArgs:          []string{"-p", TaskPlaceholder},
			ExtraArgs:         nil,
			CredEnv:           []string{"GEMINI_API_KEY", "GOOGLE_API_KEY"},
			Env:               sandboxBaseEnv(),
			Output:            OutputText,
			AutoApproveArgs:   []string{"--yolo"},
			ManualApproveArgs: nil,
		},

		// generic has NO defaults on purpose: it is the escape hatch for wiring an
		// arbitrary CLI, so the connection config must supply the whole contract
		// ({"binary":…,"task_args":["…","{{task}}"],"output":"text"} at minimum, plus
		// "env":{"HOME":"/tmp"} if the tool writes to a config dir).
		ProviderGeneric: {
			Provider: ProviderGeneric,
			Output:   "",
		},
	}
}

// KnownProviders lists the canonical providers that have built-in defaults,
// sorted, for use in error messages.
func KnownProviders() []string {
	d := Defaults()
	out := make([]string, 0, len(d))
	for k := range d {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// specOverride is the connection-config shape. Every field is a POINTER so the
// overlay can distinguish "absent" (keep the default) from "present and empty"
// (clear the default, e.g. "extra_args": [] to drop a flag we shouldn't send).
type specOverride struct {
	Binary    *string   `json:"binary"`
	TaskArgs  *[]string `json:"task_args"`
	ExtraArgs *[]string `json:"extra_args"`
	CredEnv   *[]string `json:"cred_env"`
	// Env MERGES over the default rather than replacing it, because the defaults
	// carry the writable-dir pointers a read-only sandbox needs; silently dropping
	// HOME=/tmp by setting one unrelated var would break the run. A key mapped to
	// JSON null is REMOVED.
	Env               *map[string]*string `json:"env"`
	Output            *string             `json:"output"`
	AutoApproveArgs   *[]string           `json:"auto_approve_args"`
	ManualApproveArgs *[]string           `json:"manual_approve_args"`
}

// Resolve builds the Spec for a connection: the provider's built-in default with
// the connection's config JSON overlaid, then validated.
//
// An unknown provider is an error UNLESS the config supplies the invocation
// itself — that way a brand-new CLI can be connected before this package knows
// its name.
func Resolve(provider string, cfg json.RawMessage) (Spec, error) {
	canonical := CanonicalProvider(provider)
	spec, known := Defaults()[canonical]
	if !known {
		if !hasConfig(cfg) {
			return Spec{}, fmt.Errorf("%w %q: known providers are %s — or set provider to %q and supply the invocation in the connection config",
				ErrUnknownProvider, canonical, strings.Join(KnownProviders(), ", "), ProviderGeneric)
		}
		// Unknown provider WITH config: treat it as a generic CLI fully described
		// by its config. Validation below still demands a usable invocation.
		spec = Spec{Provider: canonical}
	}

	if hasConfig(cfg) {
		var ov specOverride
		if err := json.Unmarshal(cfg, &ov); err != nil {
			return Spec{}, fmt.Errorf("cli provider %q: connection config is not valid JSON: %w", canonical, err)
		}
		spec.apply(ov)
	}

	if err := spec.Validate(); err != nil {
		return Spec{}, err
	}
	return spec, nil
}

// hasConfig reports whether cfg carries anything worth overlaying (non-empty and
// not JSON null / an empty object).
func hasConfig(cfg json.RawMessage) bool {
	s := strings.TrimSpace(string(cfg))
	return s != "" && s != "null" && s != "{}"
}

// apply overlays the present fields of ov onto s.
func (s *Spec) apply(ov specOverride) {
	if ov.Binary != nil {
		s.Binary = strings.TrimSpace(*ov.Binary)
	}
	if ov.TaskArgs != nil {
		s.TaskArgs = cloneStrings(*ov.TaskArgs)
	}
	if ov.ExtraArgs != nil {
		s.ExtraArgs = cloneStrings(*ov.ExtraArgs)
	}
	if ov.CredEnv != nil {
		s.CredEnv = cloneStrings(*ov.CredEnv)
	}
	if ov.AutoApproveArgs != nil {
		s.AutoApproveArgs = cloneStrings(*ov.AutoApproveArgs)
	}
	if ov.ManualApproveArgs != nil {
		s.ManualApproveArgs = cloneStrings(*ov.ManualApproveArgs)
	}
	if ov.Output != nil {
		s.Output = OutputFormat(strings.ToLower(strings.TrimSpace(*ov.Output)))
	}
	if ov.Env != nil {
		if s.Env == nil {
			s.Env = map[string]string{}
		}
		for k, v := range *ov.Env {
			k = strings.TrimSpace(k)
			if k == "" {
				continue
			}
			if v == nil { // explicit null → unset a default
				delete(s.Env, k)
				continue
			}
			s.Env[k] = *v
		}
	}
}

func cloneStrings(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

// Validate checks that a Spec can actually be run, with errors that name the
// config key to fix.
func (s Spec) Validate() error {
	who := s.Provider
	if who == "" {
		who = "(unset)"
	}
	if strings.TrimSpace(s.Binary) == "" {
		return fmt.Errorf("cli provider %q: no binary to run — the connection config must set %q (the executable name, e.g. {\"binary\":\"mycli\"})", who, "binary")
	}
	n := 0
	for _, a := range s.TaskArgs {
		n += strings.Count(a, TaskPlaceholder)
	}
	if n != 1 {
		return fmt.Errorf("cli provider %q: %q must contain exactly one %s placeholder (found %d) — e.g. {\"task_args\":[\"-p\",\"%s\"]}",
			who, "task_args", TaskPlaceholder, n, TaskPlaceholder)
	}
	switch s.Output {
	case OutputClaudeStreamJSON, OutputJSONL, OutputText:
	default:
		return fmt.Errorf("cli provider %q: unsupported %q %q — use one of %s, %s, %s",
			who, "output", string(s.Output), OutputClaudeStreamJSON, OutputJSONL, OutputText)
	}
	return nil
}
