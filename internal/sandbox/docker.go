package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"maps"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// CheckDockerAvailable verifies that the Docker CLI and daemon are accessible.
// Returns nil if Docker is ready, or an error describing the failure.
func CheckDockerAvailable(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "docker", "info", "--format", "{{.ServerVersion}}").CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker not available: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// DockerSandbox is a sandbox backed by a Docker container.
type DockerSandbox struct {
	containerID string
	config      Config
	workspace   string
	createdAt   time.Time
	lastUsed    time.Time
	mu          sync.Mutex // protects lastUsed
}

// newDockerSandbox creates and starts a Docker container for sandboxed execution.
// Matching TS buildSandboxCreateArgs() + createSandboxContainer().
func newDockerSandbox(ctx context.Context, name string, cfg Config, workspace string) (*DockerSandbox, error) {
	args := []string{
		"run", "-d",
		"--name", name,
		"--label", "goclaw.sandbox=true",
	}

	// Security hardening (matching TS buildSandboxCreateArgs)
	if cfg.ReadOnlyRoot {
		args = append(args, "--read-only")
	}
	for _, t := range cfg.Tmpfs {
		if !strings.Contains(t, ":") {
			// Always add security flags; optionally add size limit
			opts := "noexec,nosuid,nodev"
			if cfg.TmpfsSizeMB > 0 {
				opts = fmt.Sprintf("size=%dm,%s", cfg.TmpfsSizeMB, opts)
			}
			t = fmt.Sprintf("%s:%s", t, opts)
		} else if !strings.Contains(t, "noexec") {
			// User-specified options but missing noexec — append security flags
			t += ",noexec,nosuid,nodev"
		}
		args = append(args, "--tmpfs", t)
	}
	for _, cap := range cfg.CapDrop {
		args = append(args, "--cap-drop", cap)
	}
	args = append(args, "--security-opt", "no-new-privileges")

	// Non-root user (reduces attack surface)
	if cfg.User != "" {
		args = append(args, "--user", cfg.User)
	}

	// Resource limits
	if cfg.MemoryMB > 0 {
		args = append(args, "--memory", fmt.Sprintf("%dm", cfg.MemoryMB))
	}
	if cfg.CPUs > 0 {
		args = append(args, "--cpus", fmt.Sprintf("%.1f", cfg.CPUs))
	}
	if cfg.PidsLimit > 0 {
		args = append(args, "--pids-limit", fmt.Sprintf("%d", cfg.PidsLimit))
	}

	// Network
	if !cfg.NetworkEnabled {
		args = append(args, "--network", "none")
	}

	// Workspace mount — resolve host path for DooD (Docker-out-of-Docker) setups.
	containerWorkdir := cfg.ContainerWorkdir()
	if workspace != "" && cfg.WorkspaceAccess != AccessNone {
		mountOpt := "rw"
		if cfg.WorkspaceAccess == AccessRO {
			mountOpt = "ro"
		}
		hostPath := resolveHostWorkspacePath(ctx, workspace)
		args = append(args, "-v", fmt.Sprintf("%s:%s:%s", hostPath, containerWorkdir, mountOpt))
	}
	args = append(args, "-w", containerWorkdir)

	// Environment variables
	for k, v := range cfg.Env {
		args = append(args, "-e", k+"="+v)
	}

	// Image + keep-alive command
	args = append(args, cfg.Image, "sleep", "infinity")

	slog.Debug("creating sandbox container", "name", name, "args", args)

	cmd := exec.CommandContext(ctx, "docker", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("docker run failed: %w\nstderr: %s", err, stderr.String())
	}

	containerID := strings.TrimSpace(stdout.String())
	if len(containerID) > 12 {
		containerID = containerID[:12]
	}

	slog.Info("sandbox container created", "id", containerID, "name", name, "image", cfg.Image)

	// Run optional setup command (matching TS setupCommand)
	if cfg.SetupCommand != "" {
		setupCmd := exec.CommandContext(ctx, "docker", "exec", "-i", containerID, "sh", "-lc", cfg.SetupCommand)
		if out, err := setupCmd.CombinedOutput(); err != nil {
			slog.Warn("sandbox setup command failed", "id", containerID, "error", err, "output", string(out))
		} else {
			slog.Info("sandbox setup command completed", "id", containerID)
		}
	}

	now := time.Now()
	return &DockerSandbox{
		containerID: containerID,
		config:      cfg,
		workspace:   workspace,
		createdAt:   now,
		lastUsed:    now,
	}, nil
}

