package skills

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"
)

// writeFakeBin puts an executable shell script named name in a temp dir and
// makes that dir the only entry on PATH.
func writeFakeBin(t *testing.T, name, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake shell-script binaries are not supported on Windows")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

// sleepPath returns the absolute path of sleep so a fake PATH cannot hide it.
func sleepPath(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("sleep-based fake probes are not supported on Windows")
	}
	path, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep not available on this system")
	}
	return path
}

// A probe killed by the timeout must not report the packages as missing.
// exec replaces the shell so the kill on deadline closes stdout; sleep is
// resolved through PATH before the fake PATH is installed.
func TestCheckPythonPackagesTimeoutIsNotMissing(t *testing.T) {
	sleep := sleepPath(t)
	writeFakeBin(t, "python3", "#!/bin/sh\nexec "+sleep+" 7\n")
	t.Setenv("GOCLAW_DEP_CHECK_TIMEOUT", "1s")

	res := checkPythonPackages(context.Background(), []string{"anthropic"}, "")
	if len(res.missing) != 0 {
		t.Fatalf("timed-out check reported missing packages: got %v, want none", res.missing)
	}
	if !res.unknown {
		t.Fatal("timed-out check did not report unknown")
	}

	ok, missing := CheckSkillDeps(&SkillManifest{RequiresPython: []string{"anthropic"}})
	if !ok || len(missing) != 0 {
		t.Fatalf("legacy wrapper = (%t, %v), want fail-open (true, none)", ok, missing)
	}
}

// Genuine import failures must still surface.
func TestCheckPythonPackagesReportsImportError(t *testing.T) {
	writeFakeBin(t, "python3", "#!/bin/sh\necho yaml\n")

	res := checkPythonPackages(context.Background(), []string{"yaml"}, "")
	if want := []string{"pip:pyyaml"}; !reflect.DeepEqual(res.missing, want) {
		t.Fatalf("got %v, want %v", res.missing, want)
	}
	if res.unknown {
		t.Fatal("import error reported unknown, want known missing")
	}
}

func TestCheckNodePackagesTimeoutIsNotMissing(t *testing.T) {
	sleep := sleepPath(t)
	writeFakeBin(t, "node", "#!/bin/sh\nexec "+sleep+" 7\n")
	t.Setenv("GOCLAW_DEP_CHECK_TIMEOUT", "1s")

	res := checkNodePackages(context.Background(), []string{"left-pad"}, "")
	if len(res.missing) != 0 {
		t.Fatalf("timed-out check reported missing packages: got %v, want none", res.missing)
	}
	if !res.unknown {
		t.Fatal("timed-out check did not report unknown")
	}
}

// A cancelled caller context must stop the probe before it can archive a skill.
func TestCheckSkillDepsContextCancelledIsUnknown(t *testing.T) {
	writeFakeBin(t, "python3", "#!/bin/sh\necho\n")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	res := CheckSkillDepsContext(ctx, &SkillManifest{RequiresPython: []string{"anthropic"}})
	if res.Status != DepCheckUnknown {
		t.Fatalf("status = %v, want unknown", res.Status)
	}
	if len(res.Missing) != 0 {
		t.Fatalf("missing = %v, want none", res.Missing)
	}
}

// A pass-level deadline cancelled mid-check must not be reported as missing.
func TestCheckSkillDepsContextDeadlineIsUnknown(t *testing.T) {
	sleep := sleepPath(t)
	writeFakeBin(t, "python3", "#!/bin/sh\nexec "+sleep+" 7\n")
	t.Setenv("GOCLAW_DEP_CHECK_TIMEOUT", "1s")

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	res := CheckSkillDepsContext(ctx, &SkillManifest{RequiresPython: []string{"anthropic"}})
	if res.Status != DepCheckUnknown {
		t.Fatalf("status = %v, want unknown", res.Status)
	}
	if len(res.Missing) != 0 {
		t.Fatalf("missing = %v, want none", res.Missing)
	}
}

func TestDepCheckTimeout(t *testing.T) {
	cases := []struct {
		name string
		env  string
		want time.Duration
	}{
		{name: "default", env: "", want: defaultDepCheckTimeout},
		{name: "valid", env: "60s", want: 60 * time.Second},
		{name: "valid sub-second", env: "1500ms", want: 1500 * time.Millisecond},
		{name: "invalid", env: "60", want: defaultDepCheckTimeout},
		{name: "zero", env: "0s", want: defaultDepCheckTimeout},
		{name: "negative", env: "-5s", want: defaultDepCheckTimeout},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GOCLAW_DEP_CHECK_TIMEOUT", tc.env)
			if got := depCheckTimeout(); got != tc.want {
				t.Fatalf("depCheckTimeout() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestImportToPipName(t *testing.T) {
	cases := []struct {
		importName string
		want       string
	}{
		{"cv2", "opencv-python"},
		{"PIL", "Pillow"},
		{"yaml", "pyyaml"},
		{"sklearn", "scikit-learn"},
		{"bs4", "beautifulsoup4"},
		{"dateutil", "python-dateutil"},
		{"dotenv", "python-dotenv"},
		{"pptx", "python-pptx"},
		{"docx", "python-docx"},
		{"attr", "attrs"},
		{"gi", "PyGObject"},
		{"psycopg2", "psycopg2-binary"},
		{"psycopg", "psycopg[binary]"},
		{"MySQLdb", "mysqlclient"},
		{"Crypto", "pycryptodome"},
		{"serial", "pyserial"},
		{"skimage", "scikit-image"},
		{"Levenshtein", "python-Levenshtein"},
		{"requests", "requests"},
		{"numpy", "numpy"},
		{"", ""},
	}
	for _, tc := range cases {
		t.Run(tc.importName, func(t *testing.T) {
			if got := importToPipName(tc.importName); got != tc.want {
				t.Errorf("importToPipName(%q) = %q, want %q", tc.importName, got, tc.want)
			}
		})
	}
}

func TestCheckSkillDepsContextKnownMissingAndTimeoutIsIncomplete(t *testing.T) {
	sleep := sleepPath(t)
	writeFakeBin(t, "python3", "#!/bin/sh\nexec "+sleep+" 7\n")
	t.Setenv("GOCLAW_DEP_CHECK_TIMEOUT", "1s")
	result := CheckSkillDepsContext(context.Background(), &SkillManifest{Requires: []string{"missing_goclaw_test_binary_xyz"}, RequiresPython: []string{"anthropic"}})
	if result.Status != DepCheckMissing || !result.Incomplete || !reflect.DeepEqual(result.Missing, []string{"missing_goclaw_test_binary_xyz"}) {
		t.Fatalf("partial result=%+v", result)
	}
}
