package sandbox

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// resolveHostWorkspacePath attempts to find the true host path or volume name
// corresponding to a path inside the current container. This is necessary for
// Docker-out-of-Docker (DooD) setups where sibling containers must mount the
// same host directory/volume.
func resolveHostWorkspacePath(ctx context.Context, localPath string) string {
	// If not running in a container, the local path is the host path.
	if _, err := os.Stat("/.dockerenv"); err != nil {
		return localPath
	}

	// In a container, determine the container ID (often the hostname)
	containerID := os.Getenv("HOSTNAME")
	if containerID == "" {
		hostname, err := os.Hostname()
		if err == nil {
			containerID = hostname
		}
	}
	if containerID == "" {
		slog.Warn("docker resolving: could not determine container ID, using local path", "localPath", localPath)
		return localPath
	}

	// Inspect the container's mounts
	out, err := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{json .Mounts}}", containerID).Output()
	if err != nil {
		slog.Warn("docker resolving: inspect failed", "containerID", containerID, "error", err)
		return localPath
	}

	var mounts []struct {
		Type        string `json:"Type"`
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
		Name        string `json:"Name"`
	}
	if err := json.Unmarshal(out, &mounts); err != nil {
		slog.Warn("docker resolving: failed to parse inspect output", "error", err)
		return localPath
	}

	targetDir := filepath.Clean(localPath)
	var bestMatch *struct {
		Type        string `json:"Type"`
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
		Name        string `json:"Name"`
	}
	var bestRel string

	for i := range mounts {
		m := &mounts[i]
		dest := filepath.Clean(m.Destination)
		if targetDir == dest || strings.HasPrefix(targetDir, dest+string(filepath.Separator)) {
			// Find the most specific mount (longest destination path)
			if bestMatch == nil || len(dest) > len(filepath.Clean(bestMatch.Destination)) {
				bestMatch = m
				bestRel, _ = filepath.Rel(dest, targetDir)
			}
		}
	}

	if bestMatch != nil {
		if bestMatch.Type == "volume" && bestMatch.Name != "" {
			if bestRel == "." {
				slog.Debug("docker resolving: resolved to named volume", "localPath", localPath, "volume", bestMatch.Name)
				return bestMatch.Name
			}
			slog.Warn("docker resolving: localPath is a subfolder of a named volume. Returning host source path, which assumes host Docker uses local volumes.", "localPath", localPath, "volume", bestMatch.Name, "subPath", bestRel)
			if bestMatch.Source != "" {
				return filepath.Join(bestMatch.Source, bestRel)
			}
		}
		if bestMatch.Source != "" {
			slog.Debug("docker resolving: resolved to host path", "localPath", localPath, "hostPath", filepath.Join(bestMatch.Source, bestRel))
			return filepath.Join(bestMatch.Source, bestRel)
		}
	}

	slog.Warn("docker resolving: no matching mount found", "localPath", localPath, "containerID", containerID)
	return localPath
}
