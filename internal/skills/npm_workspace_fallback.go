package skills

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var npmPackageVersionResolver = npmViewPackageVersion

func installNpmPackage(ctx context.Context, target string) ([]byte, error) {
	out, err := runNpmInstall(ctx, target)
	if err == nil || !npmOutputHasWorkspaceProtocolError(string(out)) {
		return out, err
	}

	slog.Warn("skills: npm package contains workspace protocol deps; retrying with sanitized tarball", "target", target)
	fallbackOut, fallbackErr := installNpmPackageWithWorkspaceRewrite(ctx, target)
	if fallbackErr == nil {
		return fallbackOut, nil
	}
	return appendNpmFallbackOutput(out, fallbackOut), fallbackErr
}

func runNpmInstall(ctx context.Context, target string) ([]byte, error) {
	if err := os.MkdirAll(npmGlobalPrefix(), 0o750); err != nil {
		return nil, fmt.Errorf("npm prefix setup: %w", err)
	}
	ensureNpmGlobalEnv()
	cmd := exec.CommandContext(ctx, npmBinary, "install", "-g", target)
	cmd.Env = npmCommandEnv()
	cmd.WaitDelay = 2 * time.Second
	return cmd.CombinedOutput()
}

func npmOutputHasWorkspaceProtocolError(out string) bool {
	return strings.Contains(out, "EUNSUPPORTEDPROTOCOL") &&
		(strings.Contains(out, `Unsupported URL Type "workspace:"`) || strings.Contains(out, "workspace:"))
}

func installNpmPackageWithWorkspaceRewrite(ctx context.Context, target string) ([]byte, error) {
	tmpDir, err := os.MkdirTemp("", "goclaw-npm-workspace-*")
	if err != nil {
		return nil, fmt.Errorf("npm workspace fallback temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	tarball, packOut, err := npmPackTarball(ctx, target, tmpDir)
	if err != nil {
		return packOut, err
	}

	extractDir := filepath.Join(tmpDir, "extract")
	if err := os.MkdirAll(extractDir, 0o750); err != nil {
		return packOut, fmt.Errorf("npm workspace fallback extract dir: %w", err)
	}
	if err := extractNpmTarballToDir(tarball, extractDir); err != nil {
		return packOut, err
	}

	packageDir := filepath.Join(extractDir, "package")
	rewrites, err := rewriteWorkspacePackageJSON(ctx, filepath.Join(packageDir, "package.json"))
	if err != nil {
		return packOut, err
	}
	if rewrites == 0 {
		return packOut, errors.New("npm workspace fallback found no workspace dependencies to rewrite")
	}

	repackDir := filepath.Join(tmpDir, "repack")
	if err := os.MkdirAll(repackDir, 0o750); err != nil {
		return packOut, fmt.Errorf("npm workspace fallback repack dir: %w", err)
	}
	sanitizedTarball, err := packNpmPackageDir(packageDir, repackDir)
	if err != nil {
		return packOut, err
	}

	installOut, err := runNpmInstall(ctx, sanitizedTarball)
	return appendNpmFallbackOutput(packOut, installOut), err
}

func npmPackTarball(ctx context.Context, target, destination string) (string, []byte, error) {
	cmd := exec.CommandContext(ctx, npmBinary, "pack", "--json", "--pack-destination", destination, target)
	cmd.Env = npmCommandEnv()
	cmd.WaitDelay = 2 * time.Second

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		out := appendNpmFallbackOutput(stdout.Bytes(), stderr.Bytes())
		return "", out, fmt.Errorf("npm pack fallback failed: %w", err)
	}

	var entries []struct {
		Filename string `json:"filename"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &entries); err != nil {
		return "", stdout.Bytes(), fmt.Errorf("npm pack fallback parse: %w", err)
	}
	if len(entries) == 0 || strings.TrimSpace(entries[0].Filename) == "" {
		return "", stdout.Bytes(), errors.New("npm pack fallback returned no tarball")
	}

	tarball := filepath.Join(destination, filepath.Base(entries[0].Filename))
	if _, err := os.Stat(tarball); err != nil {
		return "", stdout.Bytes(), fmt.Errorf("npm pack fallback tarball missing: %w", err)
	}
	return tarball, stdout.Bytes(), nil
}

func rewriteWorkspacePackageJSON(ctx context.Context, path string) (int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("read package.json: %w", err)
	}

	var pkg map[string]any
	if err := json.Unmarshal(raw, &pkg); err != nil {
		return 0, fmt.Errorf("parse package.json: %w", err)
	}

	rewrites := 0
	for _, section := range []string{"dependencies", "optionalDependencies", "peerDependencies"} {
		deps, ok := pkg[section].(map[string]any)
		if !ok {
			continue
		}
		for name, value := range deps {
			spec, ok := value.(string)
			if !ok || !strings.HasPrefix(spec, "workspace:") {
				continue
			}
			resolved, err := resolveWorkspaceDependencySpec(ctx, name, spec)
			if err != nil {
				return rewrites, err
			}
			deps[name] = resolved
			rewrites++
		}
	}

	if rewrites == 0 {
		return 0, nil
	}
	updated, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		return 0, fmt.Errorf("encode package.json: %w", err)
	}
	updated = append(updated, '\n')
	if err := os.WriteFile(path, updated, 0o600); err != nil {
		return 0, fmt.Errorf("write package.json: %w", err)
	}
	return rewrites, nil
}

func resolveWorkspaceDependencySpec(ctx context.Context, name, spec string) (string, error) {
	suffix := strings.TrimSpace(strings.TrimPrefix(spec, "workspace:"))
	switch suffix {
	case "", "*":
		return npmPackageVersionResolver(ctx, name)
	case "^", "~":
		version, err := npmPackageVersionResolver(ctx, name)
		if err != nil {
			return "", err
		}
		return suffix + version, nil
	default:
		if strings.HasPrefix(suffix, ".") || strings.HasPrefix(suffix, "/") {
			return "", fmt.Errorf("unsupported workspace dependency path for %s: %s", name, spec)
		}
		return suffix, nil
	}
}

func npmViewPackageVersion(ctx context.Context, name string) (string, error) {
	cmd := exec.CommandContext(ctx, npmBinary, "view", name, "version", "--json")
	cmd.Env = npmCommandEnv()
	cmd.WaitDelay = 2 * time.Second
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("npm view %s version: %w", name, err)
	}
	var version string
	if err := json.Unmarshal(out, &version); err != nil {
		version = strings.Trim(strings.TrimSpace(string(out)), `"`)
	}
	if version == "" {
		return "", fmt.Errorf("npm view %s version returned empty version", name)
	}
	return version, nil
}

func packNpmPackageDir(packageDir, destination string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(packageDir, "package.json"))
	if err != nil {
		return "", fmt.Errorf("read package.json for repack: %w", err)
	}
	var pkg struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(raw, &pkg); err != nil {
		return "", fmt.Errorf("parse package.json for repack: %w", err)
	}
	filename := npmTarballFilename(pkg.Name, pkg.Version)
	if filename == "" {
		return "", errors.New("npm workspace fallback cannot derive tarball filename")
	}

	tarball := filepath.Join(destination, filename)
	out, err := os.OpenFile(tarball, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", fmt.Errorf("create sanitized npm tarball: %w", err)
	}
	defer out.Close()

	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)
	walkErr := filepath.WalkDir(packageDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("npm workspace fallback unsupported package entry: %s", path)
		}
		rel, err := filepath.Rel(packageDir, path)
		if err != nil {
			return err
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(filepath.Join("package", rel))
		hdr.Mode = int64(info.Mode().Perm())
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tw, in)
		closeErr := in.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	closeTarErr := tw.Close()
	closeGzipErr := gz.Close()
	if walkErr != nil {
		return "", fmt.Errorf("pack sanitized npm tarball: %w", walkErr)
	}
	if closeTarErr != nil {
		return "", fmt.Errorf("close sanitized npm tarball: %w", closeTarErr)
	}
	if closeGzipErr != nil {
		return "", fmt.Errorf("close sanitized npm gzip: %w", closeGzipErr)
	}
	return tarball, nil
}

