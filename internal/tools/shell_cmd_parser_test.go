package tools

import (
	"slices"
	"strings"
	"testing"
)

// winPath joins parts with backslashes so the fixtures read like real
// cmd.exe paths regardless of the host OS running the test.
func winPath(parts ...string) string {
	return strings.Join(parts, `\`)
}

// TestParseWindowsCommandWords exercises the cmd.exe word parser directly so
// Linux/macOS CI covers the Windows exemption parsing rules.
func TestParseWindowsCommandWords(t *testing.T) {
	dataDir := winPath("D:", "gc", "data")
	skill := winPath(dataDir, "skills-store", "anysearch", "scripts", "anysearch_cli.js")
	cfg := winPath(dataDir, "config.json")
	// Forward slashes in the directory part, backslash before the file.
	mixed := strings.ReplaceAll(winPath(dataDir, "skills-store"), `\`, "/") + `\x.js`

	cases := []struct {
		name   string
		cmd    string
		wantOK bool
		want   []string
	}{
		// --- Legitimate forms ---
		{"unquoted_backslash", "node " + skill + " --q x", true, []string{"node", skill, "--q", "x"}},
		{"double_quoted", `node "` + skill + `"`, true, []string{"node", skill}},
		{"double_quoted_with_space", `node "` + winPath("D:", "My Data", "x.js") + `"`, true, []string{"node", winPath("D:", "My Data", "x.js")}},
		{"partially_quoted_word", `node "` + dataDir + `"\x.js`, true, []string{"node", winPath(dataDir, "x.js")}},
		{"mixed_separators", "node " + mixed, true, []string{"node", mixed}},
		{"flag_equals", "node " + skill + " --input=" + skill, true, []string{"node", skill, "--input", skill}},
		{"quoted_parens_literal", `"` + winPath("D:", "Program Files (x86)", "node.exe") + `" ` + skill, true, []string{winPath("D:", "Program Files (x86)", "node.exe"), skill}},

		// --- Single quotes are literal in cmd.exe ---
		{"single_quote_unterminated", "echo '" + skill + " & type " + cfg, true, []string{"echo", "'" + skill, "type", cfg}},
		{"single_quote_space", "type '" + winPath(dataDir, "skills-store") + " " + cfg, true, []string{"type", "'" + winPath(dataDir, "skills-store"), cfg}},
		{"single_quote_pair", "echo '" + skill + " & type " + cfg + " & echo '", true, []string{"echo", "'" + skill, "type", cfg, "echo", "'"}},

		// --- Operators split commands ---
		{"amp", skill + "&type " + cfg, true, []string{skill, "type", cfg}},
		{"and", skill + " && type " + cfg, true, []string{skill, "type", cfg}},
		{"pipe", skill + "|type " + cfg, true, []string{skill, "type", cfg}},
		{"or", skill + " || type " + cfg, true, []string{skill, "type", cfg}},
		{"redirect", "type " + skill + ">" + cfg, true, []string{"type", skill, cfg}},
		{"cmd_delims", "type " + skill + "," + cfg + ";" + cfg, true, []string{"type", skill, cfg, cfg}},

		// --- Fail closed ---
		{"unterminated_double_quote", `type "` + skill + " " + cfg, false, nil},
		{"unterminated_double_quote_amp", `echo "` + skill + " & type " + cfg, false, nil},
		{"caret_escape", "echo " + skill + " ^& type " + cfg, false, nil},
		{"caret_in_quotes", `echo "` + skill + `^"`, false, nil},
		{"percent_expansion", "type %GOCLAW_DATA%\\config.json " + skill, false, nil},
		{"delayed_expansion", "type !X! " + skill, false, nil},
		{"newline", skill + "\ntype " + cfg, false, nil},
		{"carriage_return", skill + "\rtype " + cfg, false, nil},
		{"paren_block", "(type " + cfg + ") " + skill, false, nil},
		{"quoted_amp_nested_shell", `cmd /c "` + skill + " & type " + cfg + `"`, false, nil},
		{"quoted_pipe_nested_shell", `cmd /c "` + skill + " | type " + cfg + `"`, false, nil},
		{"quoted_redirect", `cmd /c "type ` + cfg + ` > ` + skill + `"`, false, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseWindowsCommandWords(tc.cmd)
			if ok != tc.wantOK {
				t.Fatalf("parseWindowsCommandWords(%q) ok = %v, want %v (words %#v)", tc.cmd, ok, tc.wantOK, got)
			}
			if tc.wantOK && !slices.Equal(got, tc.want) {
				t.Fatalf("parseWindowsCommandWords(%q) = %#v, want %#v", tc.cmd, got, tc.want)
			}
		})
	}
}

func TestWindowsPathDenyPattern(t *testing.T) {
	dataDir := winPath("D:", "gc", "data")
	re := windowsPathDenyPattern(dataDir)

	matches := []string{
		"type " + winPath(dataDir, "config.json"),
		"type " + strings.ToUpper(winPath(dataDir, "config.json")),
		"type " + strings.ReplaceAll(strings.ToUpper(dataDir), `\`, "/") + `\config.json`,
		"type D:\\\\gc//data\\config.json",
		"type " + `\\?\` + winPath(dataDir, "config.json"),
	}
	for _, cmd := range matches {
		if !re.MatchString(cmd) {
			t.Errorf("pattern %s should match %q", re, cmd)
		}
	}
	misses := []string{
		"type " + winPath("D:", "gc", "dat", "config.json"),
		"type D:gcdata",
	}
	for _, cmd := range misses {
		if re.MatchString(cmd) {
			t.Errorf("pattern %s should not match %q", re, cmd)
		}
	}

	rel := windowsPathDenyPattern(".goclaw/")
	for _, cmd := range []string{"type .goclaw\\x", "type .GOCLAW/x", "type a\\.Goclaw\\\\x"} {
		if !rel.MatchString(cmd) {
			t.Errorf("pattern %s should match %q", rel, cmd)
		}
	}
	if rel.MatchString("type xgoclaw/x") {
		t.Errorf("pattern %s must keep the literal dot", rel)
	}
}
