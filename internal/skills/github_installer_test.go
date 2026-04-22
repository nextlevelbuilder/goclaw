package skills

import (
	"errors"
	"strings"
	"testing"
)

func TestParseGitHubSpec(t *testing.T) {
	cases := []struct {
		in     string
		ok     bool
		owner  string
		repo   string
		tag    string
	}{
		{"github:cli/cli@v2.45.0", true, "cli", "cli", "v2.45.0"},
		{"github:cli/cli", true, "cli", "cli", ""},
		{"github:jesseduffield/lazygit@v0.42.0", true, "jesseduffield", "lazygit", "v0.42.0"},
		{"github:sharkdp/fd@v9.0.0+build.1", true, "sharkdp", "fd", "v9.0.0+build.1"},
		{"github:owner/repo.subpath@v1", true, "owner", "repo.subpath", "v1"},
		{"github:a/b", true, "a", "b", ""},
		{"github:Org-1/Repo_2@x-y.z", true, "Org-1", "Repo_2", "x-y.z"},
		{"pip:foo", false, "", "", ""},
		{"github:/repo", false, "", "", ""},
		{"github:owner/", false, "", "", ""},
		{"github:-bad/repo", false, "", "", ""},
		{"github:bad-/repo", false, "", "", ""},
		{"github:owner/repo@", false, "", "", ""},
		{"", false, "", "", ""},
	}
	for _, tc := range cases {
		got, err := ParseGitHubSpec(tc.in)
		if tc.ok {
			if err != nil {
				t.Errorf("%q: unexpected error %v", tc.in, err)
				continue
			}
			if got.Owner != tc.owner || got.Repo != tc.repo || got.Tag != tc.tag {
				t.Errorf("%q: got %+v, want {%s %s %s}", tc.in, got, tc.owner, tc.repo, tc.tag)
			}
		} else {
			if err == nil {
				t.Errorf("%q: expected error, got %+v", tc.in, got)
			}
		}
	}
}

func TestSelectAsset(t *testing.T) {
	// Realistic-ish asset lists.
	lazygit := []GitHubAsset{
		{Name: "lazygit_0.42.0_Linux_x86_64.tar.gz"},
		{Name: "lazygit_0.42.0_Linux_arm64.tar.gz"},
		{Name: "lazygit_0.42.0_Darwin_x86_64.tar.gz"},
		{Name: "lazygit_0.42.0_Windows_x86_64.zip"},
		{Name: "checksums.txt"},
	}
	starship := []GitHubAsset{
		{Name: "starship-x86_64-unknown-linux-musl.tar.gz"},
		{Name: "starship-aarch64-unknown-linux-musl.tar.gz"},
		{Name: "starship-x86_64-pc-windows-msvc.zip"},
		{Name: "starship-x86_64-unknown-linux-musl.tar.gz.sha256"},
	}
	noMatch := []GitHubAsset{
		{Name: "tool-Darwin-arm64.tar.gz"},
		{Name: "Source code (zip)"},
	}

	t.Run("lazygit amd64", func(t *testing.T) {
		a, err := SelectAsset(lazygit, "linux", "amd64")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(a.Name, "Linux_x86_64") {
			t.Errorf("got %s", a.Name)
		}
	})
	t.Run("lazygit arm64", func(t *testing.T) {
		a, err := SelectAsset(lazygit, "linux", "arm64")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(a.Name, "Linux_arm64") {
			t.Errorf("got %s", a.Name)
		}
	})
	t.Run("starship musl amd64", func(t *testing.T) {
		a, err := SelectAsset(starship, "linux", "amd64")
		if err != nil {
			t.Fatal(err)
		}
		if a.Name != "starship-x86_64-unknown-linux-musl.tar.gz" {
			t.Errorf("got %s", a.Name)
		}
	})
	t.Run("no match", func(t *testing.T) {
		_, err := SelectAsset(noMatch, "linux", "amd64")
		if !errors.Is(err, ErrNoMatchingAsset) {
			t.Errorf("want ErrNoMatchingAsset, got %v", err)
		}
		if !strings.Contains(err.Error(), "tool-Darwin-arm64") {
			t.Errorf("error should list available assets, got: %v", err)
		}
	})
}

func TestAllowedOrg(t *testing.T) {
	empty := NewGitHubInstaller(nil, &GitHubPackagesConfig{})
	if !empty.AllowedOrg("anyone") {
		t.Error("empty allowlist should permit all orgs")
	}
	locked := NewGitHubInstaller(nil, &GitHubPackagesConfig{AllowedOrgs: []string{"GoodOrg", " digitop "}})
	if !locked.AllowedOrg("goodorg") {
		t.Error("goodorg should be allowed (case-insensitive)")
	}
	if !locked.AllowedOrg("Digitop") {
		t.Error("digitop should be allowed after trim+lowercase")
	}
	if locked.AllowedOrg("evil") {
		t.Error("evil should be rejected")
	}
}

func TestConfigDefaults(t *testing.T) {
	c := &GitHubPackagesConfig{}
	c.Defaults()
	if c.BinDir == "" || c.ManifestPath == "" || c.MaxAssetSizeMB != 200 {
		t.Errorf("unexpected defaults: %+v", c)
	}
	if c.MaxAssetBytes() != 200*1024*1024 {
		t.Errorf("MaxAssetBytes wrong: %d", c.MaxAssetBytes())
	}
}

func TestCanonicalPackageName(t *testing.T) {
	spec := &GitHubSpec{Owner: "cli", Repo: "cli"}
	if canonicalPackageName(spec, []string{"gh"}) != "gh" {
		t.Error("single differing binary name should win")
	}
	if canonicalPackageName(spec, []string{"cli"}) != "cli" {
		t.Error("matching binary name should use repo")
	}
	if canonicalPackageName(spec, []string{"a", "b"}) != "cli" {
		t.Error("multi binaries should fall back to repo name")
	}
}
