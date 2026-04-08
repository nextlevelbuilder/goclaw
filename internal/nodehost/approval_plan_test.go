package nodehost

import (
	"os"
	"path/filepath"
	"testing"
)

// --- Helper: create temp dir with a fake runtime bin ---

func writeFakeRuntimeBin(t *testing.T, binDir, binName string) {
	t.Helper()
	runtimePath := filepath.Join(binDir, binName)
	os.WriteFile(runtimePath, []byte("#!/bin/sh\nexit 0\n"), 0o755)
}

func withFakeRuntimeBins(t *testing.T, binNames []string, fn func()) {
	t.Helper()
	tmp := t.TempDir()
	binDir := filepath.Join(tmp, "bin")
	os.MkdirAll(binDir, 0o755)
	for _, name := range binNames {
		writeFakeRuntimeBin(t, binDir, name)
	}
	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", binDir+":"+oldPath)
	fn()
}

func writeScriptFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	os.WriteFile(p, []byte(body), 0o755)
	return p
}

// --- Hardening tests (ported from exec-policy.test.ts) ---

func TestHarden_PreservesShellWrapperArgv(t *testing.T) {
	tmp := t.TempDir()
	argv := []string{"env", "sh", "-c", "echo SAFE"}
	r := HardenApprovedExecutionPaths(true, argv, nil, tmp)
	if !r.Ok {
		t.Fatalf("expected ok, got: %s", r.Message)
	}
	assertStringSliceEqual(t, argv, r.Argv)
}

func TestHarden_PreservesDispatchWrapperArgv(t *testing.T) {
	tmp := t.TempDir()
	argv := []string{"env", "tr", "a", "b"}
	r := HardenApprovedExecutionPaths(true, argv, nil, tmp)
	if !r.Ok {
		t.Fatalf("expected ok, got: %s", r.Message)
	}
	assertStringSliceEqual(t, argv, r.Argv)
	if r.ArgvChanged {
		t.Error("expected argvChanged=false")
	}
}

func TestHarden_SkipsWhenNotApproved(t *testing.T) {
	argv := []string{"echo", "hello"}
	r := HardenApprovedExecutionPaths(false, argv, nil, "/tmp")
	if !r.Ok {
		t.Fatal("expected ok")
	}
	assertStringSliceEqual(t, argv, r.Argv)
}

// --- Mutable file operand tests ---

type runtimeFixture struct {
	name             string
	binNames         []string
	argv             []string
	scriptName       string
	scriptBody       string
	expectedArgvIdx  int
}

func TestMutableOperand_ShellScript(t *testing.T) {
	tmp := t.TempDir()
	writeScriptFile(t, tmp, "run.sh", "#!/bin/sh\necho SAFE\n")

	result := BuildSystemRunApprovalPlan(
		[]string{"/bin/sh", "./run.sh"}, nil, &tmp, nil, nil,
	)
	if !result.Ok {
		t.Fatalf("expected ok, got: %s", result.Message)
	}
	if result.Plan.MutableFileOperand == nil {
		t.Fatal("expected mutable file operand")
	}
	if result.Plan.MutableFileOperand.ArgvIndex != 1 {
		t.Errorf("argvIndex = %d, want 1", result.Plan.MutableFileOperand.ArgvIndex)
	}
}

