package tools

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"slices"
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
		"base64 -w0 file.txt",      // encode, no pipe to shell
		"base64 -d file.txt",       // decode without pipe to shell
		"echo hello | base64",      // encode
		"base64 --decode file.txt", // decode without pipe to shell
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
		"halting the process", // "halt" inside word
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
	dataDir := t.TempDir()
	workspace := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspace, 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	tool := &ExecTool{
		workspace: workspace,
		restrict:  false,
	}
	tool.DenyPaths(dataDir, ".goclaw/")
	tool.AllowPathExemptions(".goclaw/skills-store/", filepath.Join(dataDir, "skills-store")+"/")

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
			normalizedCmd := strings.ReplaceAll(normalizeCommand(tc.cmd), "/app/data", dataDir)
			denied := false
			for _, pattern := range allPatterns {
				if !pattern.MatchString(normalizedCmd) {
					continue
				}
				// Replicate per-field exemption logic from Execute()
				fields := parseExecCommandWords(strings.TrimSpace(normalizedCmd))
				matchingFields := 0
				exemptFields := 0
				for _, field := range fields {
					clean := strings.TrimSpace(field)
					if !pattern.MatchString(clean) {
						continue
					}
					matchingFields++
					if matchesAnyPathExemption(clean, tool.denyExemptions, workspace) {
						exemptFields++
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
	dataDir := t.TempDir()
	workspace := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspace, 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	tool := &ExecTool{}
	tool.DenyPaths(dataDir)
	tool.AllowPathExemptions(filepath.Join(dataDir, "skills-store") + "/")

	cmd := "cat " + filepath.Join(dataDir, "config.json") + " " + filepath.Join(dataDir, "skills-store", "tool.py")
	normalizedCmd := normalizeCommand(cmd)

	denied := false
	for _, pattern := range tool.pathDenyPatterns {
		if !pattern.MatchString(normalizedCmd) {
			continue
		}
		fields := parseExecCommandWords(strings.TrimSpace(normalizedCmd))
		matchingFields := 0
		exemptFields := 0
		for _, field := range fields {
			clean := strings.TrimSpace(field)
			if !pattern.MatchString(clean) {
				continue
			}
			matchingFields++
			if matchesAnyPathExemption(clean, tool.denyExemptions, workspace) {
				exemptFields++
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

func TestExecute_AllowsCurrentWorkspaceNestedUnderDeniedRoot(t *testing.T) {
	dataDir := t.TempDir()
	workspace := filepath.Join(dataDir, "teams", "team-123")
	if err := os.MkdirAll(workspace, 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	tool := NewExecTool("/workspace", false)
	tool.DenyPaths(dataDir)

	target := filepath.Join(workspace, ".uploads", "report.png")
	ctx := WithToolWorkspace(context.Background(), workspace)
	result := tool.Execute(ctx, map[string]any{
		"command": "printf '%s' " + target,
	})

	if strings.Contains(result.ForLLM, "command denied by safety policy") {
		t.Fatalf("expected nested workspace path to bypass dataDir deny, got: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, target) {
		t.Fatalf("expected command output to include workspace path, got: %s", result.ForLLM)
	}
}

func TestExecute_AllowsCurrentTeamWorkspaceNestedUnderDeniedRoot(t *testing.T) {
	dataDir := t.TempDir()
	teamWorkspace := filepath.Join(dataDir, "tenants", "acme", "teams", "team-123")
	if err := os.MkdirAll(teamWorkspace, 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	tool := NewExecTool("/workspace", false)
	tool.DenyPaths(dataDir)

	target := filepath.Join(teamWorkspace, "report.png")
	ctx := WithToolWorkspace(context.Background(), t.TempDir())
	ctx = WithToolTeamWorkspace(ctx, teamWorkspace)
	result := tool.Execute(ctx, map[string]any{
		"command": "printf '%s' " + target,
	})

	if strings.Contains(result.ForLLM, "command denied by safety policy") {
		t.Fatalf("expected nested team workspace path to bypass dataDir deny, got: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, target) {
		t.Fatalf("expected command output to include team workspace path, got: %s", result.ForLLM)
	}
}

func TestExecute_DoesNotExemptOtherDataDirPaths(t *testing.T) {
	dataDir := t.TempDir()
	workspace := filepath.Join(dataDir, "teams", "team-123")
	if err := os.MkdirAll(workspace, 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	tool := NewExecTool("/workspace", false)
	tool.DenyPaths(dataDir)

	ctx := WithToolWorkspace(context.Background(), workspace)
	result := tool.Execute(ctx, map[string]any{
		"command": "printf '%s' " + filepath.Join(dataDir, "config.json"),
	})

	if !strings.Contains(result.ForLLM, "command denied by safety policy") {
		t.Fatalf("expected unrelated dataDir path to remain denied, got: %s", result.ForLLM)
	}
}

func TestExecute_DoesNotExemptWorkspaceLocalDotGoclawPaths(t *testing.T) {
	dataDir := t.TempDir()
	workspace := filepath.Join(dataDir, "teams", "team-123")
	if err := os.MkdirAll(workspace, 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	tool := NewExecTool("/workspace", false)
	tool.DenyPaths(dataDir, ".goclaw/")

	ctx := WithToolWorkspace(context.Background(), workspace)
	result := tool.Execute(ctx, map[string]any{
		"command": "printf '%s' " + filepath.Join(workspace, ".goclaw", "secrets.json"),
	})

	if !strings.Contains(result.ForLLM, "command denied by safety policy") {
		t.Fatalf("expected workspace-local .goclaw path to remain denied, got: %s", result.ForLLM)
	}
}

func TestExecute_AllowsQuotedAndPrefixedUploadArguments(t *testing.T) {
	dataDir := t.TempDir()
	workspace := filepath.Join(dataDir, "teams", "team-123")
	if err := os.MkdirAll(filepath.Join(workspace, ".uploads"), 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	target := filepath.Join(workspace, ".uploads", "Quarterly Report.png")
	if err := os.WriteFile(target, []byte("ok"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	tool := NewExecTool("/workspace", false)
	tool.DenyPaths(dataDir)

	ctx := WithToolWorkspace(context.Background(), workspace)
	result := tool.Execute(ctx, map[string]any{
		"command": "printf '%s' file=@\"" + target + "\"",
	})
	if strings.Contains(result.ForLLM, "command denied by safety policy") {
		t.Fatalf("expected quoted/prefixed upload argument to bypass deny, got: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "file=@"+target) {
		t.Fatalf("expected output to contain prefixed path, got: %s", result.ForLLM)
	}
}

func TestExecute_DoesNotExemptSymlinkEscapeInsideTeamWorkspace(t *testing.T) {
	if err := os.MkdirAll(t.TempDir(), 0755); err != nil {
		t.Fatalf("TempDir setup error = %v", err)
	}
	dataDir := t.TempDir()
	workspace := filepath.Join(dataDir, "personal")
	teamWorkspace := filepath.Join(dataDir, "teams", "team-123")
	if err := os.MkdirAll(workspace, 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.MkdirAll(teamWorkspace, 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	protected := filepath.Join(dataDir, "config.json")
	if err := os.WriteFile(protected, []byte("secret"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	linkPath := filepath.Join(teamWorkspace, "leak.txt")
	if err := os.Symlink(protected, linkPath); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	tool := NewExecTool("/workspace", false)
	tool.DenyPaths(dataDir)

	ctx := WithToolWorkspace(context.Background(), workspace)
	ctx = WithToolTeamWorkspace(ctx, teamWorkspace)
	result := tool.Execute(ctx, map[string]any{
		"command": "printf '%s' " + linkPath,
	})

	if !strings.Contains(result.ForLLM, "command denied by safety policy") {
		t.Fatalf("expected symlink escape to remain denied, got: %s", result.ForLLM)
	}
}

func TestExecute_AllowsLegacyWorkspaceUploadsLayout(t *testing.T) {
	dataDir := t.TempDir()
	workspace := filepath.Join(dataDir, ".goclaw", "goclaw-workspace", "ws", "system")
	legacyUploads := filepath.Join(workspace, "uploads")
	if err := os.MkdirAll(legacyUploads, 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	target := filepath.Join(legacyUploads, "Quarterly Report.png")
	if err := os.WriteFile(target, []byte("ok"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	tool := NewExecTool("/workspace", false)
	tool.DenyPaths(dataDir, ".goclaw/")

	ctx := WithToolWorkspace(context.Background(), workspace)
	result := tool.Execute(ctx, map[string]any{
		"command": "cp \"" + target + "\" /tmp/partner.png",
	})

	if strings.Contains(result.ForLLM, "command denied by safety policy") {
		t.Fatalf("expected legacy uploads layout to bypass deny, got: %s", result.ForLLM)
	}
}

func TestPathAliasVariants_AppWorkspaceMirror(t *testing.T) {
	got := pathAliasVariants("/app/workspace/glm-thuc-bo/ws/user/.uploads")
	want := "/app/.goclaw/glm-thuc-bo/ws/user/.uploads"
	if !slices.Contains(got, want) {
		t.Fatalf("expected mirror variant %q in %v", want, got)
	}
}

func TestIsNestedUnderDeniedRoot_RelativeDotGoclaw(t *testing.T) {
	tool := &ExecTool{pathDenyRoots: []string{".goclaw/"}}
	if !tool.isNestedUnderDeniedRoot("/app/.goclaw/glm-thuc-bo/ws/user/.uploads") {
		t.Fatal("expected absolute .goclaw path to be treated as nested under relative deny root")
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
