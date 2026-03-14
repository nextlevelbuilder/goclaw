// Package sandbox — fsbridge.go provides sandboxed file operations via sandbox exec.
// Matching TS src/agents/sandbox/fs-bridge.ts.
//
// When sandbox is enabled, file tools (read_file, write_file, list_files)
// route through FsBridge instead of direct host filesystem access.
// All operations execute inside the sandbox via the Sandbox interface.
package sandbox

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// FsBridge provides sandboxed file operations via the Sandbox interface.
// Matching TS SandboxFsBridge in fs-bridge.ts.
type FsBridge struct {
	sb      Sandbox
	workdir string // container-side working directory (e.g. "/workspace")
}

// NewFsBridge creates a bridge to a running sandbox.
func NewFsBridge(sb Sandbox, workdir string) *FsBridge {
	if workdir == "" {
		workdir = "/workspace"
	}
	return &FsBridge{
		sb:      sb,
		workdir: workdir,
	}
}

// ReadFile reads file contents from inside the sandbox.
// Matching TS FsBridge.readFile().
func (b *FsBridge) ReadFile(ctx context.Context, path string) (string, error) {
	resolved := b.resolvePath(path)

	result, err := b.sb.Exec(ctx, []string{"cat", "--", resolved}, "")
	if err != nil {
		return "", fmt.Errorf("fsbridge read: %w", err)
	}
	if result.ExitCode != 0 {
		return "", fmt.Errorf("read failed: %s", strings.TrimSpace(result.Stderr))
	}

	return result.Stdout, nil
}

// WriteFile writes content to a file inside the sandbox, creating directories as needed.
// Matching TS FsBridge.writeFile().
func (b *FsBridge) WriteFile(ctx context.Context, path, content string) error {
	resolved := b.resolvePath(path)

	// Create parent directory
	dir := resolved[:strings.LastIndex(resolved, "/")]
	if dir != "" && dir != "/" {
		_, _ = b.sb.Exec(ctx, []string{"mkdir", "-p", dir}, "")
	}

	// Write content via stdin pipe
	result, err := b.sb.ExecWithStdin(ctx, []string{"sh", "-c", fmt.Sprintf("cat > %q", resolved)}, "", []byte(content))
	if err != nil {
		return fmt.Errorf("fsbridge write: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("write failed: %s", strings.TrimSpace(result.Stderr))
	}

	return nil
}

// ListDir lists files and directories inside the sandbox.
// Matching TS FsBridge.readdir().
func (b *FsBridge) ListDir(ctx context.Context, path string) (string, error) {
	resolved := b.resolvePath(path)

	// Use ls -la for detailed listing
	result, err := b.sb.Exec(ctx, []string{"ls", "-la", "--", resolved}, "")
	if err != nil {
		return "", fmt.Errorf("fsbridge list: %w", err)
	}
	if result.ExitCode != 0 {
		return "", fmt.Errorf("list failed: %s", strings.TrimSpace(result.Stderr))
	}

	return result.Stdout, nil
}

// Stat checks if a path exists and returns basic info.
func (b *FsBridge) Stat(ctx context.Context, path string) (string, error) {
	resolved := b.resolvePath(path)

	result, err := b.sb.Exec(ctx, []string{"stat", "--", resolved}, "")
	if err != nil {
		return "", fmt.Errorf("fsbridge stat: %w", err)
	}
	if result.ExitCode != 0 {
		return "", fmt.Errorf("stat failed: %s", strings.TrimSpace(result.Stderr))
	}

	return result.Stdout, nil
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
