package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// A subagent's file and exec tools are constructed fresh, so they start with none of the
// hardening the gateway applies to the parent's instances at startup. Before this was
// wired, spawning a subagent widened reach: the parent could not read config.json or
// exec against the data dir, the subagent could. These tests pin the inheritance.
func TestSubagentToolsInheritParentPolicy(t *testing.T) {
	workspace := t.TempDir()
	dataDir := t.TempDir()

	// The denied files must exist. Without them a read or a `cat` fails because the file
	// is missing, the assertion sees IsError and passes — for the wrong reason. Verified
	// by disabling the fix: only the write_file assertion went red until these existed.
	if err := os.WriteFile(filepath.Join(workspace, "config.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "config.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	parent := tools.NewRegistry()
	parentRead := tools.NewReadFileTool(workspace, true)
	parentRead.DenyPaths("config.json", "memory.db")
	parentWrite := tools.NewWriteFileTool(workspace, true)
	parentWrite.DenyPaths("config.json", "delegate/")
	parentList := tools.NewListFilesTool(workspace, true)
	parentList.DenyPaths("config.json")
	parentExec := tools.NewExecTool(workspace, true)
	parentExec.DenyPaths(dataDir)
	parent.Register(parentRead)
	parent.Register(parentWrite)
	parent.Register(parentList)
	parent.Register(parentExec)

	reg, execTool := buildSubagentToolsRegistry(parent, workspace, true, nil, nil)
	if reg == nil || execTool == nil {
		t.Fatal("buildSubagentToolsRegistry returned nil")
	}

	ctx := context.Background()

	// exec: a command referencing the parent's denied data dir must be refused.
	res := execTool.Execute(ctx, map[string]any{"command": "cat " + dataDir + "/config.json"})
	if res == nil {
		t.Fatal("exec returned nil result")
	}
	if !res.IsError {
		t.Errorf("subagent exec reached the parent's denied data dir: %+v", res)
	}

	// read_file: the parent's denied prefixes must apply.
	rf, ok := reg.Get("read_file")
	if !ok {
		t.Fatal("read_file missing from subagent registry")
	}
	res = rf.Execute(ctx, map[string]any{"path": "config.json"})
	if res == nil || !res.IsError {
		t.Errorf("subagent read_file reached config.json: %+v", res)
	}

	// write_file: same.
	wf, ok := reg.Get("write_file")
	if !ok {
		t.Fatal("write_file missing from subagent registry")
	}
	res = wf.Execute(ctx, map[string]any{"path": "config.json", "content": "x"})
	if res == nil || !res.IsError {
		t.Errorf("subagent write_file reached config.json: %+v", res)
	}
}

// Inheriting denials without the parent's exemptions would leave the subagent unable to
// read the skills it is told to use: the skills store sits under the denied data dir.
func TestSubagentExecInheritsParentPathExemptions(t *testing.T) {
	workspace := t.TempDir()
	dataDir := t.TempDir()
	skillsStore := dataDir + "/skills-store/"

	if err := os.MkdirAll(filepath.Join(skillsStore, "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillsStore, "demo", "SKILL.md"), []byte("# demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	parent := tools.NewRegistry()
	parentExec := tools.NewExecTool(workspace, true)
	parentExec.DenyPaths(dataDir)
	parentExec.AllowPathExemptions(skillsStore)
	parent.Register(parentExec)

	_, execTool := buildSubagentToolsRegistry(parent, workspace, true, nil, nil)

	res := execTool.Execute(context.Background(), map[string]any{"command": "cat " + skillsStore + "demo/SKILL.md"})
	if res == nil {
		t.Fatal("exec returned nil result")
	}
	if res.IsError {
		t.Errorf("exemption did not carry over; reading the skills store was refused: %+v", res)
	}
	if !strings.Contains(res.ForLLM, "# demo") {
		t.Errorf("expected the skill file contents, got %q", res.ForLLM)
	}
}

// A parent registry without the tools, or with nothing configured, must not panic.
func TestSubagentToolsInheritTolerantOfMissingParentTools(t *testing.T) {
	workspace := t.TempDir()

	reg, execTool := buildSubagentToolsRegistry(tools.NewRegistry(), workspace, true, nil, nil)
	if reg == nil || execTool == nil {
		t.Fatal("expected a usable registry from an empty parent")
	}
	if _, ok := reg.Get("read_file"); !ok {
		t.Error("read_file should still be registered")
	}
}
