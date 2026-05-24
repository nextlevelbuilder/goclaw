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
	realPath, err := b.resolveExistingPath(ctx, resolved)
	if err != nil {
		return "", err
	}

	stdout, stderr, exitCode, err := b.dockerExec(ctx, nil, "cat", "--", realPath)
	if err != nil {
		return "", fmt.Errorf("fsbridge read: %w", err)
	}
	if exitCode != 0 {
		return "", fmt.Errorf("read failed: %s", strings.TrimSpace(stderr))
	}

	return stdout, nil
}

// WriteFile writes content to a file inside the container, creating directories as needed.
// When append is true, content is appended; otherwise the file is overwritten.
// Matching TS FsBridge.writeFile().
func (b *FsBridge) WriteFile(ctx context.Context, path, content string, appendMode bool) error {
	resolved := b.resolvePath(path)

	if err := b.validateExistingTargetIfPresent(ctx, resolved); err != nil {
		return err
	}

	dir := resolved[:strings.LastIndex(resolved, "/")]
	if dir != "" && dir != "/" {
		if err := b.validateParentBeforeCreate(ctx, dir); err != nil {
			return err
		}
		_, stderr, exitCode, err := b.dockerExec(ctx, nil, "mkdir", "-p", "--", dir)
		if err != nil {
			return fmt.Errorf("fsbridge mkdir: %w", err)
		}
		if exitCode != 0 {
			return fmt.Errorf("mkdir failed: %s", strings.TrimSpace(stderr))
		}
		if err := b.validateParentBeforeCreate(ctx, dir); err != nil {
			return err
		}
	}

	ddArgs := fsBridgeWriteDDArgs(resolved, appendMode)
	_, stderr, exitCode, err := b.dockerExec(ctx, []byte(content), ddArgs...)
	if err != nil {
		return fmt.Errorf("fsbridge write: %w", err)
	}
	if exitCode != 0 {
		return fmt.Errorf("write failed: %s", strings.TrimSpace(stderr))
	}

	return nil
}

func fsBridgeWriteDDArgs(resolved string, appendMode bool) []string {
	args := []string{"dd", "bs=1048576", "status=none", "of=" + resolved}
	if appendMode {
		args = append(args, "conv=notrunc", "oflag=append")
	}
	return args
}

// ListDir lists files and directories inside the container.
// Matching TS FsBridge.readdir().
func (b *FsBridge) ListDir(ctx context.Context, path string) (string, error) {
	resolved := b.resolvePath(path)
	realPath, err := b.resolveExistingPath(ctx, resolved)
	if err != nil {
		return "", err
	}

	// Use ls -la for detailed listing
	stdout, stderr, exitCode, err := b.dockerExec(ctx, nil, "ls", "-la", "--", realPath)
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
	realPath, err := b.resolveExistingPath(ctx, resolved)
	if err != nil {
		return "", err
	}

	stdout, stderr, exitCode, err := b.dockerExec(ctx, nil, "stat", "--", realPath)
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
	workdir := filepath.Clean(b.workdir)
	if path == "" || path == "." {
		return workdir
	}
	var cleaned string
	if strings.HasPrefix(path, "/") {
		cleaned = filepath.Clean(path)
	} else {
		cleaned = filepath.Clean(filepath.Join(workdir, path))
	}
	if cleaned == workdir || strings.HasPrefix(cleaned, workdir+"/") {
		return cleaned
	}
	return workdir
}

func fsBridgePathWithin(root, target string) bool {
	root = filepath.Clean(root)
	target = filepath.Clean(target)
	if target == root {
		return true
	}
	return strings.HasPrefix(target, root+"/")
}

func (b *FsBridge) containerRealPath(ctx context.Context, path string) (string, error) {
	stdout, stderr, exitCode, err := b.dockerExec(ctx, nil, "realpath", "-e", "--", path)
	if err != nil {
		return "", fmt.Errorf("fsbridge realpath: %w", err)
	}
	if exitCode != 0 {
		return "", fmt.Errorf("realpath failed: %s", strings.TrimSpace(stderr))
	}
	return strings.TrimSpace(stdout), nil
}

func (b *FsBridge) containerRealWorkdir(ctx context.Context) (string, error) {
	return b.containerRealPath(ctx, filepath.Clean(b.workdir))
}

func (b *FsBridge) resolveExistingPath(ctx context.Context, resolved string) (string, error) {
	realWorkdir, err := b.containerRealWorkdir(ctx)
	if err != nil {
		return "", err
	}
	realPath, err := b.containerRealPath(ctx, resolved)
	if err != nil {
		return "", err
	}
	if !fsBridgePathWithin(realWorkdir, realPath) {
		return "", fmt.Errorf("path escapes sandbox workdir")
	}
	return realPath, nil
}

func (b *FsBridge) validateExistingTargetIfPresent(ctx context.Context, resolved string) error {
	realWorkdir, err := b.containerRealWorkdir(ctx)
	if err != nil {
		return err
	}
	realPath, err := b.containerRealPath(ctx, resolved)
	if err != nil {
		return nil
	}
	if !fsBridgePathWithin(realWorkdir, realPath) {
		return fmt.Errorf("path escapes sandbox workdir")
	}
	return nil
}

func (b *FsBridge) validateParentBeforeCreate(ctx context.Context, dir string) error {
	realWorkdir, err := b.containerRealWorkdir(ctx)
	if err != nil {
		return err
	}
	current := filepath.Clean(dir)
	for {
		realParent, err := b.containerRealPath(ctx, current)
		if err == nil {
			if !fsBridgePathWithin(realWorkdir, realParent) {
				return fmt.Errorf("path parent escapes sandbox workdir")
			}
			return nil
		}
		next := filepath.Dir(current)
		if next == current {
			return fmt.Errorf("path parent does not exist inside sandbox workdir")
		}
		current = next
	}
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
