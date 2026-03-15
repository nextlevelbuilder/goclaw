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

// ContainerWorkdir returns the container-side working directory.
func (c Config) ContainerWorkdir() string {
	if c.Workdir != "" {
		return c.Workdir
	}
	return "/workspace"
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
}

// WithEnv injects additional environment variables into the sandbox exec call.
// Used by credentialed exec to pass credentials without shell interpretation.
func WithEnv(env map[string]string) ExecOption {
	return func(o *ExecOpts) { o.Env = env }
}

// ApplyExecOpts resolves variadic ExecOption into ExecOpts.
func ApplyExecOpts(opts []ExecOption) ExecOpts {
	var o ExecOpts
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// Sandbox is the interface for sandboxed code execution.
type Sandbox interface {
	// Exec runs a command inside the sandbox and returns the result.
	// Optional ExecOption (e.g. WithEnv) configures per-call behavior.
	Exec(ctx context.Context, command []string, workDir string, opts ...ExecOption) (*ExecResult, error)

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
