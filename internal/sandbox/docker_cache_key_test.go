package sandbox

import (
	"strings"
	"testing"
)

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

func TestDockerCacheKeyDoesNotExposeMountRoots(t *testing.T) {
	cfg := DefaultConfig()
	hostRoot := "/sensitive/runtime/delegation/inputs"
	got := dockerCacheKey(
		"delegation:9db9",
		"/sensitive/runtime/delegation/outputs",
		cfg,
		ReadOnlyMount{
			Name:        "inputs",
			HostPath:    hostRoot,
			Destination: "/workspace/inputs",
		},
	)

	if strings.Contains(got, "/sensitive/") || strings.Contains(got, hostRoot) {
		t.Fatalf("dockerCacheKey exposed mount root: %q", got)
	}
}

func TestDockerCacheKeyIncludesDelegationIdentity(t *testing.T) {
	cfg := DefaultConfig()
	mount := ReadOnlyMount{
		Name:        "inputs",
		HostPath:    "/runtime/inputs",
		Destination: "/workspace/inputs",
	}

	first := dockerCacheKey("delegation:first", "/runtime/outputs", cfg, mount)
	second := dockerCacheKey("delegation:second", "/runtime/outputs", cfg, mount)
	if first == second {
		t.Fatalf("dockerCacheKey reused identity across delegation keys: %q", first)
	}
}