// Exec runs a command inside the container.
// Optional ExecOption (e.g. WithEnv) injects per-call env vars via docker exec -e.
func (s *DockerSandbox) Exec(ctx context.Context, command []string, workDir string, opts ...ExecOption) (*ExecResult, error) {
	s.mu.Lock()
	s.lastUsed = time.Now()
	s.mu.Unlock()

	timeout := time.Duration(s.config.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}

	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	o := ApplyExecOpts(opts)

	args := []string{"exec"}
	// Inject env vars as -e flags before containerID (credentialed exec)
	for k, v := range o.Env {
		args = append(args, "-e", k+"="+v)
	}
	if workDir != "" {
		args = append(args, "-w", workDir)
	}
	args = append(args, s.containerID)
	args = append(args, command...)

	cmd := exec.CommandContext(execCtx, "docker", args...)

	// Limit output capture to prevent OOM from large command output
	maxOut := s.config.MaxOutputBytes
	if maxOut <= 0 {
		maxOut = 1 << 20 // 1MB default
	}
	stdout := &limitedBuffer{max: maxOut}
	stderr := &limitedBuffer{max: maxOut}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("docker exec: %w", err)
		}
	}

	result := &ExecResult{
		ExitCode: exitCode,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
	}
	if stdout.truncated {
		result.Stdout += "\n...[output truncated]"
	}
	if stderr.truncated {
		result.Stderr += "\n...[output truncated]"
	}
	return result, nil
}

// Destroy removes the container.
func (s *DockerSandbox) Destroy(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker", "rm", "-f", s.containerID)
	if err := cmd.Run(); err != nil {
		slog.Warn("failed to remove sandbox container", "id", s.containerID, "error", err)
		return err
	}
	slog.Info("sandbox container destroyed", "id", s.containerID)
	return nil
}

// ID returns the container ID.
func (s *DockerSandbox) ID() string { return s.containerID }

// DockerManager manages Docker sandbox containers based on scope.
type DockerManager struct {
	config    Config
	sandboxes map[string]*DockerSandbox
	mu        sync.RWMutex
	stopCh    chan struct{} // signals pruning goroutine to stop
}

// NewDockerManager creates a manager for Docker sandboxes.
// Automatically starts background pruning if configured.
func NewDockerManager(cfg Config) *DockerManager {
	m := &DockerManager{
		config:    cfg,
		sandboxes: make(map[string]*DockerSandbox),
		stopCh:    make(chan struct{}),
	}
	m.startPruning()
	return m
}

// Get returns an existing sandbox or creates a new one for the given key.
// If cfgOverride is non-nil, it is used for new containers instead of the global config.
func (m *DockerManager) Get(ctx context.Context, key string, workspace string, cfgOverride *Config) (Sandbox, error) {
	cfg := m.config
	if cfgOverride != nil {
		cfg = *cfgOverride
	}
	if cfg.Mode == ModeOff {
		return nil, ErrSandboxDisabled
	}

	m.mu.RLock()
	if sb, ok := m.sandboxes[key]; ok {
		m.mu.RUnlock()
		return sb, nil
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check
	if sb, ok := m.sandboxes[key]; ok {
		return sb, nil
	}

	prefix := cfg.ContainerPrefix
	if prefix == "" {
		prefix = "goclaw-sbx-"
	}
	name := prefix + sanitizeKey(key)
	sb, err := newDockerSandbox(ctx, name, cfg, workspace)
	if err != nil {
		return nil, err
	}

	m.sandboxes[key] = sb
	return sb, nil
}

// Release destroys a sandbox by key.
func (m *DockerManager) Release(ctx context.Context, key string) error {
	m.mu.Lock()
	sb, ok := m.sandboxes[key]
	if ok {
		delete(m.sandboxes, key)
	}
	m.mu.Unlock()

	if ok {
		return sb.Destroy(ctx)
	}
	return nil
}

// ReleaseAll destroys all active sandboxes.
func (m *DockerManager) ReleaseAll(ctx context.Context) error {
	m.mu.Lock()
	sbs := make(map[string]*DockerSandbox, len(m.sandboxes))
	maps.Copy(sbs, m.sandboxes)
	m.sandboxes = make(map[string]*DockerSandbox)
	m.mu.Unlock()

	for key, sb := range sbs {
		if err := sb.Destroy(ctx); err != nil {
			slog.Warn("failed to release sandbox", "key", key, "error", err)
		}
	}
	return nil
}

// Stats returns information about active sandboxes.
func (m *DockerManager) Stats() map[string]any {
	m.mu.RLock()
	defer m.mu.RUnlock()

	containers := make(map[string]string, len(m.sandboxes))
	for key, sb := range m.sandboxes {
		containers[key] = sb.containerID
	}

	return map[string]any{
		"mode":       m.config.Mode,
		"image":      m.config.Image,
		"active":     len(m.sandboxes),
		"containers": containers,
	}
}

// Stop signals the pruning goroutine to stop.
// Called during shutdown before ReleaseAll.
func (m *DockerManager) Stop() {
	select {
	case <-m.stopCh:
		// already closed
	default:
		close(m.stopCh)
	}
}

// startPruning launches a background goroutine that periodically prunes idle/old containers.
// Matching TS maybePruneSandboxes().
func (m *DockerManager) startPruning() {
	interval := time.Duration(m.config.PruneIntervalMin) * time.Minute
	if interval <= 0 {
		interval = 5 * time.Minute
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-m.stopCh:
				return
			case <-ticker.C:
				m.Prune(context.Background())
			}
		}
	}()

	slog.Debug("sandbox pruning started", "interval", interval)
}

