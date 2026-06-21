package sandbox

import "testing"

func TestDockerCacheKeyIncludesWorkspaceAndConfig(t *testing.T) {
	cfg := DefaultConfig()
	key := "agent:default:session"

	first := dockerCacheKey(key, "/srv/goclaw/workspace/tenant-a", cfg)
	second := dockerCacheKey(key, "/srv/goclaw/workspace/tenant-b", cfg)
	if first == second {
		t.Fatalf("dockerCacheKey reused key for different workspaces: %q", first)
	}

	ro := cfg
	ro.WorkspaceAccess = AccessRO
	third := dockerCacheKey(key, "/srv/goclaw/workspace/tenant-a", ro)
	if first == third {
		t.Fatalf("dockerCacheKey reused key for different workspace access: %q", first)
	}
}

func TestDockerCacheKeyPreservesEmptyWorkspaceCompatibility(t *testing.T) {
	cfg := DefaultConfig()
	key := "agent:default:session"

	if got := dockerCacheKey(key, "", cfg); got != key {
		t.Fatalf("dockerCacheKey empty workspace = %q, want original key %q", got, key)
	}
}
