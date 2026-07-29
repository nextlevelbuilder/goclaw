package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

const redactedMountRoot = "[REDACTED_MOUNT_ROOT]"

type mountPathResolver func(context.Context, string) string

func validateReadOnlyMounts(cfg Config, mounts []ReadOnlyMount) ([]ReadOnlyMount, error) {
	if len(mounts) == 0 {
		return nil, nil
	}
	if cfg.WorkspaceAccess == AccessNone {
		return nil, fmt.Errorf("read-only mounts require sandbox workspace access")
	}

	workdir := cfg.ContainerWorkdir()
	if !path.IsAbs(workdir) || path.Clean(workdir) != workdir {
		return nil, fmt.Errorf("sandbox workdir must be an absolute canonical path")
	}

	normalized := append([]ReadOnlyMount(nil), mounts...)
	names := make(map[string]struct{}, len(normalized))
	destinations := make(map[string]struct{}, len(normalized))

	for i := range normalized {
		mount := &normalized[i]
		if mount.Name == "" || strings.TrimSpace(mount.Name) != mount.Name || strings.ContainsRune(mount.Name, '\x00') {
			return nil, fmt.Errorf("read-only mount %d has an invalid name", i)
		}
		if _, exists := names[mount.Name]; exists {
			return nil, fmt.Errorf("read-only mounts must have unique names")
		}
		names[mount.Name] = struct{}{}

		if !filepath.IsAbs(mount.HostPath) || filepath.Clean(mount.HostPath) != mount.HostPath {
			return nil, fmt.Errorf("read-only mount %d host path must be absolute and canonical", i)
		}
		canonicalHost, err := filepath.EvalSymlinks(mount.HostPath)
		if err != nil {
			return nil, fmt.Errorf("read-only mount %d host path cannot be resolved", i)
		}
		if canonicalHost != mount.HostPath {
			return nil, fmt.Errorf("read-only mount %d host path must be absolute and canonical", i)
		}
		info, err := os.Stat(mount.HostPath)
		if err != nil {
			return nil, fmt.Errorf("read-only mount %d host path cannot be inspected", i)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("read-only mount %d host path must be a directory", i)
		}

		if strings.ContainsRune(mount.Destination, '\x00') ||
			!path.IsAbs(mount.Destination) ||
			path.Clean(mount.Destination) != mount.Destination {
			return nil, fmt.Errorf("read-only mount %d destination must be absolute and canonical", i)
		}
		if !containerPathStrictlyWithin(workdir, mount.Destination) {
			return nil, fmt.Errorf("read-only mount %d destination must be strictly beneath sandbox workdir", i)
		}
		if _, exists := destinations[mount.Destination]; exists {
			return nil, fmt.Errorf("read-only mounts must have unique destinations")
		}
		destinations[mount.Destination] = struct{}{}
	}

	sort.Slice(normalized, func(i, j int) bool {
		if normalized[i].Name != normalized[j].Name {
			return normalized[i].Name < normalized[j].Name
		}
		if normalized[i].Destination != normalized[j].Destination {
			return normalized[i].Destination < normalized[j].Destination
		}
		return normalized[i].HostPath < normalized[j].HostPath
	})
	return normalized, nil
}

func containerPathStrictlyWithin(root, target string) bool {
	if target == root {
		return false
	}
	return strings.HasPrefix(target, strings.TrimSuffix(root, "/")+"/")
}

func appendDockerMountArgs(
	ctx context.Context,
	args []string,
	cfg Config,
	workspace string,
	mounts []ReadOnlyMount,
	resolve mountPathResolver,
) ([]string, []string, error) {
	normalized, err := validateReadOnlyMounts(cfg, mounts)
	if err != nil {
		return nil, nil, err
	}
	if resolve == nil {
		resolve = func(_ context.Context, path string) string { return path }
	}

	sensitiveRoots := make([]string, 0, 2+len(normalized)*2)
	containerWorkdir := cfg.ContainerWorkdir()
	if workspace != "" && cfg.WorkspaceAccess != AccessNone {
		mountOpt := "rw"
		if cfg.WorkspaceAccess == AccessRO {
			mountOpt = "ro"
		}
		hostPath := resolve(ctx, workspace)
		sensitiveRoots = appendSensitiveRoot(sensitiveRoots, workspace)
		sensitiveRoots = appendSensitiveRoot(sensitiveRoots, hostPath)
		args = append(args, "-v", fmt.Sprintf("%s:%s:%s", hostPath, containerWorkdir, mountOpt))
	}

	for _, mount := range normalized {
		hostPath := resolve(ctx, mount.HostPath)
		sensitiveRoots = appendSensitiveRoot(sensitiveRoots, mount.HostPath)
		sensitiveRoots = appendSensitiveRoot(sensitiveRoots, hostPath)
		args = append(args, "-v", fmt.Sprintf("%s:%s:ro", hostPath, mount.Destination))
	}
	return args, sensitiveRoots, nil
}

func appendSensitiveRoot(roots []string, root string) []string {
	if root == "" {
		return roots
	}
	if slices.Contains(roots, root) {
		return roots
	}
	return append(roots, root)
}

func redactMountRoots(message string, roots []string) string {
	sorted := append([]string(nil), roots...)
	sort.Slice(sorted, func(i, j int) bool {
		return len(sorted[i]) > len(sorted[j])
	})
	for _, root := range sorted {
		if root != "" {
			message = strings.ReplaceAll(message, root, redactedMountRoot)
		}
	}
	return message
}

func dockerCacheKey(key, workspace string, cfg Config, mounts ...ReadOnlyMount) string {
	if workspace == "" && len(mounts) == 0 {
		return key
	}

	orderedMounts := append([]ReadOnlyMount(nil), mounts...)
	sort.Slice(orderedMounts, func(i, j int) bool {
		if orderedMounts[i].Name != orderedMounts[j].Name {
			return orderedMounts[i].Name < orderedMounts[j].Name
		}
		if orderedMounts[i].Destination != orderedMounts[j].Destination {
			return orderedMounts[i].Destination < orderedMounts[j].Destination
		}
		return orderedMounts[i].HostPath < orderedMounts[j].HostPath
	})

	parts := []string{
		workspace,
		string(cfg.WorkspaceAccess),
		cfg.ContainerWorkdir(),
		cfg.Image,
	}
	for _, mount := range orderedMounts {
		parts = append(parts, mount.Name, mount.HostPath, mount.Destination, "ro")
	}
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "w" + hex.EncodeToString(h[:])[:16] + ":" + key
}
