package sandbox

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// resolveHostWorkspacePath maps a container-local path to its host-side
// equivalent for Docker-out-of-Docker (DooD) sibling container mounts.
// When GoClaw runs inside a container and spawns sandbox containers, the
// workspace path (e.g., /app/workspace) only exists inside the GoClaw
// container — the sandbox needs the corresponding host path or volume name
// to mount it correctly.
//
// If not running in Docker (no /.dockerenv), returns localPath as-is.
func resolveHostWorkspacePath(ctx context.Context, localPath string) string {
	// Not in a container — local path is the host path.
	if _, err := os.Stat("/.dockerenv"); err != nil {
		return localPath
	}

	containerID := detectContainerID()
	if containerID == "" {
		slog.Error("sandbox.resolve: cannot determine container ID — DooD volume mounts will fail", "path", localPath)
		return localPath
	}

	// Short timeout to avoid blocking on a stuck Docker daemon.
	inspectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	out, err := exec.CommandContext(inspectCtx, "docker", "inspect", "--format", "{{json .Mounts}}", containerID).Output()
	if err != nil {
		slog.Warn("sandbox.resolve: docker inspect failed", "container", containerID, "error", err)
		return localPath
	}

	var mounts []struct {
		Type        string `json:"Type"`
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
		Name        string `json:"Name"`
	}
	if err := json.Unmarshal(out, &mounts); err != nil {
		slog.Warn("sandbox.resolve: failed to parse mounts", "error", err)
		return localPath
	}

	targetDir := filepath.Clean(localPath)
	var bestDest string
	var bestSource string
	var bestRel string
	var bestType string
	var bestName string

	for _, m := range mounts {
		dest := filepath.Clean(m.Destination)
		if targetDir == dest || strings.HasPrefix(targetDir, dest+string(filepath.Separator)) {
			if len(dest) > len(bestDest) {
				bestDest = dest
				bestSource = m.Source
				bestType = m.Type
				bestName = m.Name
				bestRel, _ = filepath.Rel(dest, targetDir)
			}
		}
	}

	if bestDest == "" {
		slog.Warn("sandbox.resolve: no matching mount found", "path", localPath, "container", containerID)
		return localPath
	}

	// Named volume: return volume name if it's the exact mount, otherwise
	// use the host source path (assumes local volume driver).
	if bestType == "volume" && bestName != "" {
		if bestRel == "." {
			slog.Debug("sandbox.resolve: resolved to named volume", "path", localPath, "volume", bestName)
			return bestName
		}
		if bestSource != "" {
			return filepath.Join(bestSource, bestRel)
		}
	}

	// Bind mount: join source with relative path.
	if bestSource != "" {
		resolved := filepath.Join(bestSource, bestRel)
		slog.Debug("sandbox.resolve: resolved to host path", "path", localPath, "host", resolved)
		return resolved
	}

	slog.Warn("sandbox.resolve: mount found but no source path", "path", localPath, "mount", bestDest)
	return localPath
}

// detectContainerID returns the current Docker container ID using multiple
// strategies for reliability:
//  1. /proc/self/mountinfo — parse the container ID from cgroup mount paths
//  2. HOSTNAME env var — usually the short container ID (but can be overridden)
//  3. os.Hostname() — fallback
func detectContainerID() string {
	// Strategy 1: Parse /proc/self/mountinfo for docker container ID.
	// Lines contain paths like /docker/containers/<id>/...
	if data, err := os.ReadFile("/proc/self/mountinfo"); err == nil {
		for line := range strings.SplitSeq(string(data), "\n") {
			if _, after, ok := strings.Cut(line, "/docker/containers/"); ok {
				rest := after
				if slashIdx := strings.IndexByte(rest, '/'); slashIdx > 0 {
					id := rest[:slashIdx]
					if len(id) >= 12 { // Docker IDs are 64 hex chars, short form 12
						return id
					}
				}
			}
		}
	}

	// Strategy 2: HOSTNAME env var (common Docker default).
	if h := os.Getenv("HOSTNAME"); h != "" {
		return h
	}

	// Strategy 3: os.Hostname() fallback.
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}

	return ""
}
