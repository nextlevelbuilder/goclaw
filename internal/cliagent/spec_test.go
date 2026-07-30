package cliagent

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// TestDefaultsAreValid guards the invariant every caller relies on: a connection
// with no config at all must still be runnable — except "generic", which exists
// precisely to require config.
func TestDefaultsAreValid(t *testing.T) {
	for provider, spec := range Defaults() {
		if provider == ProviderGeneric {
			if err := spec.Validate(); err == nil {
				t.Fatalf("generic default must NOT validate without config")
			}
			continue
		}
		if spec.Provider != provider {
			t.Errorf("%s: Provider field = %q, want %q", provider, spec.Provider, provider)
		}
		if err := spec.Validate(); err != nil {
			t.Errorf("%s: default spec invalid: %v", provider, err)
		}
		if len(spec.CredEnv) == 0 {
			t.Errorf("%s: default spec has no CredEnv, so a BYOK secret has nowhere to go", provider)
		}
		if spec.Env["HOME"] != "/tmp" {
			t.Errorf("%s: HOME=%q, want /tmp (sandbox root is read-only)", provider, spec.Env["HOME"])
		}
	}
}

// TestDefaultsAreCopies makes sure a caller mutating a resolved Spec cannot
// corrupt the built-in defaults for the next connection.
func TestDefaultsAreCopies(t *testing.T) {
	a := Defaults()[ProviderClaudeCode]
	a.TaskArgs[0] = "mutated"
	a.Env["HOME"] = "/mutated"
	b := Defaults()[ProviderClaudeCode]
	if b.TaskArgs[0] != "-p" || b.Env["HOME"] != "/tmp" {
		t.Fatalf("Defaults() leaked shared state: %v %v", b.TaskArgs, b.Env)
	}
}

