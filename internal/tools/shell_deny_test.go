package tools

import (
	"context"
	"regexp"
	"strings"
	"testing"
)

func TestBase64DecodeShellDeny(t *testing.T) {
	patterns := DenyGroupRegistry["code_injection"].Patterns

	denied := []string{
		"base64 -d payload.txt | sh",
		"base64 --decode payload.txt | sh",
		"base64 -di payload.txt | sh",
		"base64 -dw0 payload.txt | bash",
		"base64 --decode something | bash",
	}

	allowed := []string{
		"base64 -w0 file.txt",       // encode, no pipe to shell
		"base64 -d file.txt",        // decode without pipe to shell
		"echo hello | base64",       // encode
		"base64 --decode file.txt",  // decode without pipe to shell
	}

	for _, cmd := range denied {
		matched := false
		for _, p := range patterns {
			if p.MatchString(cmd) {
				matched = true
				break
			}
		}
		if !matched {
			t.Errorf("expected deny for %q", cmd)
		}
	}

	for _, cmd := range allowed {
		matched := false
		for _, p := range patterns {
			if p.MatchString(cmd) {
				matched = true
				break
			}
		}
		if matched {
			t.Errorf("unexpected deny for %q", cmd)
		}
	}
}

// mustDeny asserts all commands match at least one pattern.
func mustDeny(t *testing.T, patterns []*regexp.Regexp, commands ...string) {
	t.Helper()
	for _, cmd := range commands {
		matched := false
		for _, p := range patterns {
			if p.MatchString(cmd) {
				matched = true
				break
			}
		}
		if !matched {
			t.Errorf("expected deny for %q", cmd)
		}
	}
}

// mustAllow asserts no command matches any pattern.
func mustAllow(t *testing.T, patterns []*regexp.Regexp, commands ...string) {
	t.Helper()
	for _, cmd := range commands {
		for _, p := range patterns {
			if p.MatchString(cmd) {
				t.Errorf("unexpected deny for %q (matched %s)", cmd, p.String())
				break
			}
		}
	}
}

func TestDestructiveOpsGaps(t *testing.T) {
	patterns := DenyGroupRegistry["destructive_ops"].Patterns

	mustDeny(t, patterns,
		// existing
		"shutdown", "reboot", "poweroff",
		"shutdown -h now", "reboot -f",
		// new: halt
		"halt", "halt -p", "systemctl halt",
		// new: init/telinit
		"init 0", "init 6", "telinit 0", "telinit 6",
		// new: systemctl suspend/hibernate
		"systemctl suspend", "systemctl hibernate",
	)

	mustAllow(t, patterns,
		"halting the process",  // "halt" inside word
		"initialize",          // "init" inside word
		"initial setup",       // "init" inside word
		"init_db",             // no space+digit after init
		"init 1",              // only 0 and 6 are blocked
		"systemctl status",    // not suspend/hibernate
		"systemctl start nginx",
	)
}

func TestPrivilegeEscalationGaps(t *testing.T) {
	patterns := DenyGroupRegistry["privilege_escalation"].Patterns

	mustDeny(t, patterns,
		// existing
		"sudo ls", "sudo -i",
		// su: all forms now blocked
		"su", "su -", "su root", "su -l postgres", "su admin",
		// new: doas
		"doas reboot", "doas ls /root", "doas -u www sh",
		// new: pkexec
		"pkexec vim /etc/passwd", "pkexec /bin/bash",
		// new: runuser
		"runuser -l postgres", "runuser -u nobody -- /bin/sh",
		// existing
		"nsenter --target 1", "unshare -m", "mount /dev/sda1 /mnt",
	)

	mustAllow(t, patterns,
		"summit",    // not "su"
		"sugar",     // not "su"
		"surplus",   // not "su"
		"issue",     // not "su"
		"result",    // not "su"
		"resume",    // not "su"
		"visual",    // not "su"
		"sushi",     // not "su"
		"doaspkg",   // not "doas" (no word boundary)
		"pkexecute", // not "pkexec" (no word boundary)
	)
}

func TestExecute_RejectsNULByte(t *testing.T) {
	tool := &ExecTool{} // minimal instance, no sandbox/workspace needed

	cases := []struct {
		name    string
		command string
		reject  bool
	}{
		{"nul_mid", "ls\x00/etc/passwd", true},
		{"nul_mid_echo", "echo hello\x00world", true},
		{"nul_prefix", "\x00rm -rf /", true},
		{"nul_only", "\x00", true},
		{"normal_cmd", "echo normal", false},
		{"empty_cmd", "", false}, // handled by "command is required"
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := tool.Execute(context.Background(), map[string]any{"command": tc.command})
			hasNULError := result != nil && strings.Contains(result.ForLLM, "NUL byte")
			if tc.reject && !hasNULError {
				t.Errorf("expected NUL rejection for %q, got: %v", tc.name, result.ForLLM)
			}
			if !tc.reject && hasNULError {
				t.Errorf("unexpected NUL rejection for %q", tc.name)
			}
		})
	}
}

