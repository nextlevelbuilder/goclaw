package sandbox

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestValidateReadOnlyMountsCanonicalizesOrder(t *testing.T) {
	first := canonicalTempDir(t)
	second := canonicalTempDir(t)
	cfg := DefaultConfig()

	got, err := validateReadOnlyMounts(cfg, []ReadOnlyMount{
		{Name: "zeta", HostPath: second, Destination: "/workspace/zeta"},
		{Name: "alpha", HostPath: first, Destination: "/workspace/alpha"},
	})
	if err != nil {
		t.Fatalf("validateReadOnlyMounts() error = %v", err)
	}
	if got[0].Name != "alpha" || got[1].Name != "zeta" {
		t.Fatalf("validateReadOnlyMounts() order = %#v, want name-sorted mounts", got)
	}
}

func TestValidateReadOnlyMountsRejectsInvalidContractsWithoutLeakingRoots(t *testing.T) {
	host := canonicalTempDir(t)
	otherHost := canonicalTempDir(t)
	file := filepath.Join(host, "file.txt")
	if err := os.WriteFile(file, []byte("test"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	symlink := filepath.Join(otherHost, "linked")
	if err := os.Symlink(host, symlink); err != nil {
		t.Fatalf("create symlink fixture: %v", err)
	}

	cfg := DefaultConfig()
	tests := []struct {
		name   string
		cfg    Config
		mounts []ReadOnlyMount
	}{
		{
			name:   "empty name",
			cfg:    cfg,
			mounts: []ReadOnlyMount{{HostPath: host, Destination: "/workspace/inputs"}},
		},
		{
			name:   "duplicate name",
			cfg:    cfg,
			mounts: []ReadOnlyMount{{Name: "inputs", HostPath: host, Destination: "/workspace/a"}, {Name: "inputs", HostPath: otherHost, Destination: "/workspace/b"}},
		},
		{
			name:   "relative host",
			cfg:    cfg,
			mounts: []ReadOnlyMount{{Name: "inputs", HostPath: "relative", Destination: "/workspace/inputs"}},
		},
		{
			name:   "unclean host",
			cfg:    cfg,
			mounts: []ReadOnlyMount{{Name: "inputs", HostPath: host + string(filepath.Separator) + ".." + string(filepath.Separator) + filepath.Base(host), Destination: "/workspace/inputs"}},
		},
		{
			name:   "symlink host",
			cfg:    cfg,
			mounts: []ReadOnlyMount{{Name: "inputs", HostPath: symlink, Destination: "/workspace/inputs"}},
		},
		{
			name:   "missing host",
			cfg:    cfg,
			mounts: []ReadOnlyMount{{Name: "inputs", HostPath: filepath.Join(host, "missing"), Destination: "/workspace/inputs"}},
		},
		{
			name:   "host file",
			cfg:    cfg,
			mounts: []ReadOnlyMount{{Name: "inputs", HostPath: file, Destination: "/workspace/inputs"}},
		},
		{
			name:   "relative destination",
			cfg:    cfg,
			mounts: []ReadOnlyMount{{Name: "inputs", HostPath: host, Destination: "inputs"}},
		},
		{
			name:   "unclean destination",
			cfg:    cfg,
			mounts: []ReadOnlyMount{{Name: "inputs", HostPath: host, Destination: "/workspace/a/../inputs"}},
		},
		{
			name:   "destination equals workdir",
			cfg:    cfg,
			mounts: []ReadOnlyMount{{Name: "inputs", HostPath: host, Destination: "/workspace"}},
		},
		{
			name:   "destination sibling prefix",
			cfg:    cfg,
			mounts: []ReadOnlyMount{{Name: "inputs", HostPath: host, Destination: "/workspace-other/inputs"}},
		},
		{
			name:   "duplicate destination",
			cfg:    cfg,
			mounts: []ReadOnlyMount{{Name: "a", HostPath: host, Destination: "/workspace/inputs"}, {Name: "b", HostPath: otherHost, Destination: "/workspace/inputs"}},
		},
		{
			name: "invalid workdir",
			cfg: func() Config {
				invalid := cfg
				invalid.Workdir = "workspace"
				return invalid
			}(),
			mounts: []ReadOnlyMount{{Name: "inputs", HostPath: host, Destination: "/workspace/inputs"}},
		},
		{
			name: "workspace access none",
			cfg: func() Config {
				none := cfg
				none.WorkspaceAccess = AccessNone
				return none
			}(),
			mounts: []ReadOnlyMount{{Name: "inputs", HostPath: host, Destination: "/workspace/inputs"}},
		},
		{
			name:   "destination contains null",
			cfg:    cfg,
			mounts: []ReadOnlyMount{{Name: "inputs", HostPath: host, Destination: "/workspace/\x00inputs"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validateReadOnlyMounts(tt.cfg, tt.mounts)
			if err == nil {
				t.Fatal("validateReadOnlyMounts() error = nil, want rejection")
			}
			for _, sensitive := range []string{host, otherHost, file, symlink} {
				if strings.Contains(err.Error(), sensitive) {
					t.Fatalf("validation error leaked mount root %q: %v", sensitive, err)
				}
			}
		})
	}
}

func TestAppendDockerMountArgsAddsSortedReadOnlyMounts(t *testing.T) {
	workspace := canonicalTempDir(t)
	alpha := canonicalTempDir(t)
	zeta := canonicalTempDir(t)
	cfg := DefaultConfig()

	got, roots, err := appendDockerMountArgs(
		context.Background(),
		nil,
		cfg,
		workspace,
		[]ReadOnlyMount{
			{Name: "zeta", HostPath: zeta, Destination: "/workspace/zeta"},
			{Name: "alpha", HostPath: alpha, Destination: "/workspace/alpha"},
		},
		nil,
	)
	if err != nil {
		t.Fatalf("appendDockerMountArgs() error = %v", err)
	}

	want := []string{
		"-v", workspace + ":/workspace:rw",
		"-v", alpha + ":/workspace/alpha:ro",
		"-v", zeta + ":/workspace/zeta:ro",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("appendDockerMountArgs() = %#v, want %#v", got, want)
	}
	for _, root := range []string{workspace, alpha, zeta} {
		if !slices.Contains(roots, root) {
			t.Fatalf("sensitive roots %#v do not contain %q", roots, root)
		}
	}
}

func TestDockerCacheKeyIncludesDeterministicReadOnlyMountSet(t *testing.T) {
	cfg := DefaultConfig()
	key := "delegation:9db9"
	firstHost := canonicalTempDir(t)
	secondHost := canonicalTempDir(t)
	first := ReadOnlyMount{Name: "alpha", HostPath: firstHost, Destination: "/workspace/alpha"}
	second := ReadOnlyMount{Name: "zeta", HostPath: secondHost, Destination: "/workspace/zeta"}

	forward := dockerCacheKey(key, "/workspace/output", cfg, first, second)
	reverse := dockerCacheKey(key, "/workspace/output", cfg, second, first)
	if forward != reverse {
		t.Fatalf("mount ordering changed cache identity: %q != %q", forward, reverse)
	}

	changedDestination := first
	changedDestination.Destination = "/workspace/other"
	if got := dockerCacheKey(key, "/workspace/output", cfg, changedDestination, second); got == forward {
		t.Fatalf("destination change did not change cache identity: %q", got)
	}

	changedName := first
	changedName.Name = "other"
	if got := dockerCacheKey(key, "/workspace/output", cfg, changedName, second); got == forward {
		t.Fatalf("name change did not change cache identity: %q", got)
	}

	changedHost := first
	changedHost.HostPath = secondHost
	if got := dockerCacheKey(key, "/workspace/output", cfg, changedHost, second); got == forward {
		t.Fatalf("host change did not change cache identity: %q", got)
	}
}

func TestWorkspaceAccessOverrideIsPerAcquisition(t *testing.T) {
	for _, original := range []Access{AccessNone, AccessRO} {
		cfg := DefaultConfig()
		cfg.WorkspaceAccess = original
		opts := ApplyGetOpts([]GetOption{WithWorkspaceAccessOverride(AccessRW)})

		got := applyGetOptsToConfig(cfg, opts)
		if got.WorkspaceAccess != AccessRW {
			t.Fatalf("override from %q = %q, want rw", original, got.WorkspaceAccess)
		}
		if cfg.WorkspaceAccess != original {
			t.Fatalf("override mutated shared config: got %q, want %q", cfg.WorkspaceAccess, original)
		}
	}
}

func TestApplyGetOptsCopiesMounts(t *testing.T) {
	mounts := []ReadOnlyMount{{Name: "inputs", HostPath: "/canonical/host", Destination: "/workspace/inputs"}}
	opts := ApplyGetOpts([]GetOption{WithReadOnlyMounts(mounts...)})
	mounts[0].Name = "mutated"

	if got := opts.ReadOnlyMounts[0].Name; got != "inputs" {
		t.Fatalf("ApplyGetOpts retained caller slice: name = %q", got)
	}
}

func TestDockerManagerReleaseRetainsSandboxWhenDestroyFails(t *testing.T) {
	destroyErr := errors.New("destroy failed")
	sb := &DockerSandbox{
		containerID: "container-1",
		destroyFunc: func(context.Context) error {
			return destroyErr
		},
	}
	manager := &DockerManager{sandboxes: map[string]*DockerSandbox{"w123:delegation": sb}}

	err := manager.Release(context.Background(), "delegation")
	if !errors.Is(err, destroyErr) {
		t.Fatalf("Release() error = %v, want %v", err, destroyErr)
	}
	if got := manager.Stats()["active"]; got != 1 {
		t.Fatalf("active sandboxes after failed release = %v, want 1", got)
	}

	sb.destroyFunc = func(context.Context) error { return nil }
	if err := manager.Release(context.Background(), "delegation"); err != nil {
		t.Fatalf("Release() retry error = %v", err)
	}
	if got := manager.Stats()["active"]; got != 0 {
		t.Fatalf("active sandboxes after successful release = %v, want 0", got)
	}
}

func TestDockerManagerReleaseFailureRemainsVisibleConcurrently(t *testing.T) {
	destroyStarted := make(chan struct{})
	allowDestroy := make(chan struct{})
	destroyErr := errors.New("destroy failed")
	sb := &DockerSandbox{
		containerID: "container-1",
		destroyFunc: func(context.Context) error {
			close(destroyStarted)
			<-allowDestroy
			return destroyErr
		},
	}
	manager := &DockerManager{sandboxes: map[string]*DockerSandbox{"delegation": sb}}

	releaseDone := make(chan error, 1)
	go func() {
		releaseDone <- manager.Release(context.Background(), "delegation")
	}()
	<-destroyStarted

	statsStarted := make(chan struct{})
	statsDone := make(chan map[string]any, 1)
	go func() {
		close(statsStarted)
		statsDone <- manager.Stats()
	}()
	<-statsStarted

	select {
	case <-statsDone:
		t.Fatal("Stats returned while Release was still deciding container lifecycle")
	case <-time.After(25 * time.Millisecond):
	}

	close(allowDestroy)
	if err := <-releaseDone; !errors.Is(err, destroyErr) {
		t.Fatalf("Release() error = %v, want %v", err, destroyErr)
	}
	select {
	case stats := <-statsDone:
		if got := stats["active"]; got != 1 {
			t.Fatalf("concurrent active sandboxes = %v, want 1", got)
		}
	case <-time.After(time.Second):
		t.Fatal("Stats did not return after Release completed")
	}
}

func TestRedactMountRootsRemovesSensitivePaths(t *testing.T) {
	roots := []string{"/private/runtime/delegation", "/private/runtime/delegation/inputs"}
	message := "invalid mount /private/runtime/delegation/inputs at /private/runtime/delegation"
	got := redactMountRoots(message, roots)

	for _, root := range roots {
		if strings.Contains(got, root) {
			t.Fatalf("redactMountRoots() leaked %q in %q", root, got)
		}
	}
	if !strings.Contains(got, redactedMountRoot) {
		t.Fatalf("redactMountRoots() = %q, want redaction marker", got)
	}
}

func canonicalTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	canonical, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("canonicalize temp dir: %v", err)
	}
	return canonical
}