// TestClaudeCodeDefaultMatchesToday pins the exact invocation
// internal/tools/delegate_external_tool.go runCLI uses today, so generalising the
// package can never silently change Claude Code's behaviour.
func TestClaudeCodeDefaultMatchesToday(t *testing.T) {
	spec, err := Resolve("claude_code", nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	argv, err := spec.Command("port the repo", PermissionAuto)
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	want := []string{
		"claude", "-p", "port the repo",
		"--output-format", "stream-json", "--verbose", "--include-partial-messages",
		// AskUserQuestion renders a picker the CLI cannot show headlessly, and
		// this transport cannot return a structured choice — so it is disallowed
		// and the model asks in prose instead.
		"--disallowedTools", "AskUserQuestion",
		"--permission-mode", "bypassPermissions",
	}
	if !reflect.DeepEqual(argv, want) {
		t.Fatalf("argv mismatch\n got: %q\nwant: %q", argv, want)
	}
	if spec.Output != OutputClaudeStreamJSON {
		t.Errorf("Output = %q, want %q", spec.Output, OutputClaudeStreamJSON)
	}
	// Credential preference order matters: OAuth (subscription billing) first.
	if !reflect.DeepEqual(spec.CredEnv, []string{"CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_API_KEY"}) {
		t.Errorf("CredEnv = %q", spec.CredEnv)
	}
	wantEnv := map[string]string{"HOME": "/tmp", "GOCACHE": "/tmp/go-build", "GOPATH": "/tmp/go", "GOMODCACHE": "/tmp/go/pkg/mod"}
	if !reflect.DeepEqual(spec.Env, wantEnv) {
		t.Errorf("Env = %v, want %v", spec.Env, wantEnv)
	}
}

func TestResolveProviderAliases(t *testing.T) {
	for _, in := range []string{"claude_code", "claude", "ClaudeCode", " claude-code ", "CLAUDE"} {
		spec, err := Resolve(in, nil)
		if err != nil {
			t.Fatalf("Resolve(%q): %v", in, err)
		}
		if spec.Provider != ProviderClaudeCode {
			t.Errorf("Resolve(%q).Provider = %q", in, spec.Provider)
		}
	}
	if spec, err := Resolve("gemini", nil); err != nil || spec.Provider != ProviderGeminiCLI {
		t.Errorf("Resolve(gemini) = %+v, %v", spec, err)
	}
}

func TestResolveOverlay(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		cfg      string
		check    func(t *testing.T, s Spec)
	}{
		{
			name:     "empty config keeps defaults",
			provider: "codex",
			cfg:      `{}`,
			check: func(t *testing.T, s Spec) {
				if s.Binary != "codex" || s.Output != OutputJSONL {
					t.Errorf("got %+v", s)
				}
			},
		},
		{
			name:     "null config keeps defaults",
			provider: "aider",
			cfg:      `null`,
			check: func(t *testing.T, s Spec) {
				if s.Binary != "aider" {
					t.Errorf("Binary = %q", s.Binary)
				}
			},
		},
		{
			name:     "override binary and task args",
			provider: "codex",
			cfg:      `{"binary":"codex-next","task_args":["run","--prompt","{{task}}"]}`,
			check: func(t *testing.T, s Spec) {
				if s.Binary != "codex-next" {
					t.Errorf("Binary = %q", s.Binary)
				}
				if !reflect.DeepEqual(s.TaskArgs, []string{"run", "--prompt", "{{task}}"}) {
					t.Errorf("TaskArgs = %q", s.TaskArgs)
				}
				// untouched keys survive
				if !reflect.DeepEqual(s.ExtraArgs, []string{"--json"}) {
					t.Errorf("ExtraArgs = %q", s.ExtraArgs)
				}
			},
		},
		{
			name:     "empty array clears a default flag",
			provider: "codex",
			cfg:      `{"extra_args":[],"auto_approve_args":[]}`,
			check: func(t *testing.T, s Spec) {
				if len(s.ExtraArgs) != 0 || len(s.AutoApproveArgs) != 0 {
					t.Errorf("expected cleared, got %q / %q", s.ExtraArgs, s.AutoApproveArgs)
				}
			},
		},
		{
			name:     "env merges over defaults and null unsets",
			provider: "claude_code",
			cfg:      `{"env":{"NO_COLOR":"1","GOMODCACHE":null}}`,
			check: func(t *testing.T, s Spec) {
				if s.Env["NO_COLOR"] != "1" {
					t.Errorf("NO_COLOR not applied: %v", s.Env)
				}
				if s.Env["HOME"] != "/tmp" {
					t.Errorf("merge dropped HOME: %v", s.Env)
				}
				if _, ok := s.Env["GOMODCACHE"]; ok {
					t.Errorf("null did not unset GOMODCACHE: %v", s.Env)
				}
			},
		},
		{
			name:     "override output and cred env",
			provider: "gemini_cli",
			cfg:      `{"output":"JSONL","cred_env":["MY_KEY"],"manual_approve_args":["--approval-mode","default"]}`,
			check: func(t *testing.T, s Spec) {
				if s.Output != OutputJSONL {
					t.Errorf("Output = %q", s.Output)
				}
				if !reflect.DeepEqual(s.CredEnv, []string{"MY_KEY"}) {
					t.Errorf("CredEnv = %q", s.CredEnv)
				}
				if !s.SupportsManualApproval() {
					t.Errorf("manual approval should now be supported")
				}
			},
		},
		{
			name:     "generic fully described by config",
			provider: "generic",
			cfg:      `{"binary":"mycli","task_args":["--do","{{task}}"],"output":"text","cred_env":["MYCLI_TOKEN"],"env":{"HOME":"/tmp"}}`,
			check: func(t *testing.T, s Spec) {
				argv, err := s.Command("hello", PermissionAuto)
				if err != nil {
					t.Fatalf("Command: %v", err)
				}
				if !reflect.DeepEqual(argv, []string{"mycli", "--do", "hello"}) {
					t.Errorf("argv = %q", argv)
				}
			},
		},
		{
			name:     "unknown provider with full config is accepted",
			provider: "brand_new_cli",
			cfg:      `{"binary":"newcli","task_args":["{{task}}"],"output":"text"}`,
			check: func(t *testing.T, s Spec) {
				if s.Provider != "brand_new_cli" || s.Binary != "newcli" {
					t.Errorf("got %+v", s)
				}
			},
		},
		{
			name:     "unrelated config keys are ignored",
			provider: "aider",
			cfg:      `{"model":"gpt-5","endpoint":"https://example.test"}`,
			check: func(t *testing.T, s Spec) {
				if s.Binary != "aider" {
					t.Errorf("Binary = %q", s.Binary)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spec, err := Resolve(tc.provider, json.RawMessage(tc.cfg))
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			tc.check(t, spec)
		})
	}
}

func TestResolveErrors(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		cfg      string
		wantIs   error
		wantSubs []string
	}{
		{
			name:     "unknown provider without config",
			provider: "nope_cli",
			cfg:      "",
			wantIs:   ErrUnknownProvider,
			wantSubs: []string{"nope_cli", "claude_code", "generic"},
		},
		{
			name:     "generic without config",
			provider: "generic",
			cfg:      "",
			wantSubs: []string{"generic", "binary"},
		},
		{
			name:     "generic with only a binary",
			provider: "generic",
			cfg:      `{"binary":"mycli"}`,
			wantSubs: []string{"task_args", TaskPlaceholder},
		},
		{
			name:     "task args missing placeholder",
			provider: "codex",
			cfg:      `{"task_args":["exec"]}`,
			wantSubs: []string{"task_args", "found 0"},
		},
		{
			name:     "task args with two placeholders",
			provider: "codex",
			cfg:      `{"task_args":["exec","{{task}}","--also","{{task}}"]}`,
			wantSubs: []string{"found 2"},
		},
		{
			name:     "binary cleared by config",
			provider: "codex",
			cfg:      `{"binary":"   "}`,
			wantSubs: []string{"no binary to run", "binary"},
		},
		{
			name:     "unknown output format",
			provider: "codex",
			cfg:      `{"output":"yaml"}`,
			wantSubs: []string{"yaml", "claude_stream_json", "jsonl", "text"},
		},
		{
			name:     "generic without output",
			provider: "generic",
			cfg:      `{"binary":"mycli","task_args":["{{task}}"]}`,
			wantSubs: []string{"output"},
		},
		{
			name:     "malformed config json",
			provider: "codex",
			cfg:      `{"binary":`,
			wantSubs: []string{"not valid JSON"},
		},
		{
			name:     "config of the wrong shape",
			provider: "codex",
			cfg:      `["codex"]`,
			wantSubs: []string{"not valid JSON"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Resolve(tc.provider, json.RawMessage(tc.cfg))
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			if tc.wantIs != nil && !errors.Is(err, tc.wantIs) {
				t.Errorf("errors.Is(%v, %v) = false", err, tc.wantIs)
			}
			for _, sub := range tc.wantSubs {
				if !strings.Contains(err.Error(), sub) {
					t.Errorf("error %q does not mention %q", err, sub)
				}
			}
		})
	}
}

func TestKnownProviders(t *testing.T) {
	got := KnownProviders()
	want := []string{ProviderAider, ProviderClaudeCode, ProviderCodex, ProviderGeminiCLI, ProviderGeneric}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("KnownProviders() = %q, want %q", got, want)
	}
}
