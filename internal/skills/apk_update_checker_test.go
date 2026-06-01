package skills

// apk_update_checker_test.go — unit tests for ApkUpdateChecker and
// parseApkOutdated. Tests inject fake responses via apkHelperCallFunc and
// control Alpine detection via overrideAlpineRuntime (Phase 1 hook).

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// fakeApkHelper returns a apkHelperCallFunc implementation that returns canned
// values for specific action calls. Unrecognised actions return helper_error.
func fakeApkHelper(responses map[string]struct {
	ok     bool
	code   string
	data   string
	errMsg string
}) func(ctx context.Context, action, pkg string) (bool, string, string, string) {
	return func(ctx context.Context, action, pkg string) (bool, string, string, string) {
		if r, ok := responses[action]; ok {
			return r.ok, r.code, r.data, r.errMsg
		}
		return false, "helper_error", "", fmt.Sprintf("unexpected action: %s", action)
	}
}

// setupApkHelper overrides apkHelperCallFunc for the duration of the test and
// restores it via t.Cleanup. Also forces Alpine runtime = true unless the test
// needs to test the non-Alpine path.
func setupApkHelper(t *testing.T, fn func(ctx context.Context, action, pkg string) (bool, string, string, string)) {
	t.Helper()
	orig := apkHelperCallFunc
	apkHelperCallFunc = fn
	t.Cleanup(func() { apkHelperCallFunc = orig })
}

// ── TestApkChecker_Source ─────────────────────────────────────────────────────

func TestApkChecker_Source(t *testing.T) {
	c := NewApkUpdateChecker()
	if got := c.Source(); got != "apk" {
		t.Fatalf("Source() = %q, want %q", got, "apk")
	}
}

// ── TestApkChecker_NotAlpine ──────────────────────────────────────────────────

// TestApkChecker_NotAlpine verifies that Check returns Available:false when
// IsAlpineRuntime() reports false (e.g. macOS CI, Ubuntu, etc.).
func TestApkChecker_NotAlpine(t *testing.T) {
	overrideAlpineRuntime(false)
	t.Cleanup(func() { overrideAlpineRuntime(false) }) // leave false for safety

	c := NewApkUpdateChecker()
	res := c.Check(context.Background(), nil)

	if res.Source != "apk" {
		t.Fatalf("Source = %q, want %q", res.Source, "apk")
	}
	if res.Available {
		t.Fatal("Available = true, want false on non-Alpine runtime")
	}
	if res.Err != nil {
		t.Fatalf("Err = %v, want nil", res.Err)
	}
	if len(res.Updates) != 0 {
		t.Fatalf("Updates len = %d, want 0", len(res.Updates))
	}
}

// ── TestApkChecker_HelperUnavailable ─────────────────────────────────────────

// TestApkChecker_HelperUnavailable verifies that a dial failure on update-index
// returns Available:false with nil Err — treats the helper as absent, not broken.
func TestApkChecker_HelperUnavailable(t *testing.T) {
	overrideAlpineRuntime(true)
	t.Cleanup(func() { overrideAlpineRuntime(false) })

	dialErr := errors.New("connect unix /tmp/pkg.sock: no such file or directory")
	setupApkHelper(t, func(_ context.Context, action, _ string) (bool, string, string, string) {
		// Simulate socket dial failure for any action.
		_ = action
		return false, "helper_unavailable", "", fmt.Sprintf("pkg-helper unavailable: %v", dialErr)
	})

	c := NewApkUpdateChecker()
	res := c.Check(context.Background(), nil)

	if res.Available {
		t.Fatal("Available = true, want false when helper is unreachable")
	}
	if res.Err != nil {
		t.Fatalf("Err = %v, want nil (dial fail is not an error, just absent)", res.Err)
	}
}

// ── TestApkChecker_UpdateIndexFails_Network ───────────────────────────────────

// TestApkChecker_UpdateIndexFails_Network verifies that when update-index
// returns ok=false with code="network", Check returns Available:true with Err set.
// This distinguishes "network error" (source reachable, action failed) from
// "helper absent" (socket not connected).
func TestApkChecker_UpdateIndexFails_Network(t *testing.T) {
	overrideAlpineRuntime(true)
	t.Cleanup(func() { overrideAlpineRuntime(false) })

	setupApkHelper(t, fakeApkHelper(map[string]struct {
		ok     bool
		code   string
		data   string
		errMsg string
	}{
		"update-index": {ok: false, code: "network", errMsg: "unable to fetch index from mirror"},
	}))

	c := NewApkUpdateChecker()
	res := c.Check(context.Background(), nil)

	if !res.Available {
		t.Fatal("Available = false, want true (helper reached, index refresh failed)")
	}
	if res.Err == nil {
		t.Fatal("Err = nil, want non-nil on network index failure")
	}
}