func npmTarballFilename(name, version string) string {
	name = strings.TrimSpace(name)
	version = strings.TrimSpace(version)
	if name == "" || version == "" {
		return ""
	}
	name = strings.TrimPrefix(name, "@")
	name = strings.ReplaceAll(name, "/", "-")
	name = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '.' || r == '_' || r == '-':
			return r
		default:
			return '-'
		}
	}, name)
	version = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '.' || r == '_' || r == '-':
			return r
		default:
			return '-'
		}
	}, version)
	return name + "-" + version + ".tgz"
}

func extractNpmTarballToDir(tarball, destination string) error {
	files, err := ExtractArchiveAs(tarball, filepath.Base(tarball), 50*1024*1024)
	if err != nil {
		return fmt.Errorf("extract npm tarball: %w", err)
	}

	cleanDest := filepath.Clean(destination)
	for _, file := range files {
		target := filepath.Join(cleanDest, file.Name)
		if !isPathWithin(target, cleanDest) {
			return fmt.Errorf("npm tarball contains unsafe path: %s", file.Name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return fmt.Errorf("extract npm tarball parent: %w", err)
		}
		mode := file.Mode.Perm()
		if mode == 0 {
			mode = 0o600
		}
		if err := os.WriteFile(target, file.Content, mode); err != nil {
			return fmt.Errorf("extract npm tarball file: %w", err)
		}
	}
	return nil
}

func isPathWithin(path, parent string) bool {
	cleanPath := filepath.Clean(path)
	cleanParent := filepath.Clean(parent)
	if cleanPath == cleanParent {
		return true
	}
	return strings.HasPrefix(cleanPath, cleanParent+string(os.PathSeparator))
}

func appendNpmFallbackOutput(parts ...[]byte) []byte {
	var out []byte
	for _, part := range parts {
		trimmed := bytes.TrimSpace(part)
		if len(trimmed) == 0 {
			continue
		}
		if len(out) > 0 {
			out = append(out, '\n')
		}
		out = append(out, trimmed...)
	}
	return out
}
