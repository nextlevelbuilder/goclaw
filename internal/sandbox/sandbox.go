// Package sandbox provides Docker-based code execution isolation.
//
// Agents can run tool commands (exec, shell) inside Docker containers
// instead of the host system. Sandbox modes:
//   - off: no sandboxing, execute directly on host
//   - non-main: all agents except "main" run in sandbox
//   - all: every agent runs in sandbox
//
// Workspace access levels:
//   - none: no filesystem access
//   - ro: read-only workspace mount
//   - rw: read-write workspace mount
//
// Sandbox scope controls container reuse:
//   - session: one container per session (max isolation)
//   - agent: shared container per agent
//   - shared: one container for all agents
package sandbox

import (
	"context"
	"fmt"
	"strings"
)

// Mode determines which agents are sandboxed.
type Mode string

const (
	ModeOff     Mode = "off"      // no sandbox
	ModeNonMain Mode = "non-main" // all except "main" agent
	ModeAll     Mode = "all"      // every agent
)

// Access determines workspace filesystem permissions.
type Access string

const (
	AccessNone Access = "none" // no filesystem
	AccessRO   Access = "ro"   // read-only
	AccessRW   Access = "rw"   // read-write
)

// Scope determines container reuse granularity.
type Scope string

const (
	ScopeSession Scope = "session" // one container per session
	ScopeAgent   Scope = "agent"   // one container per agent
	ScopeShared  Scope = "shared"  // one container for all
)

// Config configures the sandbox system.
// Matches TS SandboxDockerSettings + SandboxConfig.
type Config struct {
	Mode              Mode              `json:"mode"`
	Image             string            `json:"image"`
	WorkspaceAccess   Access            `json:"workspace_access"`
	Scope             Scope             `json:"scope"`
	MemoryMB          int               `json:"memory_mb"`
	CPUs              float64           `json:"cpus"`
	TimeoutSec        int               `json:"timeout_sec"`
	NetworkEnabled    bool              `json:"network_enabled"`
	RestrictedDomains []string          `json:"restricted_domains,omitempty"`
	Env               map[string]string `json:"env,omitempty"`

	// Security hardening (matching TS buildSandboxCreateArgs)
	ReadOnlyRoot    bool     `json:"read_only_root"`
	CapDrop         []string `json:"cap_drop,omitempty"`
	Tmpfs           []string `json:"tmpfs,omitempty"`         // e.g. "/tmp", "/tmp:size=64m"
	TmpfsSizeMB     int      `json:"tmpfs_size_mb,omitempty"` // default size for tmpfs mounts without explicit :size= (0 = Docker default)
	PidsLimit       int      `json:"pids_limit,omitempty"`
	User            string   `json:"user,omitempty"`             // container user (e.g. "1000:1000", "nobody")
	MaxOutputBytes  int      `json:"max_output_bytes,omitempty"` // limit exec stdout+stderr capture (default 1MB, 0 = unlimited)
	SetupCommand    string   `json:"setup_command,omitempty"`
	ContainerPrefix string   `json:"container_prefix,omitempty"`
	Workdir         string   `json:"workdir,omitempty"` // container workdir (default "/workspace")

	// Pruning (matching TS SandboxPruneSettings)
	IdleHours        int `json:"idle_hours,omitempty"`         // prune containers idle > N hours (default 24)
	MaxAgeDays       int `json:"max_age_days,omitempty"`       // prune containers older than N days (default 7)
	PruneIntervalMin int `json:"prune_interval_min,omitempty"` // check interval in minutes (default 5)
}

// DefaultConfig returns sensible defaults matching TS sandbox defaults.
func DefaultConfig() Config {
	return Config{
		Mode:             ModeOff,
		Image:            "goclaw-sandbox:bookworm-slim",
		WorkspaceAccess:  AccessRW,
		Scope:            ScopeSession,
		MemoryMB:         512,
		CPUs:             1.0,
		TimeoutSec:       300,
		NetworkEnabled:   false,
		ReadOnlyRoot:     true,
		CapDrop:          []string{"ALL"},
		Tmpfs:            []string{"/tmp", "/var/tmp", "/run"},
		PidsLimit:        256,
		MaxOutputBytes:   1 << 20, // 1MB
		ContainerPrefix:  "goclaw-sbx-",
		Workdir:          "/workspace",
		IdleHours:        24,
		MaxAgeDays:       7,
		PruneIntervalMin: 5,
	}
}

// ShouldSandbox returns true if the given agent should run in a sandbox.
func (c Config) ShouldSandbox(agentID string) bool {
	switch c.Mode {
	case ModeAll:
		return true
	case ModeNonMain:
		return agentID != "main" && agentID != "default"
	default:
		return false
	}
}

// DefaultContainerWorkdir is the default container-side working directory
// used when no custom Workdir is configured.
const DefaultContainerWorkdir = "/workspace"

// ContainerWorkdir returns the container-side working directory.
func (c Config) ContainerWorkdir() string {
	if c.Workdir != "" {
		return c.Workdir
	}
	return DefaultContainerWorkdir
}

// ResolveScopeKey maps a session key to a sandbox scope key.
// Matching TS resolveSandboxScopeKey().
func (c Config) ResolveScopeKey(sessionKey string) string {
	switch c.Scope {
	case ScopeShared:
		return "shared"
	case ScopeAgent:
		// Extract agent ID from session key "agent:{agentId}:{rest}"
		parts := strings.SplitN(sessionKey, ":", 3)
		if len(parts) >= 2 {
			return "agent:" + parts[1]
		}
		return "agent:default"
	default: // ScopeSession
		if sessionKey == "" {
			return "default"
		}
		return sessionKey
	}
}

// ExecResult is the output of a command executed in a sandbox container.
type ExecResult struct {
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
}

