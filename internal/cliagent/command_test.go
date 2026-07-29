package cliagent

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestCommand(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		cfg      string
		task     string
		mode     PermissionMode
		want     []string
		wantIs   error
		wantSubs []string
	}{
		{
			name:     "claude auto",
			provider: "claude_code",
			task:     "fix the build",
			mode:     PermissionAuto,
			want: []string{"claude", "-p", "fix the build",
				"--output-format", "stream-json", "--verbose", "--include-partial-messages",
				"--permission-mode", "bypassPermissions"},
		},
		{
			name:     "claude manual uses its own flag",
			provider: "claude_code",
			task:     "fix the build",
			mode:     PermissionManual,
			want: []string{"claude", "-p", "fix the build",
				"--output-format", "stream-json", "--verbose", "--include-partial-messages",
				"--permission-mode", "default"},
		},
		{
			name:     "task with spaces and quotes stays one argv element",
			provider: "codex",
			task:     `port "internal/foo" & run go build`,
			mode:     PermissionAuto,
			want:     []string{"codex", "exec", `port "internal/foo" & run go build`, "--json", "--dangerously-bypass-approvals-and-sandbox"},
		},
		{
			name:     "placeholder embedded in a larger argument",
			provider: "generic",
			cfg:      `{"binary":"mycli","task_args":["--prompt=Do this: {{task}}"],"output":"text","auto_approve_args":["-y"]}`,
			task:     "ship it",
			mode:     PermissionAuto,
			want:     []string{"mycli", "--prompt=Do this: ship it", "-y"},
		},
		{
			name:     "multiline task substituted verbatim",
			provider: "aider",
			task:     "line one\nline two",
			mode:     PermissionAuto,
			want:     []string{"aider", "--message", "line one\nline two", "--no-pretty", "--yes-always"},
		},
		{
			name:     "manual unsupported names the provider",
			provider: "codex",
			task:     "do it",
			mode:     PermissionManual,
			wantIs:   ErrManualApprovalUnsupported,
			wantSubs: []string{"codex", "auto", "manual_approve_args"},
		},
		{
			name:     "manual unsupported for a config-described cli",
			provider: "generic",
			cfg:      `{"binary":"mycli","task_args":["{{task}}"],"output":"text"}`,
			task:     "do it",
			mode:     PermissionManual,
			wantIs:   ErrManualApprovalUnsupported,
			wantSubs: []string{"generic"},
		},
		{
			name:     "empty task rejected",
			provider: "claude_code",
			task:     "   ",
			mode:     PermissionAuto,
			wantSubs: []string{"task is empty"},
		},
		{
			name:     "unknown mode rejected",
			provider: "claude_code",
			task:     "do it",
			mode:     PermissionMode("yolo"),
			wantSubs: []string{"yolo", "auto", "manual"},
		},
		{
			name:     "auto with no approve args is fine",
			provider: "generic",
			cfg:      `{"binary":"mycli","task_args":["{{task}}"],"output":"text"}`,
			task:     "do it",
			mode:     PermissionAuto,
			want:     []string{"mycli", "do it"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spec, err := Resolve(tc.provider, json.RawMessage(tc.cfg))
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			argv, err := spec.Command(tc.task, tc.mode)
			if tc.want != nil {
				if err != nil {
					t.Fatalf("Command: %v", err)
				}
				if !reflect.DeepEqual(argv, tc.want) {
					t.Fatalf("argv mismatch\n got: %q\nwant: %q", argv, tc.want)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected an error, got argv %q", argv)
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

// TestCommandOnInvalidSpec: Command must not build argv from a Spec that would
// never run (callers may hold a hand-built Spec that never went through Resolve).
func TestCommandOnInvalidSpec(t *testing.T) {
	_, err := Spec{Provider: "x", Output: OutputText}.Command("t", PermissionAuto)
	if err == nil || !strings.Contains(err.Error(), "binary") {
		t.Fatalf("want binary error, got %v", err)
	}
}

func TestSupportsManualApproval(t *testing.T) {
	cases := map[string]bool{
		ProviderClaudeCode: true,
		ProviderCodex:      false,
		ProviderAider:      false,
		ProviderGeminiCLI:  false,
	}
	for provider, want := range cases {
		if got := Defaults()[provider].SupportsManualApproval(); got != want {
			t.Errorf("%s: SupportsManualApproval() = %v, want %v", provider, got, want)
		}
	}
}

const testSecret = "sk-ant-oat01-DO-NOT-LEAK-abcdef"

func TestApplyCredential(t *testing.T) {
	tests := []struct {
		name       string
		spec       Spec
		inject     string
		secret     string
		nilEnv     bool
		wantEnv    map[string]string
		wantIs     error
		wantErrSub []string
	}{
		{
			name:    "explicit env descriptor wins over cred_env",
			spec:    Defaults()[ProviderClaudeCode],
			inject:  "env:ANTHROPIC_API_KEY",
			secret:  testSecret,
			wantEnv: map[string]string{"ANTHROPIC_API_KEY": testSecret},
		},
		{
			name:    "descriptor is tolerant of spacing and case",
			spec:    Defaults()[ProviderClaudeCode],
			inject:  " ENV : MY_TOKEN ",
			secret:  testSecret,
			wantEnv: map[string]string{"MY_TOKEN": testSecret},
		},
		{
			name:    "no descriptor falls back to first cred_env",
			spec:    Defaults()[ProviderClaudeCode],
			inject:  "",
			secret:  testSecret,
			wantEnv: map[string]string{"CLAUDE_CODE_OAUTH_TOKEN": testSecret},
		},
		{
			name:    "fallback honours a config-overridden cred_env",
			spec:    mustResolve(t, "codex", `{"cred_env":["MY_CODEX_KEY","OPENAI_API_KEY"]}`),
			inject:  "  ",
			secret:  testSecret,
			wantEnv: map[string]string{"MY_CODEX_KEY": testSecret},
		},
		{
			name:       "no descriptor and no cred_env is an error",
			spec:       mustResolve(t, "generic", `{"binary":"mycli","task_args":["{{task}}"],"output":"text"}`),
			inject:     "",
			secret:     testSecret,
			wantIs:     ErrNoCredentialTarget,
			wantErrSub: []string{"generic", "cred_env"},
		},
		{
			name:       "file injection is refused loudly",
			spec:       Defaults()[ProviderGeminiCLI],
			inject:     "file:/tmp/.gemini/creds.json",
			secret:     testSecret,
			wantIs:     nil,
			wantErrSub: []string{"not supported yet", "cred_env"},
		},
		{
			name:       "malformed descriptor",
			spec:       Defaults()[ProviderClaudeCode],
			inject:     "env:",
			secret:     testSecret,
			wantErrSub: []string{"malformed", "env:VAR"},
		},
		{
			name:       "descriptor without a kind",
			spec:       Defaults()[ProviderClaudeCode],
			inject:     "ANTHROPIC_API_KEY",
			secret:     testSecret,
			wantErrSub: []string{"malformed"},
		},
		{
			name:       "empty secret",
			spec:       Defaults()[ProviderClaudeCode],
			inject:     "env:ANTHROPIC_API_KEY",
			secret:     "  ",
			wantErrSub: []string{"secret is empty"},
		},
		{
			name:       "nil env map",
			spec:       Defaults()[ProviderClaudeCode],
			inject:     "env:ANTHROPIC_API_KEY",
			secret:     testSecret,
			nilEnv:     true,
			wantErrSub: []string{"env map is nil"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var env map[string]string
			if !tc.nilEnv {
				env = map[string]string{}
			}
			err := tc.spec.ApplyCredential(tc.secret, tc.inject, env)

			if tc.wantEnv != nil {
				if err != nil {
					t.Fatalf("ApplyCredential: %v", err)
				}
				if !reflect.DeepEqual(env, tc.wantEnv) {
					// Print keys only — never the values, which are secrets.
					t.Fatalf("env keys = %q, want %q", keysOf(env), keysOf(tc.wantEnv))
				}
				return
			}

			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			if tc.wantIs != nil && !errors.Is(err, tc.wantIs) {
				t.Errorf("errors.Is(%v, %v) = false", err, tc.wantIs)
			}
			for _, sub := range tc.wantErrSub {
				if !strings.Contains(err.Error(), sub) {
					t.Errorf("error %q does not mention %q", err, sub)
				}
			}
			// A failed injection must not have half-written the secret anywhere.
			for k, v := range env {
				if strings.Contains(v, tc.secret) && strings.TrimSpace(tc.secret) != "" {
					t.Errorf("secret was written to %q despite the error", k)
				}
			}
			// And the secret must never appear in the message a user sees.
			if strings.Contains(err.Error(), strings.TrimSpace(testSecret)) {
				t.Errorf("error message leaks the secret: %q", err)
			}
		})
	}
}

// TestApplyCredentialInjectsExactlyOneVar: the CLIs' own precedence differs from
// ours (Claude Code prefers ANTHROPIC_API_KEY over the OAuth token), so injecting
// more than one variable would make the choice implicit.
func TestApplyCredentialInjectsExactlyOneVar(t *testing.T) {
	env := map[string]string{"HOME": "/tmp"}
	if err := Defaults()[ProviderClaudeCode].ApplyCredential(testSecret, "", env); err != nil {
		t.Fatal(err)
	}
	if len(env) != 2 {
		t.Fatalf("expected exactly one new var, env keys = %q", keysOf(env))
	}
	if env["CLAUDE_CODE_OAUTH_TOKEN"] != testSecret {
		t.Errorf("credential not injected into the preferred var: %q", keysOf(env))
	}
	if _, ok := env["ANTHROPIC_API_KEY"]; ok {
		t.Errorf("second credential var was also set: %q", keysOf(env))
	}
}

// TestCredentialNeverReachesArgv: a secret belongs in the env only. argv is
// logged and shown in errors ("did not finish (exit N)"), so a provider default
// that put the credential on the command line would leak it into logs.
func TestCredentialNeverReachesArgv(t *testing.T) {
	for _, provider := range KnownProviders() {
		spec := Defaults()[provider]
		if provider == ProviderGeneric {
			spec = mustResolve(t, "generic", `{"binary":"mycli","task_args":["{{task}}"],"output":"text","cred_env":["MYCLI_TOKEN"]}`)
		}
		env := map[string]string{}
		if err := spec.ApplyCredential(testSecret, "", env); err != nil {
			t.Fatalf("%s: ApplyCredential: %v", provider, err)
		}
		argv, err := spec.Command("do the work", PermissionAuto)
		if err != nil {
			t.Fatalf("%s: Command: %v", provider, err)
		}
		if strings.Contains(strings.Join(argv, " "), testSecret) {
			t.Errorf("%s: credential leaked into argv", provider)
		}
	}
}

func mustResolve(t *testing.T, provider, cfg string) Spec {
	t.Helper()
	s, err := Resolve(provider, json.RawMessage(cfg))
	if err != nil {
		t.Fatalf("Resolve(%q): %v", provider, err)
	}
	return s
}

// keysOf returns sorted-ish key names for assertions; values are deliberately
// never printed because they may be secrets.
func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
