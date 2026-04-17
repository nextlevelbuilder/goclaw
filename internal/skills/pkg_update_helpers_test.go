package skills

import (
	"strings"
	"testing"
)

func TestIsPipPreRelease(t *testing.T) {
	cases := []struct {
		version string
		want    bool
	}{
		// Pre-release: bare identifiers (no digit) — M-1 fix
		{"1.0.0rc", true},
		{"1.0.0a", true},
		{"1.0.0b", true},
		// Pre-release: with digit
		{"1.0.0rc1", true},
		{"1.0.0a1", true},
		{"1.0.0b0", true},
		{"2.0.0.dev1", true},
		{"1.0.0.dev0", true},
		// Pre-release: .pre / .preview suffix
		{"1.0.0.pre", true},
		{"1.0.0.preview", true},
		// Stable releases
		{"1.0.0", false},
		{"2.3.4", false},
		{"1.0.0.post1", false},
		{"1.0.0.post0", false},
	}
	for _, tc := range cases {
		got := IsPipPreRelease(tc.version)
		if got != tc.want {
			t.Errorf("IsPipPreRelease(%q) = %v, want %v", tc.version, got, tc.want)
		}
	}
}

func TestIsNpmPreRelease(t *testing.T) {
	cases := []struct {
		version string
		want    bool
	}{
		// Pre-release labels
		{"5.0.0-beta.1", true},
		{"5.0.0-rc.0", true},
		{"5.0.0-alpha.1", true},
		{"5.0.0-pre", true},
		{"5.0.0-preview.2", true},
		{"5.0.0-dev", true},
		{"5.0.0-nightly", true},
		{"5.0.0-snapshot", true},
		// Stable
		{"5.0.0", false},
		{"5.0.0-foo", false},    // unknown label → not pre-release
		{"5.0.0-stable", false}, // "stable" not in list
	}
	for _, tc := range cases {
		got := IsNpmPreRelease(tc.version)
		if got != tc.want {
			t.Errorf("IsNpmPreRelease(%q) = %v, want %v", tc.version, got, tc.want)
		}
	}
}

func TestValidatePipPackageName(t *testing.T) {
	accept := []string{
		"Django",
		"my-pkg",
		"pip_tools",
		"PyJWT",
		"numpy",
		"scikit-learn",
		"A1",
	}
	for _, name := range accept {
		if err := ValidatePipPackageName(name); err != nil {
			t.Errorf("ValidatePipPackageName(%q) rejected valid name: %v", name, err)
		}
	}

	reject := []string{
		"",
		"typescript@latest", // @ suffix
		"pkg@@",             // double @
		"pkg;rm",            // shell metachar
		"pkg space",         // space
		"-pkg",              // leading hyphen
		".pkg",              // leading dot
		"pkg|other",         // pipe
		"pkg>1.0",           // gt
	}
	for _, name := range reject {
		if err := ValidatePipPackageName(name); err == nil {
			t.Errorf("ValidatePipPackageName(%q) accepted invalid name", name)
		}
	}
}

func TestValidateNpmPackageName(t *testing.T) {
	accept := []string{
		"typescript",
		"@angular/core",
		"@scope/name-2",
		"react",
		"@babel/core",
		"lodash.get",
	}
	for _, name := range accept {
		if err := ValidateNpmPackageName(name); err != nil {
			t.Errorf("ValidateNpmPackageName(%q) rejected valid name: %v", name, err)
		}
	}

	reject := []string{
		"",
		"TypeScript",          // uppercase (npm forbids)
		"typescript@latest",   // @ version suffix on bare name
		"pkg@@",               // double @
		"@scope/PKG",          // uppercase in scoped path
		"@Scope/name",         // uppercase scope
		"pkg space",           // space
		"@/name",              // empty scope
	}
	for _, name := range reject {
		if err := ValidateNpmPackageName(name); err == nil {
			t.Errorf("ValidateNpmPackageName(%q) accepted invalid name", name)
		}
	}
}

func TestClassifyPipStderr(t *testing.T) {
	cases := []struct {
		name        string
		stderr      string
		wantSentinel error
	}{
		{
			name:        "externally managed environment",
			stderr:      "error: externally-managed-environment\nsome extra text",
			wantSentinel: ErrUpdatePipExternallyManaged,
		},
		{
			name:        "EXTERNALLY-MANAGED upper",
			stderr:      "This environment is EXTERNALLY-MANAGED",
			wantSentinel: ErrUpdatePipExternallyManaged,
		},
		{
			name:        "permission denied",
			stderr:      "ERROR: Could not install packages: Permission denied",
			wantSentinel: ErrUpdatePipPermission,
		},
		{
			name:        "no matching distribution",
			stderr:      "ERROR: No matching distribution found for nonexistent-pkg==99.0",
			wantSentinel: ErrUpdatePipNotFound,
		},
		{
			name:        "could not find a version",
			stderr:      "ERROR: Could not find a version that satisfies the requirement",
			wantSentinel: ErrUpdatePipNotFound,
		},
		{
			name:        "network read timeout",
			stderr:      "Read timed out. (read timeout=15)",
			wantSentinel: ErrUpdatePipNetwork,
		},
		{
			name:        "dependency conflict",
			stderr:      "ERROR: pip's dependency resolver does not currently take into account all the packages that are installed. This behaviour is the source of the following dependency conflicts.",
			wantSentinel: ErrUpdatePipConflict,
		},
		{
			name:        "shallow backtracking",
			stderr:      "Shallow backtracking detected: could not find a matching version",
			wantSentinel: ErrUpdatePipConflict,
		},
		{
			name:        "unclassified returns nil sentinel",
			stderr:      "some random pip error output",
			wantSentinel: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sentinel, reason := ClassifyPipStderr(tc.stderr)
			if sentinel != tc.wantSentinel {
				t.Errorf("ClassifyPipStderr sentinel = %v, want %v", sentinel, tc.wantSentinel)
			}
			if reason == "" {
				t.Error("reason must not be empty")
			}
		})
	}
}

