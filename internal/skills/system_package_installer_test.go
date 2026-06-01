package skills

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func withSystemPackageTestHooks(t *testing.T, alpine bool, lookPathErr error, run func(context.Context, string, ...string) ([]byte, error)) {
	t.Helper()
	t.Setenv("RUNTIME_DIR", t.TempDir())
	overrideAlpineRuntime(alpine)
	origLookPath := systemLookPath
	origRun := systemCommandCombinedOutput
	systemLookPath = func(file string) (string, error) {
		if lookPathErr != nil {
			return "", lookPathErr
		}
		return "/usr/bin/" + file, nil
	}
	systemCommandCombinedOutput = run
	t.Cleanup(func() {
		systemLookPath = origLookPath
		systemCommandCombinedOutput = origRun
		overrideAlpineRuntime(false)
	})
}

func TestResolveDebianPackageNameAliases(t *testing.T) {
	tests := map[string]string{
		"pip3":       "python3-pip",
		"github-cli": "gh",
		"go":         "golang-go",
		"golang":     "golang-go",
		"ripgrep":    "ripgrep",
		"libstdc++":  "libstdc++",
	}
	for input, want := range tests {
		got, err := resolveDebianPackageName(input)
		if err != nil {
			t.Fatalf("resolveDebianPackageName(%q): %v", input, err)
		}
		if got != want {
			t.Fatalf("resolveDebianPackageName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestResolveDebianPackageNameRejectsUnsafeNames(t *testing.T) {
	for _, input := range []string{"", "../curl", "pkg/name", "@scope/pkg", "curl;reboot", "-flag"} {
		if got, err := resolveDebianPackageName(input); err == nil {
			t.Fatalf("resolveDebianPackageName(%q) = %q, want error", input, got)
		}
	}
}

func TestInstallSystemPackageUsesAptOnNonAlpine(t *testing.T) {
	var gotName string
	var gotArgs []string
	withSystemPackageTestHooks(t, false, nil, func(_ context.Context, name string, args ...string) ([]byte, error) {
		gotName = name
		gotArgs = append([]string(nil), args...)
		return nil, nil
	})

	ok, msg := installSystemPackage(context.Background(), "pip3")

	if !ok || msg != "" {
		t.Fatalf("installSystemPackage failed: ok=%v msg=%q", ok, msg)
	}
	if gotName != "sudo" {
		t.Fatalf("command = %q, want sudo", gotName)
	}
	wantArgs := []string{"-n", "env", "DEBIAN_FRONTEND=noninteractive", "apt-get", "install", "-y", "--no-install-recommends", "python3-pip"}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("args = %#v, want %#v", gotArgs, wantArgs)
	}
}

func TestUninstallSystemPackageUsesAptRemoveOnNonAlpine(t *testing.T) {
	var gotArgs []string
	withSystemPackageTestHooks(t, false, nil, func(_ context.Context, _ string, args ...string) ([]byte, error) {
		gotArgs = append([]string(nil), args...)
		return nil, nil
	})

	ok, msg := uninstallSystemPackage(context.Background(), "github-cli")

	if !ok || msg != "" {
		t.Fatalf("uninstallSystemPackage failed: ok=%v msg=%q", ok, msg)
	}
	wantArgs := []string{"-n", "env", "DEBIAN_FRONTEND=noninteractive", "apt-get", "remove", "-y", "gh"}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("args = %#v, want %#v", gotArgs, wantArgs)
	}
}

func TestInstallSystemPackageReportsMissingApt(t *testing.T) {
	withSystemPackageTestHooks(t, false, errors.New("missing"), func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("command should not run")
		return nil, nil
	})

	ok, msg := installSystemPackage(context.Background(), "ripgrep")

	if ok {
		t.Fatal("installSystemPackage succeeded, want failure")
	}
	if !strings.Contains(msg, errSystemPackageMgrUnavailable) {
		t.Fatalf("msg = %q, want unavailable", msg)
	}
}
