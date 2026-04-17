package skills

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// fixturePip3Path returns the absolute path to the fixture pip3 script.
// Uses runtime.Caller so the path is correct regardless of test working directory.
func fixturePip3Path(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "testdata", "pip", "bin", "pip3")
}

// setupFixturePip overrides pipBinary and pipLookPath to use the bundled fixture script.
func setupFixturePip(t *testing.T) {
	t.Helper()
	origBinary := pipBinary
	origLookPath := pipLookPath
	pipBinary = fixturePip3Path(t)
	pipLookPath = func(string) (string, error) { return pipBinary, nil }
	t.Cleanup(func() {
		pipBinary = origBinary
		pipLookPath = origLookPath
	})
}

// writeExecScript writes a shell script to path and makes it executable.
func writeExecScript(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("writeExecScript: %v", err)
	}
}

// TestPipChecker_LookPathMiss verifies that a missing pip3 binary returns
// Available:false with nil Err and empty Updates — not an error condition.
func TestPipChecker_LookPathMiss(t *testing.T) {
	origLookPath := pipLookPath
	pipLookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	t.Cleanup(func() { pipLookPath = origLookPath })

	c := NewPipUpdateChecker()
	res := c.Check(context.Background(), nil)

	if res.Source != "pip" {
		t.Fatalf("Source = %q, want %q", res.Source, "pip")
	}
	if res.Available {
		t.Fatal("Available = true, want false when pip3 not found")
	}
	if res.Err != nil {
		t.Fatalf("Err = %v, want nil", res.Err)
	}
	if len(res.Updates) != 0 {
		t.Fatalf("Updates len = %d, want 0", len(res.Updates))
	}
}

// TestPipChecker_ParseFixture verifies that the checker correctly parses the
// outdated-23.3.json fixture (3 packages, one with a pre-release current version).
func TestPipChecker_ParseFixture(t *testing.T) {
	setupFixturePip(t)

	c := NewPipUpdateChecker()
	res := c.Check(context.Background(), nil)

	if !res.Available {
		t.Fatal("Available = false, want true")
	}
	if res.Err != nil {
		t.Fatalf("unexpected Err: %v", res.Err)
	}
	if len(res.Updates) != 3 {
		t.Fatalf("Updates len = %d, want 3", len(res.Updates))
	}

	// Build lookup map for assertions.
	byName := make(map[string]UpdateInfo, len(res.Updates))
	for _, u := range res.Updates {
		byName[u.Name] = u
	}

	// setuptools: stable current version — no preRelease flag.
	st, ok := byName["setuptools"]
	if !ok {
		t.Fatal("missing 'setuptools' in Updates")
	}
	if st.Source != "pip" {
		t.Errorf("setuptools Source = %q, want %q", st.Source, "pip")
	}
	if st.CurrentVersion != "65.5.0" {
		t.Errorf("setuptools CurrentVersion = %q, want %q", st.CurrentVersion, "65.5.0")
	}
	if st.LatestVersion != "68.2.2" {
		t.Errorf("setuptools LatestVersion = %q, want %q", st.LatestVersion, "68.2.2")
	}
	if v, _ := st.Meta["preRelease"].(bool); v {
		t.Error("setuptools should NOT have preRelease=true")
	}
	if ft, _ := st.Meta["filetype"].(string); ft != "wheel" {
		t.Errorf("setuptools filetype = %q, want %q", ft, "wheel")
	}

	// pip package: stable current version.
	pipPkg, ok := byName["pip"]
	if !ok {
		t.Fatal("missing 'pip' in Updates")
	}
	if pipPkg.LatestVersion != "23.3.1" {
		t.Errorf("pip LatestVersion = %q, want %q", pipPkg.LatestVersion, "23.3.1")
	}

	// torch: current version is pre-release (2.0.0rc1) → preRelease=true in Meta.
	torch, ok := byName["torch"]
	if !ok {
		t.Fatal("missing 'torch' in Updates")
	}
	if torch.CurrentVersion != "2.0.0rc1" {
		t.Errorf("torch CurrentVersion = %q, want %q", torch.CurrentVersion, "2.0.0rc1")
	}
	preRel, _ := torch.Meta["preRelease"].(bool)
	if !preRel {
		t.Error("torch should have preRelease=true because current version is rc1")
	}
}

// TestPipChecker_EmptyResult verifies that zero outdated packages is valid
// (Available:true, empty Updates, nil Err).
func TestPipChecker_EmptyResult(t *testing.T) {
	origBinary := pipBinary
	origLookPath := pipLookPath

	script := filepath.Join(t.TempDir(), "pip3")
	writeExecScript(t, script, "#!/bin/sh\necho '[]'\n")
	pipBinary = script
	pipLookPath = func(string) (string, error) { return script, nil }
	t.Cleanup(func() {
		pipBinary = origBinary
		pipLookPath = origLookPath
	})

	c := NewPipUpdateChecker()
	res := c.Check(context.Background(), nil)

	if !res.Available {
		t.Fatal("Available = false, want true for empty-but-successful check")
	}
	if res.Err != nil {
		t.Fatalf("unexpected Err: %v", res.Err)
	}
	if len(res.Updates) != 0 {
		t.Fatalf("Updates len = %d, want 0", len(res.Updates))
	}
}

// TestPipChecker_ExecError verifies that a non-zero pip exit sets Err and
// keeps Available:true (source is reachable, command failed transiently).
func TestPipChecker_ExecError(t *testing.T) {
	origBinary := pipBinary
	origLookPath := pipLookPath

	script := filepath.Join(t.TempDir(), "pip3")
	writeExecScript(t, script, "#!/bin/sh\necho 'internal error' >&2\nexit 1\n")
	pipBinary = script
	pipLookPath = func(string) (string, error) { return script, nil }
	t.Cleanup(func() {
		pipBinary = origBinary
		pipLookPath = origLookPath
	})

	c := NewPipUpdateChecker()
	res := c.Check(context.Background(), nil)

	if !res.Available {
		t.Fatal("Available = false, want true (source exists but errored)")
	}
	if res.Err == nil {
		t.Fatal("Err = nil, want non-nil on exec failure")
	}
}

// TestMergePipResults verifies union-by-name and higher-latest-version preference.
func TestMergePipResults(t *testing.T) {
	primary := []pipOutdatedEntry{
		{Name: "requests", Version: "2.28.0", LatestVersion: "2.31.0", LatestFiletype: "wheel"},
		{Name: "numpy", Version: "1.24.0", LatestVersion: "1.25.0", LatestFiletype: "wheel"},
	}
	secondary := []pipOutdatedEntry{
		{Name: "requests", Version: "2.28.0", LatestVersion: "2.32.0rc1", LatestFiletype: "wheel"},
		{Name: "scipy", Version: "1.10.0", LatestVersion: "1.11.0", LatestFiletype: "wheel"},
	}

	merged := mergePipResults(primary, secondary)

	if len(merged) != 3 {
		t.Fatalf("merged len = %d, want 3", len(merged))
	}
	byName := make(map[string]pipOutdatedEntry, len(merged))
	for _, e := range merged {
		byName[e.Name] = e
	}

	// requests: secondary has higher latest_version string.
	if req := byName["requests"]; req.LatestVersion != "2.32.0rc1" {
		t.Errorf("requests LatestVersion = %q, want %q", req.LatestVersion, "2.32.0rc1")
	}
	if _, ok := byName["numpy"]; !ok {
		t.Error("numpy missing from merge result")
	}
	if _, ok := byName["scipy"]; !ok {
		t.Error("scipy missing from merge result")
	}
}