func TestClassifyNpmStderr(t *testing.T) {
	cases := []struct {
		name        string
		stderr      string
		wantSentinel error
	}{
		{
			name:        "EACCES permission",
			stderr:      "npm ERR! code EACCES\nnpm ERR! path /usr/local/lib",
			wantSentinel: ErrUpdateNpmPermission,
		},
		{
			name:        "ERESOLVE conflict",
			stderr:      "npm ERR! code ERESOLVE\nnpm ERR! ERESOLVE unable to resolve dependency tree",
			wantSentinel: ErrUpdateNpmConflict,
		},
		{
			name:        "ETIMEDOUT network",
			stderr:      "npm ERR! code ETIMEDOUT\nnpm ERR! errno ETIMEDOUT",
			wantSentinel: ErrUpdateNpmNetwork,
		},
		{
			name:        "ENOTFOUND network",
			stderr:      "npm ERR! code ENOTFOUND\nnpm ERR! errno ENOTFOUND registry.npmjs.org",
			wantSentinel: ErrUpdateNpmNetwork,
		},
		{
			name:        "ETARGET version missing",
			stderr:      "npm ERR! code ETARGET\nnpm ERR! notarget No matching version found for typescript@99.0.0",
			wantSentinel: ErrUpdateNpmTargetMissing,
		},
		{
			name:        "E404 not found",
			stderr:      "npm ERR! code E404\nnpm ERR! 404 Not Found",
			wantSentinel: ErrUpdateNpmNotFound,
		},
		{
			name:        "not in this registry",
			stderr:      "npm ERR! my-private-pkg is not in this registry",
			wantSentinel: ErrUpdateNpmNotFound,
		},
		{
			name:        "unclassified returns nil sentinel",
			stderr:      "npm ERR! some random error",
			wantSentinel: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sentinel, reason := ClassifyNpmStderr(tc.stderr)
			if sentinel != tc.wantSentinel {
				t.Errorf("ClassifyNpmStderr sentinel = %v, want %v", sentinel, tc.wantSentinel)
			}
			if reason == "" {
				t.Error("reason must not be empty")
			}
		})
	}
}

func TestTruncateStderr(t *testing.T) {
	t.Run("strips ANSI codes", func(t *testing.T) {
		in := "\x1b[31mERROR\x1b[0m: something failed"
		got := truncateStderr(in, 500)
		if strings.Contains(got, "\x1b") {
			t.Errorf("ANSI codes not stripped: %q", got)
		}
		if !strings.Contains(got, "ERROR") {
			t.Errorf("content should remain after strip: %q", got)
		}
	})

	t.Run("normalizes CRLF to space", func(t *testing.T) {
		in := "line1\r\nline2\r\nline3"
		got := truncateStderr(in, 500)
		// After normalization CRLF → LF → Fields() collapses to spaces
		if strings.Contains(got, "\r") {
			t.Errorf("CRLF not normalized: %q", got)
		}
		if !strings.Contains(got, "line1") || !strings.Contains(got, "line2") {
			t.Errorf("content lost: %q", got)
		}
	})

	t.Run("caps at n bytes with ellipsis", func(t *testing.T) {
		in := strings.Repeat("a", 600)
		got := truncateStderr(in, 500)
		if len([]rune(got)) > 502 { // 500 + len("…") rune (3 bytes but 1 rune)
			t.Errorf("not capped: len=%d", len(got))
		}
		if !strings.HasSuffix(got, "…") {
			t.Errorf("missing ellipsis: %q", got)
		}
	})

	t.Run("short string unchanged", func(t *testing.T) {
		in := "short error"
		got := truncateStderr(in, 500)
		if got != in {
			t.Errorf("short string modified: got %q, want %q", got, in)
		}
	})

	t.Run("collapses whitespace", func(t *testing.T) {
		in := "err  msg\t\twith\n\ntabs"
		got := truncateStderr(in, 500)
		if strings.Contains(got, "  ") || strings.Contains(got, "\t") || strings.Contains(got, "\n") {
			t.Errorf("whitespace not collapsed: %q", got)
		}
	})
}
