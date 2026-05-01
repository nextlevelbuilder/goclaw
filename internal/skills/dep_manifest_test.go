package skills

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

func TestSplitPipSpec(t *testing.T) {
	cases := []struct {
		spec        string
		wantImport  string
		wantInstall string
	}{
		{"requests", "requests", "requests"},
		{"requests>=2.31", "requests", "requests>=2.31"},
		{"requests==2.31.0", "requests", "requests==2.31.0"},
		{"numpy<2.0", "numpy", "numpy<2.0"},
		{"pkg~=1.2", "pkg", "pkg~=1.2"},
		{"pkg!=1.0", "pkg", "pkg!=1.0"},
		{"pkg<=3", "pkg", "pkg<=3"},
		{"psycopg[binary]", "psycopg", "psycopg[binary]"},
		{"psycopg[binary]>=3.1", "psycopg", "psycopg[binary]>=3.1"},
		{"psycopg2-binary", "psycopg2-binary", "psycopg2-binary"},
	}
	for _, tc := range cases {
		t.Run(tc.spec, func(t *testing.T) {
			imp, ins := splitPipSpec(tc.spec)
			if imp != tc.wantImport || ins != tc.wantInstall {
				t.Errorf("splitPipSpec(%q) = (%q,%q), want (%q,%q)",
					tc.spec, imp, ins, tc.wantImport, tc.wantInstall)
			}
		})
	}
}

func TestCategorizeManifestDep(t *testing.T) {
	cases := []struct {
		raw            string
		wantCategory   string
		wantImportName string
		wantInstall    string
	}{
		{"pip:psycopg2-binary", "pip", "psycopg2-binary", "psycopg2-binary"},
		{"pip:requests>=2.31", "pip", "requests", "requests>=2.31"},
		{"pip:psycopg[binary]", "pip", "psycopg", "psycopg[binary]"},
		{"npm:typescript", "npm", "typescript", "typescript"},
		{"github:cli/cli@v2.40.0", "github", "", "github:cli/cli@v2.40.0"},
		{"system:ffmpeg", "system", "ffmpeg", "ffmpeg"},
		{"ffmpeg", "system", "ffmpeg", "ffmpeg"},
		{"pandoc", "system", "pandoc", "pandoc"},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			p := categorizeManifestDep(tc.raw)
			if p.Category != tc.wantCategory {
				t.Errorf("category = %q, want %q", p.Category, tc.wantCategory)
			}
			if p.ImportName != tc.wantImportName {
				t.Errorf("importName = %q, want %q", p.ImportName, tc.wantImportName)
			}
			if p.InstallSpec != tc.wantInstall {
				t.Errorf("installSpec = %q, want %q", p.InstallSpec, tc.wantInstall)
			}
			if p.Raw != tc.raw {
				t.Errorf("raw = %q, want %q", p.Raw, tc.raw)
			}
		})
	}
}

func TestParseSkillManifestFile(t *testing.T) {
	dir := t.TempDir()

	cases := []struct {
		name            string
		content         string
		wantDeps        []string
		wantExcludeDeps []string
	}{
		{
			name: "deps_only",
			content: `---
name: test
description: test skill
deps:
  - pip:psycopg2-binary
  - system:ffmpeg
---
body`,
			wantDeps:        []string{"pip:psycopg2-binary", "system:ffmpeg"},
			wantExcludeDeps: nil,
		},
		{
			name: "exclude_deps_only",
			content: `---
name: test
exclude_deps:
  - pip:my_local
---
`,
			wantDeps:        nil,
			wantExcludeDeps: []string{"pip:my_local"},
		},
		{
			name: "both",
			content: `---
name: test
deps:
  - pip:requests
exclude_deps:
  - pip:foo
---`,
			wantDeps:        []string{"pip:requests"},
			wantExcludeDeps: []string{"pip:foo"},
		},
		{
			name: "neither",
			content: `---
name: test
description: plain
---`,
			wantDeps:        nil,
			wantExcludeDeps: nil,
		},
		{
			name:            "no_frontmatter",
			content:         "plain content",
			wantDeps:        nil,
			wantExcludeDeps: nil,
		},
		{
			name: "quoted_items",
			content: `---
deps:
  - "pip:psycopg2-binary"
  - 'npm:typescript'
---`,
			wantDeps:        []string{"pip:psycopg2-binary", "npm:typescript"},
			wantExcludeDeps: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.name+"-SKILL.md")
			if err := os.WriteFile(path, []byte(tc.content), 0o644); err != nil {
				t.Fatal(err)
			}
			deps, excl := parseSkillManifestFile(path)
			if !slices.Equal(deps, tc.wantDeps) {
				t.Errorf("deps = %v, want %v", deps, tc.wantDeps)
			}
			if !slices.Equal(excl, tc.wantExcludeDeps) {
				t.Errorf("exclude_deps = %v, want %v", excl, tc.wantExcludeDeps)
			}
		})
	}

	t.Run("missing_file", func(t *testing.T) {
		deps, excl := parseSkillManifestFile(filepath.Join(dir, "nope.md"))
		if deps != nil || excl != nil {
			t.Errorf("missing file → deps=%v excl=%v, want nil nil", deps, excl)
		}
	})
}

