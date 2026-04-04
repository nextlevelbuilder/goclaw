// Package sandbox — fsbridge.go provides sandboxed file operations via Docker exec.
// Matching TS src/agents/sandbox/fs-bridge.ts.
//
// When sandbox is enabled, file tools (read_file, write_file, list_files)
// route through FsBridge instead of direct host filesystem access.
// All operations execute inside the Docker container via "docker exec".
package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// FsBridge provides sandboxed file operations via Docker exec.
// Matching TS SandboxFsBridge in fs-bridge.ts.
type FsBridge struct {
	containerID string
	workdir     string // container-side working directory (e.g. "/workspace")
}

// NewFsBridge creates a bridge to a running sandbox container.
func NewFsBridge(containerID, workdir string) *FsBridge {
	if workdir == "" {
		workdir = "/workspace"
	}
	return &FsBridge{
		containerID: containerID,
		workdir:     workdir,
	}
}

// ReadFile reads file contents from inside the container.
// Matching TS FsBridge.readFile().
func (b *FsBridge) ReadFile(ctx context.Context, path string) (string, error) {
	resolved := b.resolvePath(path)

	stdout, stderr, exitCode, err := b.dockerExec(ctx, nil, "cat", "--", resolved)
	if err != nil {
		return "", fmt.Errorf("fsbridge read: %w", err)
	}
	if exitCode != 0 {
		return "", fmt.Errorf("read failed: %s", strings.TrimSpace(stderr))
	}

	return stdout, nil
}

// WriteFile writes content to a file inside the container, creating directories as needed.
// When append is true, content is appended (shell >>); otherwise the file is overwritten (shell >).
// Matching TS FsBridge.writeFile().
func (b *FsBridge) WriteFile(ctx context.Context, path, content string, appendMode bool) error {
	resolved := b.resolvePath(path)

	// Create parent directory
	dir := resolved[:strings.LastIndex(resolved, "/")]
	if dir != "" && dir != "/" {
		_, _, _, _ = b.dockerExec(ctx, nil, "mkdir", "-p", dir)
	}

	redir := ">"
	if appendMode {
		redir = ">>"
	}
	// Write content via stdin pipe
	_, stderr, exitCode, err := b.dockerExec(ctx, []byte(content), "sh", "-c", fmt.Sprintf("cat %s %q", redir, resolved))
	if err != nil {
		return fmt.Errorf("fsbridge write: %w", err)
	}
	if exitCode != 0 {
		return fmt.Errorf("write failed: %s", strings.TrimSpace(stderr))
	}

	return nil
}

// ListDir lists files and directories inside the container.
// Matching TS FsBridge.readdir().
func (b *FsBridge) ListDir(ctx context.Context, path string) (string, error) {
	resolved := b.resolvePath(path)

	// Use ls -la for detailed listing
	stdout, stderr, exitCode, err := b.dockerExec(ctx, nil, "ls", "-la", "--", resolved)
	if err != nil {
		return "", fmt.Errorf("fsbridge list: %w", err)
	}
	if exitCode != 0 {
		return "", fmt.Errorf("list failed: %s", strings.TrimSpace(stderr))
	}

	return stdout, nil
}

// Stat checks if a path exists and returns basic info.
func (b *FsBridge) Stat(ctx context.Context, path string) (string, error) {
	resolved := b.resolvePath(path)

	stdout, stderr, exitCode, err := b.dockerExec(ctx, nil, "stat", "--", resolved)
	if err != nil {
		return "", fmt.Errorf("fsbridge stat: %w", err)
	}
	if exitCode != 0 {
		return "", fmt.Errorf("stat failed: %s", strings.TrimSpace(stderr))
	}

	return stdout, nil
}

// resolvePath resolves a path relative to the container workdir.
// Validates that absolute paths stay within the workdir (defense in depth).
func (b *FsBridge) resolvePath(path string) string {
	if path == "" || path == "." {
		return b.workdir
	}
	if strings.HasPrefix(path, "/") {
		// Validate absolute paths stay within workdir (defense in depth,
		// container is already sandboxed with read-only FS + cap-drop ALL).
		cleaned := filepath.Clean(path)
		if cleaned == b.workdir || strings.HasPrefix(cleaned, b.workdir+"/") {
			return cleaned
		}
		return b.workdir // fallback to workdir for escapes
	}
	// Relative paths: use filepath.Join for proper normalization
	return filepath.Clean(filepath.Join(b.workdir, path))
}

// dockerExec runs a command inside the container and returns stdout, stderr, exit code.
func (b *FsBridge) dockerExec(ctx context.Context, stdin []byte, args ...string) (string, string, int, error) {
	dockerArgs := []string{"exec"}
	if stdin != nil {
		dockerArgs = append(dockerArgs, "-i")
	}
	dockerArgs = append(dockerArgs, b.containerID)
	dockerArgs = append(dockerArgs, args...)

	cmd := exec.CommandContext(ctx, "docker", dockerArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
			err = nil // non-zero exit is not an execution error
		} else {
			return "", "", -1, err
		}
	}

	return stdout.String(), stderr.String(), exitCode, nil
}
