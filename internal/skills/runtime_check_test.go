package skills

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCheckRuntimesMissingBinaries(t *testing.T) {
	for _, name := range []string{"empty PATH", "isolated PATH"} {
		t.Run(name, func(t *testing.T) {
			dir := isolatedRuntimePath(t)
			if name == "empty PATH" {
				t.Setenv("PATH", "")
			}
			t.Chdir(dir)

			assertRuntimeAvailability(t, CheckRuntimes(), false, map[string]bool{
				"python3": false,
				"pip3":    false,
				"node":    false,
				"npm":     false,
			})
		})
	}
}

func TestCheckRuntimesMissingCriticalRuntime(t *testing.T) {
	for _, missing := range []string{"python3", "pip3"} {
		t.Run(missing, func(t *testing.T) {
			dir := isolatedRuntimePath(t)
			want := map[string]bool{}
			for _, name := range []string{"python3", "pip3", "node", "npm"} {
				want[name] = name != missing
				if name != missing {
					writeRuntimeLookupFixture(t, dir, name)
				}
			}
			assertRuntimeAvailability(t, CheckRuntimes(), false, want)
		})
	}
}

func TestCheckRuntimesFoundBinaries(t *testing.T) {
	for _, includeOptional := range []bool{false, true} {
		name := "critical only"
		if includeOptional {
			name = "all runtimes"
		}
		t.Run(name, func(t *testing.T) {
			dir := isolatedRuntimePath(t)
			want := map[string]bool{"python3": true, "pip3": true, "node": includeOptional, "npm": includeOptional}
			for name, available := range want {
				if available {
					writeRuntimeLookupFixture(t, dir, name)
				}
			}
			assertRuntimeAvailability(t, CheckRuntimes(), true, want)
		})
	}
}

func TestCheckRuntimesWindowsAliases(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only Python and pip aliases")
	}

	dir := isolatedRuntimePath(t)
	writeRuntimeLookupFixture(t, dir, "python")
	writeRuntimeLookupFixture(t, dir, "pip")
	assertRuntimeAvailability(t, CheckRuntimes(), true, map[string]bool{
		"python3": true,
		"pip3":    true,
		"node":    false,
		"npm":     false,
	})
}

func TestFirstAvailableBinary(t *testing.T) {
	dir := isolatedRuntimePath(t)
	first := writeRuntimeLookupFixture(t, dir, "runtime-first")
	second := writeRuntimeLookupFixture(t, dir, "runtime-second")
	for _, tc := range []struct {
		name  string
		names []string
		want  string
	}{
		{"missing", []string{"runtime-missing"}, ""},
		{"found", []string{"runtime-first"}, first},
		{"fallback", []string{"runtime-missing", "runtime-second"}, second},
		{"prefer first", []string{"runtime-first", "runtime-second"}, first},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := firstAvailableBinary(tc.names...); got != tc.want {
				t.Fatalf("firstAvailableBinary(%q) = %q, want %q", tc.names, got, tc.want)
			}
		})
	}
}

func isolatedRuntimePath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	if runtime.GOOS == "windows" {
		t.Setenv("PATHEXT", ".EXE")
	}
	return dir
}

func writeRuntimeLookupFixture(t *testing.T, dir, name string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(dir, name)
	// Lookup-only fixtures also exercise availability when version probing fails.
	if err := os.WriteFile(path, []byte("runtime lookup fixture\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertRuntimeAvailability(t *testing.T, status *RuntimeStatus, ready bool, want map[string]bool) {
	t.Helper()
	if status.Ready != ready {
		t.Errorf("Ready = %v, want %v", status.Ready, ready)
	}
	seen := map[string]bool{}
	for _, info := range status.Runtimes {
		available, ok := want[info.Name]
		if !ok {
			continue
		}
		seen[info.Name] = true
		if info.Available != available {
			t.Errorf("%s Available = %v, want %v", info.Name, info.Available, available)
		}
		if info.Version != "" {
			t.Errorf("%s Version = %q, want empty for lookup-only fixture", info.Name, info.Version)
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("missing runtime status for %s", name)
		}
	}
}