// ── TestApkChecker_ListOutdated_ParsesCorrectly ───────────────────────────────

// TestApkChecker_ListOutdated_ParsesCorrectly verifies that a three-line
// list-outdated response produces three correctly parsed UpdateInfo entries.
func TestApkChecker_ListOutdated_ParsesCorrectly(t *testing.T) {
	overrideAlpineRuntime(true)
	t.Cleanup(func() { overrideAlpineRuntime(false) })

	listData := "curl-8.5.0-r0 < 8.6.0-r1\npy3-pip-22.0.4-r0 < 22.3-r0\nbash-5.2.21-r6 < 5.2.26-r0\n"

	setupApkHelper(t, fakeApkHelper(map[string]struct {
		ok     bool
		code   string
		data   string
		errMsg string
	}{
		"update-index":  {ok: true},
		"list-outdated": {ok: true, data: listData},
	}))

	c := NewApkUpdateChecker()
	res := c.Check(context.Background(), nil)

	if !res.Available {
		t.Fatal("Available = false, want true")
	}
	if res.Err != nil {
		t.Fatalf("Err = %v, want nil", res.Err)
	}
	if len(res.Updates) != 3 {
		t.Fatalf("Updates len = %d, want 3", len(res.Updates))
	}

	byName := make(map[string]UpdateInfo, len(res.Updates))
	for _, u := range res.Updates {
		byName[u.Name] = u
	}

	tests := []struct {
		name    string
		current string
		latest  string
	}{
		{"curl", "8.5.0-r0", "8.6.0-r1"},
		{"py3-pip", "22.0.4-r0", "22.3-r0"},
		{"bash", "5.2.21-r6", "5.2.26-r0"},
	}
	for _, tc := range tests {
		u, ok := byName[tc.name]
		if !ok {
			t.Errorf("missing package %q in Updates", tc.name)
			continue
		}
		if u.Source != "apk" {
			t.Errorf("%s Source = %q, want %q", tc.name, u.Source, "apk")
		}
		if u.CurrentVersion != tc.current {
			t.Errorf("%s CurrentVersion = %q, want %q", tc.name, u.CurrentVersion, tc.current)
		}
		if u.LatestVersion != tc.latest {
			t.Errorf("%s LatestVersion = %q, want %q", tc.name, u.LatestVersion, tc.latest)
		}
		if src, _ := u.Meta["source"].(string); src != "apk" {
			t.Errorf("%s Meta[source] = %q, want %q", tc.name, src, "apk")
		}
		if u.CheckedAt.IsZero() {
			t.Errorf("%s CheckedAt is zero", tc.name)
		}
	}
}

// ── TestApkChecker_ListOutdated_SkipsMalformed ────────────────────────────────

// TestApkChecker_ListOutdated_SkipsMalformed verifies that malformed lines are
// silently skipped and valid lines still produce UpdateInfo entries.
func TestApkChecker_ListOutdated_SkipsMalformed(t *testing.T) {
	overrideAlpineRuntime(true)
	t.Cleanup(func() { overrideAlpineRuntime(false) })

	// One malformed line (no " < " separator) + one valid line.
	listData := "invalid no-separator-here\ncurl-8.5.0-r0 < 8.6.0-r1\n"

	setupApkHelper(t, fakeApkHelper(map[string]struct {
		ok     bool
		code   string
		data   string
		errMsg string
	}{
		"update-index":  {ok: true},
		"list-outdated": {ok: true, data: listData},
	}))

	c := NewApkUpdateChecker()
	res := c.Check(context.Background(), nil)

	if !res.Available {
		t.Fatal("Available = false, want true")
	}
	if res.Err != nil {
		t.Fatalf("Err = %v, want nil", res.Err)
	}
	if len(res.Updates) != 1 {
		t.Fatalf("Updates len = %d, want 1 (malformed line skipped)", len(res.Updates))
	}
	if res.Updates[0].Name != "curl" {
		t.Errorf("Updates[0].Name = %q, want %q", res.Updates[0].Name, "curl")
	}
}

