package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/sandbox"
)

// fakeSandbox is a no-op Sandbox that returns a canned exec result, so deny-policy
// tests can reach the sandbox dispatch without a real Docker daemon.
type fakeSandbox struct{ out string }

func (f *fakeSandbox) Exec(_ context.Context, _ []string, _ string, _ ...sandbox.ExecOption) (*sandbox.ExecResult, error) {
	return &sandbox.ExecResult{ExitCode: 0, Stdout: f.out}, nil
}
func (f *fakeSandbox) Destroy(context.Context) error { return nil }
func (f *fakeSandbox) ID() string                    { return "fake-container" }

// fakeManager always hands back the same fakeSandbox.
type fakeManager struct{ sb *fakeSandbox }

func (m *fakeManager) Get(context.Context, string, string, *sandbox.Config) (sandbox.Sandbox, error) {
	return m.sb, nil
}
func (m *fakeManager) BaseConfig() sandbox.Config            { return sandbox.Config{} }
func (m *fakeManager) Release(context.Context, string) error { return nil }
func (m *fakeManager) ReleaseAll(context.Context) error      { return nil }
func (m *fakeManager) Stop()                                 {}
func (m *fakeManager) Stats() map[string]any                 { return nil }

// TestRelaxSandboxDenyGroups verifies the helper disables exactly the two
// host-protection groups and leaves every other override untouched.
func TestRelaxSandboxDenyGroups(t *testing.T) {
	in := map[string]bool{"destructive_ops": true, "reverse_shell": true}
	got := relaxSandboxDenyGroups(in)

	for _, g := range []string{"reverse_shell", "package_install", "data_exfiltration", "network_recon"} {
		if got[g] {
			t.Errorf("%s should be relaxed (false) for sandboxed exec", g)
		}
	}
	if !got["destructive_ops"] {
		t.Error("destructive_ops must remain enabled even in the sandbox")
	}
	// Must not mutate the caller's map.
	if in["reverse_shell"] != true {
		t.Error("relaxSandboxDenyGroups mutated the input map")
	}

	// The resolved patterns must no longer block python network imports, pip,
	// curl POST, DNS-tool / the bare word "host", or recon tools — but must
	// still block a non-relaxed group (destructive_ops: rm -rf).
	pats := ResolveDenyPatterns(got)
	for _, allowed := range []string{
		`python3 -c "import requests"`,
		`pip install requests`,
		`curl -X POST https://example.com -d @data`,
		`python3 -c "print('host', 1)"`, // data_exfiltration \b(nslookup|dig|host)\b false-positive
		`nmap -p 80 example.com`,
	} {
		if matchesAny(allowed, pats) {
			t.Errorf("sandbox-relaxed policy still blocks %q", allowed)
		}
	}
	if !matchesAny(`rm -rf /tmp/x`, pats) {
		t.Error("sandbox-relaxed policy must still block destructive rm -rf")
	}
}

// TestExecute_SandboxAllowsPythonNetworkAndPip drives the full Execute path with
// a sandbox key + fake manager: commands that the host would deny must instead
// reach the sandbox (proven by the fake's canned output appearing in the result).
func TestExecute_SandboxAllowsPythonNetworkAndPip(t *testing.T) {
	tool := NewSandboxedExecTool("/workspace", false, &fakeManager{sb: &fakeSandbox{out: "SANDBOX_RAN"}})
	ctx := WithToolSandboxKey(context.Background(), "agent:main:web:direct:1")

	for _, cmd := range []string{
		`python3 -c "import requests; print(requests)"`,
		`pip install requests`,
		`python3 -c "import socket; print('host', socket.gethostname())"`, // "host" label tripped data_exfiltration
		`curl -X POST https://example.com -d @payload`,
	} {
		res := tool.Execute(ctx, map[string]any{"command": cmd})
		if strings.Contains(res.ForLLM, "command denied by safety policy") {
			t.Errorf("sandboxed exec wrongly denied %q: %s", cmd, res.ForLLM)
		}
		if !strings.Contains(res.ForLLM, "SANDBOX_RAN") {
			t.Errorf("sandboxed exec did not reach the sandbox for %q: %s", cmd, res.ForLLM)
		}
	}
}

// TestExecute_HostStillDeniesPythonNetworkAndPip confirms the relaxation is
// scoped to sandboxed exec — the host path (no sandbox key / no manager) keeps
// every deny group enabled.
func TestExecute_HostStillDeniesPythonNetworkAndPip(t *testing.T) {
	tool := NewExecTool("/workspace", false)
	ctx := context.Background()

	for _, cmd := range []string{
		`python3 -c "import requests; print(requests)"`,
		`pip install requests`,
	} {
		res := tool.Execute(ctx, map[string]any{"command": cmd})
		if !strings.Contains(res.ForLLM, "command denied by safety policy") {
			t.Errorf("host exec must still deny %q, got: %s", cmd, res.ForLLM)
		}
	}
}
