package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

// ACPProcess represents a running ACP agent subprocess.
type ACPProcess struct {
	cmd        *exec.Cmd
	conn       *Conn
	sessionID  string // ACP session ID from session/new
	agentCaps  AgentCaps
	lastActive time.Time
	inUse      atomic.Int32 // >0 means prompt active — reaper must skip
	mu         sync.Mutex
	ctx        context.Context
	cancel     context.CancelFunc
	exited     chan struct{} // closed when process exits

	// updateFn is called for session/update notifications during a prompt.
	updateFn func(SessionUpdate)
	updateMu sync.Mutex
}

// setUpdateFn sets the callback for session/update notifications (thread-safe).
func (p *ACPProcess) setUpdateFn(fn func(SessionUpdate)) {
	p.updateMu.Lock()
	p.updateFn = fn
	p.updateMu.Unlock()
}

// dispatchUpdate routes a session/update to the active prompt callback.
func (p *ACPProcess) dispatchUpdate(update SessionUpdate) {
	p.updateMu.Lock()
	fn := p.updateFn
	p.updateMu.Unlock()
	if fn != nil {
		fn(update)
	}
}

// ProcessPool manages a pool of ACP agent subprocesses keyed by session.
type ProcessPool struct {
	processes   sync.Map // sessionKey → *ACPProcess
	spawnMu     sync.Map // sessionKey → *sync.Mutex — prevents concurrent spawn
	agentBinary string
	agentArgs   []string
	workDir     string
	idleTTL     time.Duration
	mu          sync.RWMutex   // protects toolHandler
	toolHandler RequestHandler
	done        chan struct{}
	closeOnce   sync.Once
}

// NewProcessPool creates a pool that spawns ACP agents as subprocesses.
func NewProcessPool(binary string, args []string, workDir string, idleTTL time.Duration) *ProcessPool {
	pp := &ProcessPool{
		agentBinary: binary,
		agentArgs:   args,
		workDir:     workDir,
		idleTTL:     idleTTL,
		done:        make(chan struct{}),
	}
	go pp.reapLoop()
	return pp
}

// SetToolHandler sets the agent→client request handler (tool bridge).
// Must be called before any GetOrSpawn calls.
func (pp *ProcessPool) SetToolHandler(h RequestHandler) {
	pp.mu.Lock()
	defer pp.mu.Unlock()
	pp.toolHandler = h
}

// getToolHandler returns the current tool handler (thread-safe).
func (pp *ProcessPool) getToolHandler() RequestHandler {
	pp.mu.RLock()
	defer pp.mu.RUnlock()
	return pp.toolHandler
}

// GetOrSpawn returns an existing process for the session key or spawns a new one.
// Uses per-key mutex to prevent concurrent spawn for the same session.
func (pp *ProcessPool) GetOrSpawn(ctx context.Context, sessionKey string) (*ACPProcess, error) {
	// Acquire per-key spawn lock to prevent concurrent spawns
	actual, _ := pp.spawnMu.LoadOrStore(sessionKey, &sync.Mutex{})
	mu := actual.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()

	if val, ok := pp.processes.Load(sessionKey); ok {
		proc := val.(*ACPProcess)
		select {
		case <-proc.exited:
			// Process crashed — remove and respawn
			pp.processes.Delete(sessionKey)
			slog.Info("acp: respawning crashed process", "session_key", sessionKey)
		default:
			return proc, nil
		}
	}
	return pp.spawn(ctx, sessionKey)
}

// spawn creates a new ACP subprocess, initializes it, and creates a session.
func (pp *ProcessPool) spawn(ctx context.Context, sessionKey string) (*ACPProcess, error) {
	procCtx, cancel := context.WithCancel(context.Background())

	cmd := exec.CommandContext(procCtx, pp.agentBinary, pp.agentArgs...)
	cmd.Dir = pp.workDir
	cmd.Env = filterACPEnv(os.Environ())

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("acp: stdin pipe: %w", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("acp: stdout pipe: %w", err)
	}
	// Capture stderr for diagnostics (capped via limitedWriter)
	cmd.Stderr = &limitedWriter{max: 4096}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("acp: start %s: %w", pp.agentBinary, err)
	}

	proc := &ACPProcess{
		cmd:        cmd,
		lastActive: time.Now(),
		ctx:        procCtx,
		cancel:     cancel,
		exited:     make(chan struct{}),
	}

	// Notification handler: route session/update to active prompt callback
	notifyHandler := func(method string, params json.RawMessage) {
		if method == "session/update" {
			var update SessionUpdate
			if err := json.Unmarshal(params, &update); err != nil {
				slog.Warn("acp: failed to parse session/update", "error", err)
				return
			}
			proc.dispatchUpdate(update)
		}
	}

	proc.conn = NewConn(stdinPipe, stdoutPipe, pp.getToolHandler(), notifyHandler)
	proc.conn.Start()

	// Monitor process exit and log stderr
	stderrWriter := cmd.Stderr.(*limitedWriter)
	go func() {
		_ = cmd.Wait()
		if s := stderrWriter.String(); s != "" {
			slog.Debug("acp: process stderr", "session_key", sessionKey, "stderr", s)
		}
		close(proc.exited)
	}()

	// ACP handshake: initialize + session/new
	if err := proc.Initialize(ctx); err != nil {
		cancel()
		return nil, err
	}
	if err := proc.NewSession(ctx); err != nil {
		cancel()
		return nil, err
	}

	pp.processes.Store(sessionKey, proc)
	slog.Info("acp: process spawned", "session_key", sessionKey, "binary", pp.agentBinary)
	return proc, nil
}

// reapLoop periodically checks for idle processes and kills them.
func (pp *ProcessPool) reapLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			pp.processes.Range(func(key, value any) bool {
				proc := value.(*ACPProcess)
				// Skip processes with active prompts
				if proc.inUse.Load() > 0 {
					return true
				}
				proc.mu.Lock()
				idle := time.Since(proc.lastActive) > pp.idleTTL
				proc.mu.Unlock()
				if idle {
					slog.Info("acp: reaping idle process", "session_key", key)
					proc.cancel()
					pp.processes.Delete(key)
				}
				return true
			})
		case <-pp.done:
			return
		}
	}
}

// processCloseTimeout is the per-process max wait during ProcessPool.Close
// before we log a warning and move on. Production default = 5s. Exposed as a
// package var so tests can override to a few ms, avoiding 5s of dead CI time
// when exercising the "process never exits" path.
var processCloseTimeout = 5 * time.Second

// Close shuts down all processes gracefully.
func (pp *ProcessPool) Close() error {
	pp.closeOnce.Do(func() {
		close(pp.done)
		pp.processes.Range(func(key, value any) bool {
			proc := value.(*ACPProcess)
			proc.cancel()
			// Wait briefly for process to exit
			select {
			case <-proc.exited:
			case <-time.After(processCloseTimeout):
				slog.Warn("acp: process did not exit in time", "session_key", key)
			}
			pp.processes.Delete(key)
			return true
		})
	})
	return nil
}

