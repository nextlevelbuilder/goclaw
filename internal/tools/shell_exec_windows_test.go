package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const windowsExecSecret = "GOCLAW-SECRET-MARKER"

// newWindowsProductionExecTool mirrors cmd/gateway_setup.go DenyPaths and
// AllowPathExemptions wiring against a temp data dir holding a fake config.
func newWindowsProductionExecTool(t *testing.T) (tool *ExecTool, ctx context.Context, dataDir, skillDir string) {
	t.Helper()
	root := t.TempDir()
	dataDir = filepath.Join(root, "data")
	workspace := filepath.Join(root, "workspace")
	skillDir = filepath.Join(dataDir, "skills-store", "anysearch", "scripts")
	for _, dir := range []string{workspace, skillDir, filepath.Join(dataDir, "skills-storebad")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", dir, err)
		}
	}
	files := map[string]string{
		filepath.Join(dataDir, "config.json"):              `{"secret":"` + windowsExecSecret + `"}`,
		filepath.Join(dataDir, "skills-storebad", "x.txt"): windowsExecSecret,
		filepath.Join(skillDir, "anysearch_cli.js"):        "console.log('SKILL-JS')\r\n",
		filepath.Join(skillDir, "run.cmd"):                 "@echo SKILL-OK\r\n",
		filepath.Join(skillDir, "readme.txt"):              "SKILL-README\r\n",
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", path, err)
		}
	}

	tool = NewExecTool(workspace, false)
	tool.DenyPaths(dataDir, ".goclaw/")
	tool.AllowPathExemptions(
		".goclaw/skills-store/",
		filepath.Join(dataDir, "skills-store")+"/",
		filepath.Join(dataDir, "tenants")+"/",
	)
	return tool, WithToolWorkspace(context.Background(), workspace), dataDir, skillDir
}

func TestExecute_WindowsSkillExemptionBypassesDenied(t *testing.T) {
	tool, ctx, dataDir, skillDir := newWindowsProductionExecTool(t)
	cfg := filepath.Join(dataDir, "config.json")
	skillJS := filepath.Join(skillDir, "anysearch_cli.js")
	skillsStore := filepath.Join(dataDir, "skills-store")

	cases := map[string]string{
		"single_quote_unterminated": "echo '" + skillJS + " & type " + cfg,
		"single_quote_space":        "type '" + skillsStore + " " + cfg,
		"single_quote_pair":         "echo '" + skillJS + " & type " + cfg + " & echo '",
		"double_quote_unterminated": `type "` + skillJS + " " + cfg,
		"chained_amp":               "type " + filepath.Join(skillDir, "readme.txt") + " & type " + cfg,
		"chained_and":               "type " + filepath.Join(skillDir, "readme.txt") + " && type " + cfg,
		"chained_pipe":              "type " + cfg + " | type " + filepath.Join(skillDir, "readme.txt"),
		"chained_or":                "type " + filepath.Join(skillDir, "missing.txt") + " || type " + cfg,
		"traversal":                 "type " + filepath.Join(skillDir, "readme.txt") + " " + skillsStore + `\..\config.json`,
		"extended_prefix":           "type " + filepath.Join(skillDir, "readme.txt") + ` \\?\` + cfg,
		"sibling_dir":               "type " + filepath.Join(dataDir, "skills-storebad", "x.txt"),
		"uppercase_datadir":         "type " + strings.ToUpper(cfg),
		"forward_slash_datadir":     "type " + filepath.ToSlash(cfg),
		"cmd_delimiter":             "type " + filepath.Join(skillDir, "readme.txt") + "," + cfg,
		"nested_cmd":                `cmd /c "type ` + filepath.Join(skillDir, "readme.txt") + " & type " + cfg + `"`,
		"caret_escape":              "type " + filepath.Join(skillDir, "readme.txt") + " ^& type " + cfg,
		"fullwidth_ampersand":       "type " + filepath.Join(skillDir, "readme.txt") + " \uff06 type " + cfg,
	}
	for name, cmd := range cases {
		t.Run(name, func(t *testing.T) {
			result := tool.Execute(ctx, map[string]any{"command": cmd})
			if strings.Contains(result.ForLLM, windowsExecSecret) {
				t.Fatalf("command leaked protected content: %q\noutput: %s", cmd, result.ForLLM)
			}
			if !strings.Contains(result.ForLLM, "command denied by safety policy") {
				t.Fatalf("command %q was not denied: %s", cmd, result.ForLLM)
			}
		})
	}
}

func TestExecute_WindowsSkillExemptionAllowsLegitimate(t *testing.T) {
	tool, ctx, _, skillDir := newWindowsProductionExecTool(t)
	runCmd := filepath.Join(skillDir, "run.cmd")
	readme := filepath.Join(skillDir, "readme.txt")

	cases := []struct {
		name string
		cmd  string
		want string
	}{
		{"unquoted_script", runCmd, "SKILL-OK"},
		{"unquoted_type", "type " + readme, "SKILL-README"},
		// Double-quoted commands pass the safety check, but exec.Command
		// re-escapes inner quotes as \" for cmd.exe, so their output is not
		// asserted here (want "" = only check they are not denied).
		{"double_quoted_script", `"` + runCmd + `"`, ""},
		{"double_quoted_type", `type "` + readme + `"`, ""},
		{"mixed_separators_node", "node " + strings.Replace(filepath.Join(skillDir, "anysearch_cli.js"), `\`, "/", 2), "SKILL-JS"},
		{"unquoted_node", "node " + filepath.Join(skillDir, "anysearch_cli.js") + " --q x", "SKILL-JS"},
		{"uppercase_path", strings.ToUpper(runCmd), "SKILL-OK"},
		{"single_quote_literal_in_arg", runCmd + " 'quoted arg'", "SKILL-OK"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := tool.Execute(ctx, map[string]any{"command": tc.cmd})
			if strings.Contains(result.ForLLM, "command denied by safety policy") {
				t.Fatalf("legitimate command %q denied: %s", tc.cmd, result.ForLLM)
			}
			if tc.want != "" && !strings.Contains(result.ForLLM, tc.want) {
				t.Fatalf("command %q output missing %q: %s", tc.cmd, tc.want, result.ForLLM)
			}
		})
	}
}
