package skills

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
)

// fixturNpmBin is the path to the fixture npm shell script.
const fixturNpmBin = "testdata/npm/bin/npm"

// restoreNpmLookPath resets npmLookPath to exec.LookPath after the test.
func restoreNpmLookPath(t *testing.T) {
	t.Helper()
	orig := npmLookPath
	t.Cleanup(func() { npmLookPath = orig })
}

// restoreNpmBinary resets npmBinary to "npm" after the test.
func restoreNpmBinary(t *testing.T) {
	t.Helper()
	orig := npmBinary
	t.Cleanup(func() { npmBinary = orig })
}

// useFixtureNpm sets npmBinary to the fixture script and npmLookPath to a stub
// that always succeeds. Registers cleanup via t.Cleanup.
func useFixtureNpm(t *testing.T) {
	t.Helper()
	restoreNpmBinary(t)
	restoreNpmLookPath(t)
	npmBinary = filepath.Join("testdata", "npm", "bin", "npm")
	npmLookPath = func(string) (string, error) { return npmBinary, nil }
}

// TestNpmChecker_LookPathMiss verifies that a missing npm binary results in
// Available:false, nil Err, and no Updates.
func TestNpmChecker_LookPathMiss(t *testing.T) {
	restoreNpmLookPath(t)
	npmLookPath = func(string) (string, error) { return "", exec.ErrNotFound }

	res := NewNpmUpdateChecker().Check(context.Background(), nil)
	if res.Source != "npm" {
		t.Fatalf("want source=npm, got %q", res.Source)
	}
	if res.Available {
		t.Fatal("want Available=false on LookPath miss")
	}
	if res.Err != nil {
		t.Fatalf("want nil Err on LookPath miss, got %v", res.Err)
	}
	if len(res.Updates) != 0 {
		t.Fatalf("want 0 Updates on LookPath miss, got %d", len(res.Updates))
	}
}

// TestNpmChecker_Exit0_NoUpdates verifies that exit 0 (all up to date) returns
// Available:true with no updates and no error.
func TestNpmChecker_Exit0_NoUpdates(t *testing.T) {
	useFixtureNpm(t)
	t.Setenv("FIXTURE_MODE", "empty") // exits 0

	res := NewNpmUpdateChecker().Check(context.Background(), nil)
	if !res.Available {
		t.Fatal("want Available=true")
	}
	if res.Err != nil {
		t.Fatalf("want nil Err, got %v", res.Err)
	}
	if len(res.Updates) != 0 {
		t.Fatalf("want 0 updates, got %d", len(res.Updates))
	}
}

// TestNpmChecker_Exit1WithOutdated verifies that exit 1 + valid JSON stdout +
// no "npm ERR!" stderr is parsed correctly. The fixture has 4 entries:
//   - typescript 5.0.0 → 5.5.0   (stable→stable, kept)
//   - @angular/core 16.0.0 → 17.0.0  (stable→stable, kept)
//   - lodash 4.17.20 → 4.17.21-beta.0  (stable→pre, SKIPPED by H5 gate)
//   - react-beta 19.0.0-beta.1 → 19.0.0-beta.3  (pre→pre, kept)
//
// Expected: 3 updates returned, lodash excluded.
func TestNpmChecker_Exit1WithOutdated(t *testing.T) {
	useFixtureNpm(t)
	t.Setenv("FIXTURE_MODE", "outdated")

	res := NewNpmUpdateChecker().Check(context.Background(), nil)
	if !res.Available {
		t.Fatal("want Available=true")
	}
	if res.Err != nil {
		t.Fatalf("want nil Err, got %v", res.Err)
	}
	if len(res.Updates) != 3 {
		t.Fatalf("want 3 updates (lodash skipped as stable→pre), got %d: %+v", len(res.Updates), res.Updates)
	}

	// Verify lodash is absent.
	for _, u := range res.Updates {
		if u.Name == "lodash" {
			t.Fatal("lodash must be excluded (stable current → pre-release latest)")
		}
	}

	// Verify react-beta (pre→pre) is included with preRelease meta.
	var foundReactBeta bool
	for _, u := range res.Updates {
		if u.Name == "react-beta" {
			foundReactBeta = true
			if v, ok := u.Meta["preRelease"].(bool); !ok || !v {
				t.Error("react-beta missing Meta[preRelease]=true")
			}
		}
	}
	if !foundReactBeta {
		t.Error("react-beta (pre→pre) must be included in updates")
	}
}

// TestNpmChecker_Exit1WithNpmErr verifies that exit 1 + "npm ERR!" in stderr
// is treated as a real error (Available:true, Err set, no Updates).
func TestNpmChecker_Exit1WithNpmErr(t *testing.T) {
	useFixtureNpm(t)
	t.Setenv("FIXTURE_MODE", "error")

	res := NewNpmUpdateChecker().Check(context.Background(), nil)
	if !res.Available {
		t.Fatal("want Available=true even on npm error")
	}
	if res.Err == nil {
		t.Fatal("want non-nil Err when stderr contains npm ERR!")
	}
	if len(res.Updates) != 0 {
		t.Fatalf("want 0 Updates on error, got %d", len(res.Updates))
	}
}

// TestNpmChecker_AmbiguousExit1 verifies that exit 1 with empty stdout and
// empty stderr is treated as no-updates (Available:true, nil Err, empty Updates).
func TestNpmChecker_AmbiguousExit1(t *testing.T) {
	useFixtureNpm(t)
	t.Setenv("FIXTURE_MODE", "ambiguous")

	res := NewNpmUpdateChecker().Check(context.Background(), nil)
	if !res.Available {
		t.Fatal("want Available=true")
	}
	if res.Err != nil {
		t.Fatalf("want nil Err for ambiguous exit 1, got %v", res.Err)
	}
	if len(res.Updates) != 0 {
		t.Fatalf("want 0 Updates for ambiguous exit 1, got %d", len(res.Updates))
	}
}

// TestNpmChecker_SourceName verifies the Source() method returns "npm".
func TestNpmChecker_SourceName(t *testing.T) {
	if got := NewNpmUpdateChecker().Source(); got != "npm" {
		t.Fatalf("want source=npm, got %q", got)
	}
}

// TestNpmChecker_ScopedPackageIncluded verifies that scoped packages
// (@angular/core) appear in updates when they have a valid upgrade.
func TestNpmChecker_ScopedPackageIncluded(t *testing.T) {
	useFixtureNpm(t)
	t.Setenv("FIXTURE_MODE", "outdated")

	res := NewNpmUpdateChecker().Check(context.Background(), nil)
	var found bool
	for _, u := range res.Updates {
		if u.Name == "@angular/core" {
			found = true
			if u.CurrentVersion != "16.0.0" {
				t.Errorf("want current=16.0.0, got %q", u.CurrentVersion)
			}
			if u.LatestVersion != "17.0.0" {
				t.Errorf("want latest=17.0.0, got %q", u.LatestVersion)
			}
		}
	}
	if !found {
		t.Error("@angular/core must be included in updates")
	}
}