func TestMutableOperand_Runtimes(t *testing.T) {
	fixtures := []runtimeFixture{
		{name: "tsx direct file", binNames: []string{"tsx"}, argv: []string{"tsx", "./run.ts"},
			scriptName: "run.ts", scriptBody: "console.log(\"SAFE\");\n", expectedArgvIdx: 1},
		{name: "jiti direct file", binNames: []string{"jiti"}, argv: []string{"jiti", "./run.ts"},
			scriptName: "run.ts", scriptBody: "console.log(\"SAFE\");\n", expectedArgvIdx: 1},
		{name: "ts-node direct file", binNames: []string{"ts-node"}, argv: []string{"ts-node", "./run.ts"},
			scriptName: "run.ts", scriptBody: "console.log(\"SAFE\");\n", expectedArgvIdx: 1},
		{name: "vite-node direct file", binNames: []string{"vite-node"}, argv: []string{"vite-node", "./run.ts"},
			scriptName: "run.ts", scriptBody: "console.log(\"SAFE\");\n", expectedArgvIdx: 1},
		{name: "bun direct file", binNames: []string{"bun"}, argv: []string{"bun", "./run.ts"},
			scriptName: "run.ts", scriptBody: "console.log(\"SAFE\");\n", expectedArgvIdx: 1},
		{name: "bun run file", binNames: []string{"bun"}, argv: []string{"bun", "run", "./run.ts"},
			scriptName: "run.ts", scriptBody: "console.log(\"SAFE\");\n", expectedArgvIdx: 2},
		{name: "pnpm exec tsx file", binNames: []string{"pnpm", "tsx"}, argv: []string{"pnpm", "exec", "tsx", "./run.ts"},
			scriptName: "run.ts", scriptBody: "console.log(\"SAFE\");\n", expectedArgvIdx: 3},
		{name: "npx tsx file", binNames: []string{"npx", "tsx"}, argv: []string{"npx", "tsx", "./run.ts"},
			scriptName: "run.ts", scriptBody: "console.log(\"SAFE\");\n", expectedArgvIdx: 2},
		{name: "bunx tsx file", binNames: []string{"bunx", "tsx"}, argv: []string{"bunx", "tsx", "./run.ts"},
			scriptName: "run.ts", scriptBody: "console.log(\"SAFE\");\n", expectedArgvIdx: 2},
		{name: "npm exec tsx file", binNames: []string{"npm", "tsx"}, argv: []string{"npm", "exec", "--", "tsx", "./run.ts"},
			scriptName: "run.ts", scriptBody: "console.log(\"SAFE\");\n", expectedArgvIdx: 4},
	}

	for _, f := range fixtures {
		t.Run(f.name, func(t *testing.T) {
			withFakeRuntimeBins(t, f.binNames, func() {
				tmp := t.TempDir()
				writeScriptFile(t, tmp, f.scriptName, f.scriptBody)

				result := BuildSystemRunApprovalPlan(f.argv, nil, &tmp, nil, nil)
				if !result.Ok {
					t.Fatalf("expected ok, got: %s", result.Message)
				}
				if result.Plan.MutableFileOperand == nil {
					t.Fatal("expected mutable file operand")
				}
				if result.Plan.MutableFileOperand.ArgvIndex != f.expectedArgvIdx {
					t.Errorf("argvIndex = %d, want %d", result.Plan.MutableFileOperand.ArgvIndex, f.expectedArgvIdx)
				}
			})
		})
	}
}

// --- Unsafe runtime invocation tests ---

func TestUnsafeRuntime_BunPackageScript(t *testing.T) {
	withFakeRuntimeBins(t, []string{"bun"}, func() {
		tmp := t.TempDir()
		expectRuntimeApprovalDenied(t, []string{"bun", "run", "dev"}, tmp)
	})
}

func TestUnsafeRuntime_RubyRequirePreload(t *testing.T) {
	withFakeRuntimeBins(t, []string{"ruby"}, func() {
		tmp := t.TempDir()
		writeScriptFile(t, tmp, "safe.rb", "puts \"SAFE\"\n")
		expectRuntimeApprovalDenied(t, []string{"ruby", "-r", "attacker", "./safe.rb"}, tmp)
	})
}

func TestUnsafeRuntime_RubyLoadPath(t *testing.T) {
	withFakeRuntimeBins(t, []string{"ruby"}, func() {
		tmp := t.TempDir()
		writeScriptFile(t, tmp, "safe.rb", "puts \"SAFE\"\n")
		expectRuntimeApprovalDenied(t, []string{"ruby", "-I.", "./safe.rb"}, tmp)
	})
}

func TestUnsafeRuntime_PerlModulePreload(t *testing.T) {
	withFakeRuntimeBins(t, []string{"perl"}, func() {
		tmp := t.TempDir()
		writeScriptFile(t, tmp, "safe.pl", "print \"SAFE\\n\";\n")
		expectRuntimeApprovalDenied(t, []string{"perl", "-MPreload", "./safe.pl"}, tmp)
	})
}

func TestUnsafeRuntime_PerlLoadPath(t *testing.T) {
	withFakeRuntimeBins(t, []string{"perl"}, func() {
		tmp := t.TempDir()
		writeScriptFile(t, tmp, "safe.pl", "print \"SAFE\\n\";\n")
		expectRuntimeApprovalDenied(t, []string{"perl", "-Ilib", "./safe.pl"}, tmp)
	})
}

func TestUnsafeRuntime_ShellPayloadHidesInterpreter(t *testing.T) {
	withFakeRuntimeBins(t, []string{"node"}, func() {
		tmp := t.TempDir()
		writeScriptFile(t, tmp, "run.js", "console.log(\"SAFE\")\n")
		expectRuntimeApprovalDenied(t, []string{"sh", "-lc", "node ./run.js"}, tmp)
	})
}

// --- Shell option-value tests ---

func TestShellOptionValue_CapuresRealOperand(t *testing.T) {
	tmp := t.TempDir()
	writeScriptFile(t, tmp, "run.sh", "#!/bin/sh\necho SAFE\n")
	writeScriptFile(t, tmp, "errexit", "decoy\n")

	snap := ResolveMutableFileOperandSnapshot(
		[]string{"/bin/bash", "-o", "errexit", "./run.sh"}, tmp, nil,
	)
	if !snap.Ok {
		t.Fatalf("expected ok, got: %s", snap.Message)
	}
	if snap.Snapshot == nil {
		t.Fatal("expected snapshot")
	}
	if snap.Snapshot.ArgvIndex != 3 {
		t.Errorf("argvIndex = %d, want 3", snap.Snapshot.ArgvIndex)
	}
}