// ── TestApkChecker_ListOutdated_Empty ────────────────────────────────────────

// TestApkChecker_ListOutdated_Empty verifies that an empty data payload
// produces Available:true with zero Updates and nil Err.
func TestApkChecker_ListOutdated_Empty(t *testing.T) {
	overrideAlpineRuntime(true)
	t.Cleanup(func() { overrideAlpineRuntime(false) })

	setupApkHelper(t, fakeApkHelper(map[string]struct {
		ok     bool
		code   string
		data   string
		errMsg string
	}{
		"update-index":  {ok: true},
		"list-outdated": {ok: true, data: ""},
	}))

	c := NewApkUpdateChecker()
	res := c.Check(context.Background(), nil)

	if !res.Available {
		t.Fatal("Available = false, want true")
	}
	if res.Err != nil {
		t.Fatalf("Err = %v, want nil", res.Err)
	}
	if len(res.Updates) != 0 {
		t.Fatalf("Updates len = %d, want 0 for empty data", len(res.Updates))
	}
}

// ── TestParseApkOutdated_HandlesSuffixes ─────────────────────────────────────

// TestParseApkOutdated_HandlesSuffixes validates the table of fixtures from the
// research report (researcher-260417-1500-apk-cli-behavior.md §12), covering
// dash-in-name, + in name, _git suffix, and standard packages.
func TestParseApkOutdated_HandlesSuffixes(t *testing.T) {
	tests := []struct {
		line    string
		name    string
		version string
		latest  string
		skip    bool // true = expect the line to be skipped (malformed)
	}{
		// Standard package.
		{line: "curl-8.5.0-r0 < 8.6.0-r1", name: "curl", version: "8.5.0-r0", latest: "8.6.0-r1"},
		// Dash in package name.
		{line: "py3-pip-22.0.4-r0 < 22.3-r0", name: "py3-pip", version: "22.0.4-r0", latest: "22.3-r0"},
		// _git suffix in version.
		{line: "libstdc++-12.2.1_git20220924-r4 < 13.0.0-r0", name: "libstdc++", version: "12.2.1_git20220924-r4", latest: "13.0.0-r0"},
		// + in package name.
		{line: "gtk+3.0-3.24.35-r0 < 3.24.37-r0", name: "gtk+3.0", version: "3.24.35-r0", latest: "3.24.37-r0"},
		// bash (Phase task example).
		{line: "bash-5.2.21-r6 < 5.2.26-r0", name: "bash", version: "5.2.21-r6", latest: "5.2.26-r0"},
		// musl with _git in name-portion (unusual but valid Alpine pkg naming).
		{line: "musl-1.2.4_git20240312-r0 < 1.2.5-r0", name: "musl", version: "1.2.4_git20240312-r0", latest: "1.2.5-r0"},
		// ca-certificates: hyphen in name, release suffix in version.
		{line: "ca-certificates-20230506-r0 < 20240226-r0", name: "ca-certificates", version: "20230506-r0", latest: "20240226-r0"},

		// Malformed: wrong direction operator (skip).
		{line: "musl-1.2.4_git > 1.2.3", skip: true},
		// Malformed: no separator (skip).
		{line: "invalid no-separator-here", skip: true},
		// Empty line (skip, no error).
		{line: "", skip: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.line, func(t *testing.T) {
			raw := tc.line
			if raw != "" {
				raw += "\n" // simulate newline-terminated output
			}
			entries := parseApkOutdated(raw)

			if tc.skip {
				if len(entries) != 0 {
					t.Errorf("expected 0 entries for malformed/empty line, got %d: %+v",
						len(entries), entries)
				}
				return
			}

			if len(entries) != 1 {
				t.Fatalf("expected 1 entry, got %d", len(entries))
			}
			e := entries[0]
			if e.Name != tc.name {
				t.Errorf("Name = %q, want %q", e.Name, tc.name)
			}
			if e.Version != tc.version {
				t.Errorf("Version = %q, want %q", e.Version, tc.version)
			}
			if e.Latest != tc.latest {
				t.Errorf("Latest = %q, want %q", e.Latest, tc.latest)
			}
		})
	}
}
