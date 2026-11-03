//go:build windows

package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

type envEntry struct{ Key, Value string }

var envKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// parseEnv reads shell-style `KEY=VAL` lines as written by the onboarding
// wizard (`export KEY=VAL`). It strips one layer of matching quotes, skips
// comments and blank lines, never expands `$`, and ignores PATH because the
// existing files carry bash syntax there.
func parseEnv(r io.Reader) ([]envEntry, error) {
	var out []envEntry
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(sc.Text(), "\xef\xbb\xbf"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if rest, ok := strings.CutPrefix(line, "export"); ok && rest != "" && (rest[0] == ' ' || rest[0] == '\t') {
			line = strings.TrimSpace(rest)
		}
		key, value, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || !envKeyPattern.MatchString(key) || strings.EqualFold(key, "PATH") {
			continue
		}
		out = append(out, envEntry{Key: key, Value: unquote(strings.TrimSpace(value))})
	}
	return out, sc.Err()
}

func unquote(v string) string {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		return v[1 : len(v)-1]
	}
	return v
}

// mergeEnv combines layers; a later layer overrides an earlier one and the
// first appearance of a key fixes its position.
func mergeEnv(layers ...[]envEntry) []envEntry {
	index := map[string]int{}
	var out []envEntry
	for _, layer := range layers {
		for _, e := range layer {
			if i, ok := index[e.Key]; ok {
				out[i].Value = e.Value
				continue
			}
			index[e.Key] = len(out)
			out = append(out, e)
		}
	}
	return out
}

func formatEnv(entries []envEntry) string {
	var b strings.Builder
	for _, e := range entries {
		fmt.Fprintf(&b, "%s=\"%s\"\n", e.Key, e.Value)
	}
	return b.String()
}

func envLookup(entries []envEntry, key string) (string, bool) {
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Key == key {
			return entries[i].Value, true
		}
	}
	return "", false
}

func loadEnvFile(path string) ([]envEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parseEnv(f)
}

// restrictToCurrentUser removes inherited permissions and leaves full control
// to the current user only.
func restrictToCurrentUser(path string) error {
	u, err := user.Current()
	if err != nil {
		return err
	}
	cmd := exec.Command("icacls", path, "/inheritance:r", "/grant:r", u.Username+":(F)")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("icacls: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// writePrivateFile replaces path atomically with data readable by the current
// user only. An existing file is kept as path.bak-<timestamp>.
func writePrivateFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if old, err := os.ReadFile(path); err == nil {
		bak := path + ".bak-" + time.Now().Format("20060102-150405")
		if err := os.WriteFile(bak, old, 0o600); err != nil {
			return err
		}
		if err := restrictToCurrentUser(bak); err != nil {
			return err
		}
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := restrictToCurrentUser(tmp); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

type importResult struct {
	Keys         int
	ConfigCopied bool
}

// importFromDir builds the app env file from <dir>/.env then <dir>/.env.local
// (the same order the current watchdog uses) and copies <dir>/config.json when
// present. The originals are never modified.
func importFromDir(srcDir, dstEnv, dstConfig string) (importResult, error) {
	var layers [][]envEntry
	for _, name := range []string{".env", ".env.local"} {
		entries, err := loadEnvFile(filepath.Join(srcDir, name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return importResult{}, fmt.Errorf("read %s: %w", name, err)
		}
		layers = append(layers, entries)
	}
	if len(layers) == 0 {
		return importResult{}, fmt.Errorf("no .env or .env.local in %s", srcDir)
	}
	merged := mergeEnv(layers...)
	if _, ok := envLookup(merged, "GOCLAW_POSTGRES_DSN"); !ok {
		return importResult{}, errors.New("GOCLAW_POSTGRES_DSN not found in the selected folder")
	}
	if err := writePrivateFile(dstEnv, []byte(formatEnv(merged))); err != nil {
		return importResult{}, err
	}
	res := importResult{Keys: len(merged)}
	if cfg, err := os.ReadFile(filepath.Join(srcDir, "config.json")); err == nil {
		if err := writePrivateFile(dstConfig, cfg); err != nil {
			return res, err
		}
		res.ConfigCopied = true
	}
	return res, nil
}