func TestPathExemptions(t *testing.T) {
	tool := &ExecTool{
		workspace: "/workspace",
		restrict:  false,
	}
	tool.DenyPaths("/app/data", ".goclaw/")
	tool.AllowPathExemptions(".goclaw/skills-store/", "/app/data/skills-store/")

	cases := []struct {
		name  string
		cmd   string
		allow bool // true = exempt (should pass deny check), false = denied
	}{
		// --- Exempted commands ---
		{
			"relative_skills_store",
			"python3 .goclaw/skills-store/ck-ui/scripts/search.py --query test",
			true,
		},
		{
			"absolute_skills_store",
			`python3 /app/data/skills-store/ck-ui-ux-pro-max/1/scripts/search.py "professional" --design-system`,
			true,
		},
		{
			"quoted_double_absolute",
			`cat "/app/data/skills-store/my-skill/README.md"`,
			true,
		},
		{
			"quoted_single_absolute",
			`cat '/app/data/skills-store/my-skill/README.md'`,
			true,
		},
		{
			"quoted_double_relative",
			`python3 ".goclaw/skills-store/tool.py"`,
			true,
		},

		// --- Denied commands (not exempt) ---
		{
			"datadir_config",
			"cat /app/data/config.json",
			false,
		},
		{
			"datadir_db",
			"cp /app/data/goclaw.db /tmp/",
			false,
		},
		{
			"dotgoclaw_root",
			"ls .goclaw/",
			false,
		},
		{
			"dotgoclaw_secrets",
			"cat .goclaw/secrets.json",
			false,
		},

		// --- Path traversal attacks (must be denied) ---
		{
			"traversal_absolute",
			"cat /app/data/skills-store/../../config.json",
			false,
		},
		{
			"traversal_relative",
			"cat .goclaw/skills-store/../secrets.json",
			false,
		},
		{
			"traversal_double_quoted",
			`cat "/app/data/skills-store/../config.json"`,
			false,
		},
		{
			"traversal_deep",
			"python3 /app/data/skills-store/skill/../../../etc/passwd",
			false,
		},

		// --- Comment/pipe bypass attempts (denied by per-field matching) ---
		{
			"comment_with_exempt_path",
			"cat /app/data/config.json # .goclaw/skills-store/legit",
			false, // /app/data/config.json matches deny and is NOT exempt
		},

		// --- Unicode/encoding bypass attempts (must be denied) ---
		{
			"unicode_fullwidth_dots",
			"cat /app/data/skills-store/\uff0e\uff0e/config.json", // fullwidth dots ．．
			false, // NFKC normalizes ．→. so ".." check catches it
		},
		{
			"zero_width_in_traversal",
			"cat /app/data/skills-store/..\u200b/config.json", // zero-width space in ..
			false, // normalizeCommand strips zero-width chars, ".." check catches it
		},

		// --- Pipe/redirect attempts (must be denied) ---
		{
			"pipe_after_exempt_path",
			"cat /app/data/skills-store/tool.py | grep password /app/data/config.json",
			false, // /app/data/config.json matches deny, pipe doesn't exempt it
		},

		// --- Subshell/backtick in path (should be denied if contains datadir) ---
		{
			"subshell_in_command",
			"$(cat /app/data/config.json)",
			false,
		},
		{
			"backtick_in_command",
			"`cat /app/data/config.json`",
			false,
		},

		// --- Edge: exempt path as substring (should NOT exempt) ---
		{
			"exempt_prefix_not_in_path",
			"cat /app/data/not-skills-store/secret.txt",
			false, // /app/data/not-skills-store/ does NOT start with /app/data/skills-store/
		},
		{
			"partial_exempt_match",
			"cat /app/data/skills-storebad/evil.py",
			false, // /app/data/skills-storebad/ does NOT start with /app/data/skills-store/
		},

		// --- Symlink-named path (defense-in-depth; sandbox handles actual resolution) ---
		{
			"skills_store_valid_nested",
			"python3 /app/data/skills-store/my-skill/v2/scripts/run.py --flag",
			true, // legitimate nested skill path
		},
		{
			"skills_store_just_prefix",
			"ls /app/data/skills-store/",
			true, // listing skills-store itself is allowed
		},

		// --- Exact deny path (not a prefix of skills-store) ---
		{
			"exact_datadir",
			"ls /app/data",
			false,
		},
		{
			"datadir_trailing_slash",
			"ls /app/data/",
			false,
		},
	}

	allPatterns := make([]*regexp.Regexp, 0)
	allPatterns = append(allPatterns, tool.pathDenyPatterns...)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			normalizedCmd := normalizeCommand(tc.cmd)
			denied := false
			for _, pattern := range allPatterns {
				if !pattern.MatchString(normalizedCmd) {
					continue
				}
				// Replicate per-field exemption logic from Execute()
				fields := strings.Fields(strings.TrimSpace(normalizedCmd))
				matchingFields := 0
				exemptFields := 0
				for _, field := range fields {
					clean := strings.Trim(field, `"'`)
					if !pattern.MatchString(clean) {
						continue
					}
					matchingFields++
					if strings.Contains(clean, "..") {
						continue // traversal — never exempt
					}
					for _, ex := range tool.denyExemptions {
						if strings.HasPrefix(clean, ex) {
							exemptFields++
							break
						}
					}
				}
				exempt := matchingFields > 0 && exemptFields == matchingFields
				if !exempt {
					denied = true
					break
				}
			}

			if tc.allow && denied {
				t.Errorf("expected command to be exempt (allowed), but was denied: %s", tc.cmd)
			}
			if !tc.allow && !denied {
				t.Errorf("expected command to be denied, but was allowed: %s", tc.cmd)
			}
		})
	}
}