func TestApplyManifestOverride_NoOp(t *testing.T) {
	scan := &SkillManifest{
		Requires:       []string{"ffmpeg"},
		RequiresPython: []string{"requests", "psycopg2"},
	}
	got := applyManifestOverride(scan, nil, nil)
	if got.FromManifest {
		t.Error("FromManifest should be false when no explicit deps")
	}
	if !slices.Equal(got.RequiresPython, []string{"requests", "psycopg2"}) {
		t.Errorf("RequiresPython altered: %v", got.RequiresPython)
	}
	if !slices.Equal(got.Requires, []string{"ffmpeg"}) {
		t.Errorf("Requires altered: %v", got.Requires)
	}
}

func TestApplyManifestOverride_Explicit(t *testing.T) {
	scan := &SkillManifest{
		Requires:       []string{"python3"},
		RequiresPython: []string{"auto_detected"},
		RequiresNode:   []string{"leftover"},
	}
	explicit := []string{
		"pip:psycopg2-binary",
		"pip:requests>=2.31",
		"npm:typescript",
		"system:ffmpeg",
		"github:cli/cli@v2.40.0",
	}
	got := applyManifestOverride(scan, explicit, nil)
	if !got.FromManifest {
		t.Error("FromManifest should be true")
	}
	wantPy := []string{"psycopg2-binary", "requests"}
	if !reflect.DeepEqual(got.RequiresPython, wantPy) {
		t.Errorf("RequiresPython = %v, want %v", got.RequiresPython, wantPy)
	}
	wantNode := []string{"typescript"}
	if !reflect.DeepEqual(got.RequiresNode, wantNode) {
		t.Errorf("RequiresNode = %v, want %v", got.RequiresNode, wantNode)
	}
	wantSys := []string{"ffmpeg"}
	if !reflect.DeepEqual(got.Requires, wantSys) {
		t.Errorf("Requires = %v, want %v", got.Requires, wantSys)
	}
	if !reflect.DeepEqual(got.Explicit, explicit) {
		t.Errorf("Explicit not preserved: %v", got.Explicit)
	}
}

func TestApplyManifestOverride_ExcludeOnly(t *testing.T) {
	scan := &SkillManifest{
		Requires:       []string{"ffmpeg", "pandoc"},
		RequiresPython: []string{"requests", "my_local_module", "psycopg2"},
		RequiresNode:   []string{"typescript", "my_local_js"},
	}
	exclude := []string{
		"pip:my_local_module",
		"npm:my_local_js",
		"pandoc",
	}
	got := applyManifestOverride(scan, nil, exclude)
	if got.FromManifest {
		t.Error("FromManifest should be false for exclude-only")
	}
	wantPy := []string{"requests", "psycopg2"}
	if !slices.Equal(got.RequiresPython, wantPy) {
		t.Errorf("RequiresPython = %v, want %v", got.RequiresPython, wantPy)
	}
	wantNode := []string{"typescript"}
	if !slices.Equal(got.RequiresNode, wantNode) {
		t.Errorf("RequiresNode = %v, want %v", got.RequiresNode, wantNode)
	}
	wantSys := []string{"ffmpeg"}
	if !slices.Equal(got.Requires, wantSys) {
		t.Errorf("Requires = %v, want %v", got.Requires, wantSys)
	}
}

func TestApplyManifestOverride_NilScan(t *testing.T) {
	got := applyManifestOverride(nil, []string{"pip:requests"}, nil)
	if got == nil {
		t.Fatal("nil result")
	}
	if !got.FromManifest || !slices.Equal(got.RequiresPython, []string{"requests"}) {
		t.Errorf("unexpected result: %+v", got)
	}
}

