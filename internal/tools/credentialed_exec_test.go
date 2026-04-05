package tools

import "testing"

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