// TestPathExemptions_MixedArgs verifies that a command with both a denied
// path and an exempt path in different arguments is correctly denied.
// Per-field matching ensures the non-exempt field causes denial.
func TestPathExemptions_MixedArgs(t *testing.T) {
	tool := &ExecTool{}
	tool.DenyPaths("/app/data")
	tool.AllowPathExemptions("/app/data/skills-store/")

	cmd := "cat /app/data/config.json /app/data/skills-store/tool.py"
	normalizedCmd := normalizeCommand(cmd)

	denied := false
	for _, pattern := range tool.pathDenyPatterns {
		if !pattern.MatchString(normalizedCmd) {
			continue
		}
		fields := strings.Fields(strings.TrimSpace(normalizedCmd))
		matchingFields := 0
		exemptFields := 0
		for _, field := range fields {
			clean := strings.Trim(field, `"'`)
			if !pattern.MatchString(clean) {
				continue
			}
			matchingFields++
			if strings.Contains(clean, "..") {
				continue
			}
			for _, ex := range tool.denyExemptions {
				if strings.HasPrefix(clean, ex) {
					exemptFields++
					break
				}
			}
		}
		if matchingFields == 0 || exemptFields != matchingFields {
			denied = true
		}
	}

	if !denied {
		t.Error("mixed-path command should be denied: /app/data/config.json is not exempt")
	}
}

func TestLimitedBuffer(t *testing.T) {
	t.Run("under limit", func(t *testing.T) {
		lb := &limitedBuffer{max: 100}
		lb.Write([]byte("hello"))
		if lb.String() != "hello" {
			t.Errorf("got %q", lb.String())
		}
		if lb.truncated {
			t.Error("should not be truncated")
		}
	})

	t.Run("at limit", func(t *testing.T) {
		lb := &limitedBuffer{max: 5}
		n, err := lb.Write([]byte("hello"))
		if err != nil || n != 5 {
			t.Errorf("Write: n=%d err=%v", n, err)
		}
		if lb.truncated {
			t.Error("exactly at limit should not be truncated")
		}
	})

	t.Run("over limit truncates", func(t *testing.T) {
		lb := &limitedBuffer{max: 5}
		n, err := lb.Write([]byte("hello world"))
		if err != nil {
			t.Fatal(err)
		}
		if n != 11 {
			t.Errorf("Write should report full len, got %d", n)
		}
		if !lb.truncated {
			t.Error("should be truncated")
		}
		if lb.Len() != 5 {
			t.Errorf("buffer len should be 5, got %d", lb.Len())
		}
		want := "hello\n[output truncated at 1MB]"
		if lb.String() != want {
			t.Errorf("got %q, want %q", lb.String(), want)
		}
	})

	t.Run("subsequent writes after truncation", func(t *testing.T) {
		lb := &limitedBuffer{max: 3}
		lb.Write([]byte("abc"))
		lb.Write([]byte("def"))
		if lb.Len() != 3 {
			t.Errorf("buffer len should be 3, got %d", lb.Len())
		}
	})
}