// ExecOption configures optional behavior for sandbox Exec calls.
type ExecOption func(*ExecOpts)

// ExecOpts holds optional settings applied via ExecOption.
type ExecOpts struct {
	Env map[string]string // extra env vars injected into the container exec
	// StdoutLine, when set, is called with each complete line of stdout AS IT IS
	// PRODUCED (not just at the end) — used to stream a long exec's progress
	// (e.g. a delegated Claude Code run) back to the user in real time. The full
	// stdout is still buffered and returned in ExecResult.
	StdoutLine func(line string)
	// StderrLine is the stderr counterpart of StdoutLine. It matters most for
	// ExecInteractive, where a CLI's diagnostics arrive interleaved with its
	// protocol output on stdout and there is no "end of run" at which to inspect
	// them. The full stderr is still buffered and returned in ExecResult.
	StderrLine func(line string)
}

// WithEnv injects additional environment variables into the sandbox exec call.
// Used by credentialed exec to pass credentials without shell interpretation.
func WithEnv(env map[string]string) ExecOption {
	return func(o *ExecOpts) { o.Env = env }
}

// WithStdoutLine streams each complete stdout line to cb as it is produced,
// while still buffering the full output for the returned ExecResult.
func WithStdoutLine(cb func(line string)) ExecOption {
	return func(o *ExecOpts) { o.StdoutLine = cb }
}

// WithStderrLine streams each complete stderr line to cb as it is produced,
// while still buffering the full output for the returned ExecResult.
func WithStderrLine(cb func(line string)) ExecOption {
	return func(o *ExecOpts) { o.StderrLine = cb }
}

// ApplyExecOpts resolves variadic ExecOption into ExecOpts.
func ApplyExecOpts(opts []ExecOption) ExecOpts {
	var o ExecOpts
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// InteractiveSession is a live handle on a long-running process started by
// Sandbox.ExecInteractive: stdin stays open for writing while stdout/stderr are
// line-streamed to the callbacks supplied via WithStdoutLine/WithStderrLine.
//
// It exists so goclaw can hold a bidirectional conversation with a coding CLI
// (`claude --input-format stream-json --output-format stream-json`): user turns
// and permission answers go IN via WriteLine, events and permission requests
// come OUT as streamed lines.
//
// A session is safe for concurrent use: WriteLine may be called from a different
// goroutine than the one reading lines or waiting on Done.
type InteractiveSession interface {
	// WriteLine sends one line to the process's stdin, appending a trailing
	// newline if s does not already end with one. It returns ErrSessionClosed if
	// the session was closed or the process has already exited — it never panics
	// on a closed pipe.
	WriteLine(s string) error

	// Wait blocks until the process exits and returns its result (exit code plus
	// the full captured stdout/stderr, subject to Config.MaxOutputBytes). It may
	// be called more than once and from several goroutines; every caller gets the
	// same result. A non-zero exit status is NOT an error — it is reported via
	// ExecResult.ExitCode, matching Exec. The error is non-nil only when the
	// process could not be run/reaped, or when the caller's ctx was cancelled out
	// from under a live session (a Close() is the normal end of a session and
	// reports no error).
	Wait() (*ExecResult, error)

	// Close terminates the process and releases resources. It is idempotent and
	// safe to call concurrently with Wait.
	Close() error

	// Done is closed once the process has exited, so callers can select on it
	// alongside their own context.
	Done() <-chan struct{}

	// ExitCode reports the process's exit status. It is only valid after Done is
	// closed; before that it returns 0.
	ExitCode() int
}

// Sandbox is the interface for sandboxed code execution.
type Sandbox interface {
	// Exec runs a command inside the sandbox and returns the result.
	// Optional ExecOption (e.g. WithEnv) configures per-call behavior.
	Exec(ctx context.Context, command []string, workDir string, opts ...ExecOption) (*ExecResult, error)

	// ExecInteractive starts a long-lived process with a writable stdin and
	// line-streamed stdout/stderr. It returns once the process has STARTED
	// (not when it exits) — the caller then drives it through the returned
	// InteractiveSession and must Close() it to release the process.
	//
	// Unlike Exec, Config.TimeoutSec is NOT applied: an interactive session is
	// long-lived by nature. The supplied ctx is the only deadline — cancelling it
	// kills the process.
	ExecInteractive(ctx context.Context, command []string, workDir string, opts ...ExecOption) (InteractiveSession, error)

	// Destroy removes the sandbox container and cleans up resources.
	Destroy(ctx context.Context) error

	// ID returns the sandbox's unique identifier (container ID).
	ID() string
}

// Manager manages sandbox lifecycle based on scope.
type Manager interface {
	// Get returns (or creates) a sandbox for the given scope key.
	// For session scope: key = sessionKey
	// For agent scope: key = agentID
	// For shared scope: key = "shared"
	// If cfgOverride is non-nil, it is used instead of the global config for new containers.
	Get(ctx context.Context, key string, workspace string, cfgOverride *Config) (Sandbox, error)

	// BaseConfig returns a copy of the manager's global sandbox config. Callers
	// that need to run a one-off container with a tweaked setting (e.g. network
	// enabled) start from this so the image and other defaults are preserved.
	BaseConfig() Config

	// Release destroys a sandbox by key.
	Release(ctx context.Context, key string) error

	// ReleaseAll destroys all active sandboxes.
	ReleaseAll(ctx context.Context) error

	// Stop signals background goroutines (pruning) to stop.
	Stop()

	// Stats returns info about active sandboxes.
	Stats() map[string]any
}

// ErrSandboxDisabled is returned when sandbox mode is "off".
var ErrSandboxDisabled = fmt.Errorf("sandbox is disabled")