// --- Revalidation tests ---

func TestRevalidate_MutableFileOperand_Passes(t *testing.T) {
	tmp := t.TempDir()
	scriptPath := writeScriptFile(t, tmp, "run.sh", "#!/bin/sh\necho SAFE\n")
	hash, _ := hashFileContents(scriptPath)
	realPath, _ := filepath.EvalSymlinks(scriptPath)

	ok := RevalidateApprovedMutableFileOperand(
		&SystemRunApprovalFileOperand{ArgvIndex: 1, Path: realPath, SHA256: hash},
		[]string{"/bin/sh", "./run.sh"}, tmp,
	)
	if !ok {
		t.Error("expected revalidation to pass")
	}
}

func TestRevalidate_MutableFileOperand_FailsOnModification(t *testing.T) {
	tmp := t.TempDir()
	scriptPath := writeScriptFile(t, tmp, "run.sh", "#!/bin/sh\necho SAFE\n")
	hash, _ := hashFileContents(scriptPath)
	realPath, _ := filepath.EvalSymlinks(scriptPath)

	// Modify the file.
	os.WriteFile(scriptPath, []byte("#!/bin/sh\necho PWNED\n"), 0o755)

	ok := RevalidateApprovedMutableFileOperand(
		&SystemRunApprovalFileOperand{ArgvIndex: 1, Path: realPath, SHA256: hash},
		[]string{"/bin/sh", "./run.sh"}, tmp,
	)
	if ok {
		t.Error("expected revalidation to fail after modification")
	}
}

func TestRevalidate_CwdSnapshot(t *testing.T) {
	tmp := t.TempDir()
	snap, msg := ResolveCanonicalApprovalCwd(tmp)
	if msg != "" {
		t.Fatalf("resolve cwd: %s", msg)
	}
	if !RevalidateApprovedCwdSnapshot(snap) {
		t.Error("expected cwd revalidation to pass")
	}
}

// --- SplitShellArgs tests ---

func TestSplitShellArgs(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{`echo hello`, []string{"echo", "hello"}},
		{`echo "hello world"`, []string{"echo", "hello world"}},
		{`echo 'hello world'`, []string{"echo", "hello world"}},
		{`echo hello\ world`, []string{"echo", "hello world"}},
		{`echo "it\"s"`, []string{"echo", `it"s`}},
		{`cmd # comment`, []string{"cmd"}},
		{``, nil}, // empty returns nil (no tokens = nil slice from append)
	}
	for _, tt := range tests {
		got := SplitShellArgs(tt.input)
		if tt.want == nil {
			if len(got) > 0 {
				t.Errorf("SplitShellArgs(%q) = %v, want nil/empty", tt.input, got)
			}
			continue
		}
		assertStringSliceEqual(t, tt.want, got)
	}
}

func TestSplitShellArgs_UnterminatedQuote(t *testing.T) {
	if got := SplitShellArgs(`echo "unterminated`); got != nil {
		t.Errorf("expected nil for unterminated quote, got %v", got)
	}
}

// --- NormalizeExecutableToken tests ---

func TestNormalizeExecutableToken(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"/usr/bin/node", "node"},
		{"C:\\Program Files\\node.exe", "node"},
		{"./pnpm.js", "pnpm.js"},
		{"tsx", "tsx"},
		{"NODE.CMD", "node"},
	}
	for _, tt := range tests {
		got := NormalizeExecutableToken(tt.input)
		if got != tt.want {
			t.Errorf("NormalizeExecutableToken(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// --- FormatExecCommand tests ---

func TestFormatExecCommand(t *testing.T) {
	tests := []struct {
		argv []string
		want string
	}{
		{[]string{"echo", "hello"}, "echo hello"},
		{[]string{"sh", "-c", "echo SAFE"}, `sh -c "echo SAFE"`},
		{[]string{""}, `""`},
	}
	for _, tt := range tests {
		got := FormatExecCommand(tt.argv)
		if got != tt.want {
			t.Errorf("FormatExecCommand(%v) = %q, want %q", tt.argv, got, tt.want)
		}
	}
}

// --- helpers ---

func expectRuntimeApprovalDenied(t *testing.T, command []string, cwd string) {
	t.Helper()
	result := BuildSystemRunApprovalPlan(command, nil, &cwd, nil, nil)
	if result.Ok {
		t.Fatalf("expected denial, but got ok with plan: %+v", result.Plan)
	}
	want := "SYSTEM_RUN_DENIED: approval cannot safely bind this interpreter/runtime command"
	if result.Message != want {
		t.Errorf("message = %q, want %q", result.Message, want)
	}
}

func assertStringSliceEqual(t *testing.T, want, got []string) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("slice length %d != %d\nwant: %v\ngot:  %v", len(want), len(got), want, got)
	}
	for i := range want {
		if want[i] != got[i] {
			t.Errorf("[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
