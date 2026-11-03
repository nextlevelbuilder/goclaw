//go:build windows

package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const defaultPort = 18800

var errNeedsSetup = errors.New("no settings imported yet")

// childEnv is an environment block with Windows' case-insensitive names.
type childEnv []string

func (e *childEnv) set(key, value string) {
	for i, kv := range *e {
		if k, _, _ := strings.Cut(kv, "="); strings.EqualFold(k, key) {
			(*e)[i] = key + "=" + value
			return
		}
	}
	*e = append(*e, key+"="+value)
}

func (e childEnv) get(key string) string {
	for _, kv := range e {
		if k, v, _ := strings.Cut(kv, "="); strings.EqualFold(k, key) {
			return v
		}
	}
	return ""
}

// runtimeConfig is everything derived from the imported settings.
type runtimeConfig struct {
	Server    serverConfig
	PostgresA string // host:port of Postgres, for the preflight check
}

// buildRuntime turns the imported env file into a server configuration.
// installDir holds goclaw-server.exe and the pg_dump shim; extra overrides are
// applied last (for example GOCLAW_AUTO_UPGRADE for a one-off start).
func buildRuntime(p appPaths, installDir string, extra map[string]string) (runtimeConfig, error) {
	entries, err := loadEnvFile(p.EnvFile)
	if errors.Is(err, os.ErrNotExist) {
		return runtimeConfig{}, errNeedsSetup
	}
	if err != nil {
		return runtimeConfig{}, fmt.Errorf("read settings: %w", err)
	}
	dsn, ok := envLookup(entries, "GOCLAW_POSTGRES_DSN")
	if !ok || dsn == "" {
		return runtimeConfig{}, errors.New("GOCLAW_POSTGRES_DSN is missing in the imported settings")
	}
	host, port, err := postgresAddr(dsn)
	if err != nil {
		return runtimeConfig{}, err
	}

	srvPort := defaultPort
	if v, ok := envLookup(entries, "GOCLAW_PORT"); ok && v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 65535 {
			return runtimeConfig{}, fmt.Errorf("GOCLAW_PORT %q is not a valid port", v)
		}
		srvPort = n
	}

	// Settings come only from the imported file: ambient GOCLAW_* variables of
	// whoever launched the app must not change what the server does.
	var env childEnv
	for _, kv := range os.Environ() {
		if k, _, _ := strings.Cut(kv, "="); !strings.HasPrefix(strings.ToUpper(k), "GOCLAW_") {
			env = append(env, kv)
		}
	}
	for _, e := range entries {
		env.set(e.Key, e.Value)
	}
	// Loopback only unless the imported settings say otherwise.
	if !hasKey(entries, "GOCLAW_HOST") {
		env.set("GOCLAW_HOST", "127.0.0.1")
	}
	env.set("GOCLAW_PORT", strconv.Itoa(srvPort))
	if _, err := os.Stat(p.ConfigFile); err == nil {
		env.set("GOCLAW_CONFIG", p.ConfigFile)
	}
	logPath := env.get("GOCLAW_LOG_FILE")
	if logPath == "" {
		logPath = p.ServerLog
		env.set("GOCLAW_LOG_FILE", logPath)
	}
	// The window's origin must be accepted whenever an allowlist is configured.
	if origins := env.get("GOCLAW_ALLOWED_ORIGINS"); strings.TrimSpace(origins) != "" {
		own := "http://127.0.0.1:" + strconv.Itoa(srvPort)
		if !originListed(origins, own) {
			env.set("GOCLAW_ALLOWED_ORIGINS", origins+","+own)
		}
	}
	// pg_dump shim next to the executable, visible to the child only.
	env.set("PATH", installDir+string(os.PathListSeparator)+env.get("PATH"))
	for k, v := range extra {
		env.set(k, v)
	}

	exe := os.Getenv("GOCLAW_SERVER_EXE")
	if exe == "" {
		exe = filepath.Join(installDir, "goclaw-server.exe")
	}
	return runtimeConfig{
		PostgresA: net.JoinHostPort(host, port),
		Server: serverConfig{
			Exe:        exe,
			Dir:        p.ConfigDir,
			Env:        env,
			Port:       srvPort,
			LogPath:    logPath,
			StderrPath: p.StderrLog,
			PidPath:    p.PidFile,
		},
	}, nil
}

func hasKey(entries []envEntry, key string) bool {
	_, ok := envLookup(entries, key)
	return ok
}

func originListed(list, origin string) bool {
	for _, o := range strings.Split(list, ",") {
		if strings.TrimSpace(o) == origin || strings.TrimSpace(o) == "*" {
			return true
		}
	}
	return false
}
