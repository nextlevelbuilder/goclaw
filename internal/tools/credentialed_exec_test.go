package tools

import (
	"encoding/json"
	"testing"
)

func TestDetectShellOperators(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    int // number of detected operators
	}{
		{"clean command", "gh api repos/foo/bar", 0},
		{"pipe operator", "gh api foo | jq .", 1},
		{"semicolon", "echo a; echo b", 1},
		{"ampersand", "cmd1 && cmd2", 1},
		{"redirect", "cmd > /tmp/out", 1},
		{"backtick", "echo `whoami`", 1},
		{"subshell", "echo $(whoami)", 1},
		{"multiple operators", "cmd1 | cmd2 && cmd3", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ops := detectShellOperators(tt.command)
			if len(ops) != tt.want {
				t.Errorf("detectShellOperators(%q) = %v (len %d), want len %d", tt.command, ops, len(ops), tt.want)
			}
		})
	}
}

func TestExtractUnquotedSegments(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    string
	}{
		{"no quotes", "gh api foo", "gh api foo"},
		{"single quoted pipe", "gh --jq '.[0] | .name'", "gh --jq "},
		{"double quoted pipe", `gh --jq ".[0] | .name"`, "gh --jq "},
		{"mixed quotes", `gh --jq '.[0] | .a' --format "b | c"`, "gh --jq  --format "},
		{"escaped quote in double", `gh "say \"hello\""`, "gh "},
		{"empty single quotes", "gh ''", "gh "},
		{"unquoted metachar", "gh api foo | jq", "gh api foo | jq"},
		// Backslash escape outside quotes: \" should NOT start double-quoting
		{"escaped dquote outside", `gh api \"foo | bar\"`, `gh api \"foo | bar\"`},
		{"escaped squote outside", `gh api \'foo | bar\'`, `gh api \'foo | bar\'`},
		{"double backslash", `gh api \\arg`, `gh api \\arg`},
		{"backslash at end", `gh api foo\`, `gh api foo\`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractUnquotedSegments(tt.command)
			if got != tt.want {
				t.Errorf("extractUnquotedSegments(%q) = %q, want %q", tt.command, got, tt.want)
			}
		})
	}
}

func TestDetectUnquotedShellOperators(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    int
	}{
		// Should NOT detect (inside quotes)
		{"pipe in single quotes", "gh api repos/foo --jq '.[0] | .name'", 0},
		{"pipe in double quotes", `gh api repos/foo --jq ".[0] | .name"`, 0},
		{"semicolon in quotes", `echo 'a; b'`, 0},
		{"backtick in single quotes", "echo 'hello `world`'", 0},
		{"complex jq", `gh api repos/org/repo/commits --jq '.[0] | "SHA: \(.sha)\nAuthor: \(.commit.author.name)"'`, 0},
		// Should detect (outside quotes)
		{"unquoted pipe", "gh api foo | jq .", 1},
		{"unquoted semicolon", "echo a; echo b", 1},
		{"mixed: quoted safe + unquoted unsafe", "gh --jq '.[0] | .x' | cat", 1},
		{"redirect after quotes", "gh api foo --jq '.x' > out.json", 1},
		// Escaped quotes outside quotes: operators after \" must still be detected
		// (backslash prevents " from starting a quoted section)
		{"escaped dquote then pipe", `gh \"arg\" | env`, 1},
		{"escaped dquote with content pipe", `gh api \"foo | bar\"`, 1},
		{"escaped squote then pipe", `gh api \'foo | bar\'`, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ops := detectUnquotedShellOperators(tt.command)
			if len(ops) != tt.want {
				t.Errorf("detectUnquotedShellOperators(%q) = %v (len %d), want len %d", tt.command, ops, len(ops), tt.want)
			}
		})
	}
}

func TestParseCommandBinary(t *testing.T) {
	tests := []struct {
		name       string
		command    string
		wantBinary string
		wantArgs   int
		wantErr    bool
	}{
		{"simple", "gh api foo", "gh", 2, false},
		{"with quotes", "gh api --jq '.[0] | .name'", "gh", 3, false},
		{"empty", "", "", 0, true},
		{"single binary", "gh", "gh", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			binary, args, err := parseCommandBinary(tt.command)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseCommandBinary(%q) err = %v, wantErr %v", tt.command, err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if binary != tt.wantBinary {
					t.Errorf("binary = %q, want %q", binary, tt.wantBinary)
				}
				if len(args) != tt.wantArgs {
					t.Errorf("args len = %d, want %d (args: %v)", len(args), tt.wantArgs, args)
				}
			}
		})
	}
}