// Prune removes containers that are idle too long or exceed max age.
// Matching TS SandboxPruneSettings (idleHours, maxAgeDays).
func (m *DockerManager) Prune(ctx context.Context) {
	idleHours := m.config.IdleHours
	if idleHours <= 0 {
		idleHours = 24
	}
	maxAgeDays := m.config.MaxAgeDays
	if maxAgeDays <= 0 {
		maxAgeDays = 7
	}

	now := time.Now()
	idleThreshold := now.Add(-time.Duration(idleHours) * time.Hour)
	ageThreshold := now.Add(-time.Duration(maxAgeDays) * 24 * time.Hour)

	// Collect keys to prune
	m.mu.RLock()
	var toRemove []string
	for key, sb := range m.sandboxes {
		sb.mu.Lock()
		lastUsed := sb.lastUsed
		created := sb.createdAt
		sb.mu.Unlock()

		if lastUsed.Before(idleThreshold) || created.Before(ageThreshold) {
			toRemove = append(toRemove, key)
		}
	}
	m.mu.RUnlock()

	if len(toRemove) == 0 {
		return
	}

	// Remove them
	for _, key := range toRemove {
		m.mu.Lock()
		sb, ok := m.sandboxes[key]
		if ok {
			delete(m.sandboxes, key)
		}
		m.mu.Unlock()

		if ok {
			if err := sb.Destroy(ctx); err != nil {
				slog.Warn("prune: failed to destroy sandbox", "key", key, "error", err)
			} else {
				slog.Info("pruned idle sandbox container", "key", key, "container", sb.containerID)
			}
		}
	}

	slog.Info("sandbox prune completed", "removed", len(toRemove))
}

// sanitizeKey makes a key safe for Docker container names.
func sanitizeKey(key string) string {
	safe := strings.NewReplacer(
		":", "-",
		"/", "-",
		" ", "-",
		".", "-",
	).Replace(key)

	if len(safe) > 50 {
		safe = safe[:50]
	}
	return safe
}

// limitedBuffer is a bytes.Buffer that stops accepting writes after max bytes.
// Prevents OOM when commands produce large output.
type limitedBuffer struct {
	buf       bytes.Buffer
	max       int
	truncated bool
}

func (lb *limitedBuffer) Write(p []byte) (int, error) {
	if lb.truncated {
		return len(p), nil // discard silently
	}
	remaining := lb.max - lb.buf.Len()
	if remaining <= 0 {
		lb.truncated = true
		return len(p), nil
	}
	if len(p) > remaining {
		lb.buf.Write(p[:remaining])
		lb.truncated = true
		return len(p), nil
	}
	return lb.buf.Write(p)
}

func (lb *limitedBuffer) String() string {
	return lb.buf.String()
}