func TestIsValidDepName(t *testing.T) {
	cases := []struct {
		category, name string
		want           bool
	}{
		// pip — python import / pip package names
		{"pip", "requests", true},
		{"pip", "psycopg2-binary", true},
		{"pip", "google.cloud", true},
		{"pip", "_private", true},
		{"pip", "", false},
		{"pip", "1leading_digit", false},
		{"pip", "foo;__import__('os').system('pwn')", false}, // C1 injection
		{"pip", "foo\nbar", false},                           // newline injection
		{"pip", "foo)", false},                               // paren break-out
		{"pip", "foo'; x='", false},                          // quote break-out

		// npm
		{"npm", "typescript", true},
		{"npm", "@scope/pkg-name", true},
		{"npm", "lodash.debounce", true},
		{"npm", "", false},
		{"npm", "Upper", false}, // npm pkgs are lowercase
		{"npm", "a');require('child_process').exec('evil", false}, // C2 injection
		{"npm", "a';b('", false},

		// system
		{"system", "ffmpeg", true},
		{"system", "gcc-13", true},
		{"system", "lib_foo+bar.1", true},
		{"system", "", false},
		{"system", "rm -rf /", false},   // space
		{"system", "foo;bar", false},    // semicolon
		{"system", "$(evil)", false},    // command substitution
		{"system", "`bad`", false},      // backtick
		{"system", "a|b", false},        // pipe

		// github — opaque spec, validated downstream
		{"github", "anything/goes@v1", true},

		// unknown category
		{"bogus", "foo", false},
	}
	for _, tc := range cases {
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			got := isValidDepName(tc.category, tc.name)
			if got != tc.want {
				t.Errorf("isValidDepName(%q, %q) = %v, want %v", tc.category, tc.name, got, tc.want)
			}
		})
	}
}

func TestApplyManifestOverride_DropsInjection(t *testing.T) {
	scan := &SkillManifest{}
	malicious := []string{
		"pip:foo;__import__('os').system('pwn')",
		"pip:good_one",
		"npm:a');require('child_process').exec('evil",
		"npm:typescript",
		"system:rm -rf /",
		"system:ffmpeg",
		"pip:",             // empty spec
		"pip:>=1.0",        // version only, no name
		"pip:[binary]",     // extras only, no name
	}
	got := applyManifestOverride(scan, malicious, nil)
	if !got.FromManifest {
		t.Fatal("FromManifest should be true")
	}
	wantPy := []string{"good_one"}
	if !slices.Equal(got.RequiresPython, wantPy) {
		t.Errorf("RequiresPython = %v, want %v (injection should be dropped)", got.RequiresPython, wantPy)
	}
	wantNode := []string{"typescript"}
	if !slices.Equal(got.RequiresNode, wantNode) {
		t.Errorf("RequiresNode = %v, want %v", got.RequiresNode, wantNode)
	}
	wantSys := []string{"ffmpeg"}
	if !slices.Equal(got.Requires, wantSys) {
		t.Errorf("Requires = %v, want %v", got.Requires, wantSys)
	}
}

func TestSplitPipSpec_MalformedReturnsEmpty(t *testing.T) {
	cases := []string{">=1.0", "<=2", "==3", "!=4", "~=1", "<5", ">6", "[binary]"}
	for _, spec := range cases {
		t.Run(spec, func(t *testing.T) {
			imp, ins := splitPipSpec(spec)
			if imp != "" {
				t.Errorf("splitPipSpec(%q) importName = %q, want empty", spec, imp)
			}
			if ins != spec {
				t.Errorf("installSpec = %q, want %q", ins, spec)
			}
		})
	}
}

func TestFilterOutByImportName_DoesNotMutateInput(t *testing.T) {
	original := []string{"a", "b", "c"}
	snapshot := append([]string(nil), original...)
	_ = filterOutByImportName(original, []string{"pip:b"}, "pip")
	if !slices.Equal(original, snapshot) {
		t.Errorf("input mutated: got %v, want %v", original, snapshot)
	}
}

func TestFilterOutByImportName(t *testing.T) {
	names := []string{"requests", "psycopg2", "bad"}
	excl := []string{"pip:bad", "pip:unused_other"}
	got := filterOutByImportName(names, excl, "pip")
	want := []string{"requests", "psycopg2"}
	if !slices.Equal(got, want) {
		t.Errorf("filter = %v, want %v", got, want)
	}
}