// TestMatchesBinaryVerbose verifies start-anchored per-arg matching for
// deny_verbose patterns. Regression guard: `-v` must NOT false-positive on
// `--version` (used by the system to probe CLI availability), but MUST still
// block real verbose flags (`-v`, `-vv`, `-v=1`, `--verbose=true`) to prevent
// leakage of tokens/request bodies via verbose output.
func TestMatchesBinaryVerbose(t *testing.T) {
	ghPatterns, _ := json.Marshal([]string{"--verbose", "-v"})
	gcloudPatterns, _ := json.Marshal([]string{"--verbosity=debug", "--log-http"})
	awsPatterns, _ := json.Marshal([]string{"--debug"})

	tests := []struct {
		name     string
		patterns json.RawMessage
		args     []string
		wantHit  bool
	}{
		// --- regression: safe flags must pass ---
		{"gh --version not blocked", ghPatterns, []string{"--version"}, false},
		{"gh version subcmd not blocked", ghPatterns, []string{"version"}, false},
		{"gh --help not blocked", ghPatterns, []string{"--help"}, false},
		{"gh api repos/x not blocked", ghPatterns, []string{"api", "repos/x"}, false},

		// --- real verbose flags still blocked ---
		{"gh -v blocked", ghPatterns, []string{"-v"}, true},
		{"gh --verbose blocked", ghPatterns, []string{"--verbose"}, true},
		{"gh -vv blocked (escalation)", ghPatterns, []string{"-vv"}, true},
		{"gh -vvv blocked (escalation)", ghPatterns, []string{"-vvv"}, true},
		{"gh --verbose=true blocked (equals form)", ghPatterns, []string{"--verbose=true"}, true},
		{"gh -v in middle of args blocked", ghPatterns, []string{"api", "-v", "repos/x"}, true},

		// --- gcloud patterns: exact flag=value ---
		{"gcloud --verbosity=debug blocked", gcloudPatterns, []string{"--verbosity=debug"}, true},
		{"gcloud --verbosity=info not blocked", gcloudPatterns, []string{"--verbosity=info"}, false},
		{"gcloud --log-http blocked", gcloudPatterns, []string{"--log-http"}, true},
		{"gcloud version not blocked", gcloudPatterns, []string{"version"}, false},

		// --- aws ---
		{"aws --debug blocked", awsPatterns, []string{"--debug"}, true},
		{"aws --debugger not blocked-worthy (prefix match)", awsPatterns, []string{"--debugger"}, true}, // acceptable: still debug family
		{"aws --version not blocked", awsPatterns, []string{"--version"}, false},

		// --- empty / no patterns ---
		{"empty patterns", json.RawMessage(nil), []string{"--verbose"}, false},
		{"empty args", ghPatterns, []string{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesBinaryVerbose(tt.args, tt.patterns)
			if (got != "") != tt.wantHit {
				t.Errorf("matchesBinaryVerbose(%v) = %q, wantHit=%v", tt.args, got, tt.wantHit)
			}
		})
	}
}

// TestMatchesBinaryDenyJoinedArgs verifies deny_args keeps joined-string
// matching so multi-token patterns like `auth\s+login` and `repo\s+delete`
// still work.
func TestMatchesBinaryDenyJoinedArgs(t *testing.T) {
	ghPatterns, _ := json.Marshal([]string{`auth\s+`, `repo\s+delete`, `secret\s+`})

	tests := []struct {
		name    string
		args    []string
		wantHit bool
	}{
		{"gh auth login blocked", []string{"auth", "login"}, true},
		{"gh repo delete blocked", []string{"repo", "delete", "foo/bar"}, true},
		{"gh secret set blocked", []string{"secret", "set", "TOKEN"}, true},
		{"gh api repos allowed", []string{"api", "repos/x"}, false},
		{"gh repo view allowed", []string{"repo", "view", "foo/bar"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesBinaryDeny(tt.args, ghPatterns)
			if (got != "") != tt.wantHit {
				t.Errorf("matchesBinaryDeny(%v) = %q, wantHit=%v", tt.args, got, tt.wantHit)
			}
		})
	}
}
