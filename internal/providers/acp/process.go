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
// One process is shared across all sessions — each goclaw conversation
// creates its own ACP session (identified by session ID) on this process.
type ACPProcess struct {
	cmd  *exec.Cmd
	conn *Conn

	agentCaps    AgentCaps
	workDir      string
	mcpServersFn func(context.Context) []McpServer // invoked on every session/new + session/load
	lastActive   time.Time
	inUse      atomic.Int32 // >0 means at least one prompt is active — reaper must skip
	mu         sync.Mutex
	ctx        context.Context
	cancel     context.CancelFunc
	exited     chan struct{} // closed when process exits

	// updateFns routes session/update notifications to the correct active prompt.
	updateFns map[string]func(SessionUpdate)
	updateMu  sync.Mutex
}

// AgentCaps returns the capability flags reported by the agent during Initialize.
func (p *ACPProcess) AgentCaps() AgentCaps {
	return p.agentCaps
}

// WorkDir returns the process pool's base work directory. Callers building
// per-session workspaces should join a session-specific segment under this
// path and pass the result as the cwd argument to NewSession/LoadSession.
func (p *ACPProcess) WorkDir() string { return p.workDir }

// registerUpdateFn registers a callback for session/update notifications on sessionID.
func (p *ACPProcess) registerUpdateFn(sid string, fn func(SessionUpdate)) {
	p.updateMu.Lock()
	defer p.updateMu.Unlock()
	if p.updateFns == nil {
		p.updateFns = make(map[string]func(SessionUpdate))
	}
	p.updateFns[sid] = fn
}

// unregisterUpdateFn removes the callback for sessionID after a Prompt completes.
func (p *ACPProcess) unregisterUpdateFn(sid string) {
	p.updateMu.Lock()
	defer p.updateMu.Unlock()
	delete(p.updateFns, sid)
}

// dispatchUpdate routes a session/update notification to the registered callback.
// It also performs Gemini ACP protocol mapping: the "agent_message_chunk" update type
// carries content in Update.Content rather than the standard Message field; this is
// normalized here so all callers receive a consistent SessionUpdate.
func (p *ACPProcess) dispatchUpdate(update SessionUpdate) {
	// Gemini protocol mapping: agent_message_chunk → Message
	if update.Update.SessionUpdate == "agent_message_chunk" && len(update.Update.Content) > 0 {
		if update.Message == nil {
			update.Message = &MessageUpdate{Role: "assistant"}
		}
		// Content may arrive as a single object {"type":"text","text":"..."} or an array
		var single struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(update.Update.Content, &single); err == nil && single.Type != "" {
			update.Message.Content = append(update.Message.Content, ContentBlock{
				Type: single.Type,
				Text: single.Text,
			})
		} else {
			var arr []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			if err := json.Unmarshal(update.Update.Content, &arr); err == nil {
				for _, c := range arr {
					update.Message.Content = append(update.Message.Content, ContentBlock{
						Type: c.Type,
						Text: c.Text,
					})
				}
			}
		}
	}

	p.updateMu.Lock()
	fn, ok := p.updateFns[update.SessionID]
	p.updateMu.Unlock()
	if !ok {
		slog.Debug("acp: session/update with no registered callback", "sid", update.SessionID)
		return
	}
	if fn != nil {
		fn(update)
	}
}

// ProcessPool manages a pool of ACP agent subprocesses.
// Typically a single shared process is used (poolKey = binary identifier),
// and multiple ACP sessions are multiplexed over it.
type ProcessPool struct {
	processes   sync.Map // poolKey → *ACPProcess
	spawnMu     sync.Map // poolKey → *sync.Mutex — prevents concurrent spawn
	agentBinary  string
	agentArgs    []string
	workDir      string
	mcpServersFn func(context.Context) []McpServer // resolved per session/new + session/load
	idleTTL      time.Duration
	mu          sync.RWMutex // protects toolHandler
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

// SetMcpServersFunc configures the callback used to build the MCP server list
// on every session/new and session/load request. The callback receives the
// request context (with agent/tenant IDs) so it can return per-agent servers
// resolved from the MCP store. Must be called before GetOrSpawn; spawned
// processes inherit the current value at spawn time.
func (pp *ProcessPool) SetMcpServersFunc(fn func(context.Context) []McpServer) {
	pp.mu.Lock()
	defer pp.mu.Unlock()
	pp.mcpServersFn = fn
}

func (pp *ProcessPool) getMcpServersFn() func(context.Context) []McpServer {
	pp.mu.RLock()
	defer pp.mu.RUnlock()
	return pp.mcpServersFn
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

// GetOrSpawn returns an existing process for the pool key or spawns a new one.
// Uses per-key mutex to prevent concurrent spawn for the same key.
func (pp *ProcessPool) GetOrSpawn(ctx context.Context, poolKey string) (*ACPProcess, error) {
	actual, _ := pp.spawnMu.LoadOrStore(poolKey, &sync.Mutex{})
	mu := actual.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()

	if val, ok := pp.processes.Load(poolKey); ok {
		proc := val.(*ACPProcess)
		select {
		case <-proc.exited:
			pp.processes.Delete(poolKey)
			slog.Info("acp: respawning crashed process", "pool_key", poolKey)
		default:
			return proc, nil
		}
	}
	return pp.spawn(ctx, poolKey)
}

// spawn creates a new ACP subprocess and performs the ACP initialize handshake.
// Session creation (session/new) is NOT done here — the provider handles that
// per-conversation via NewSession or LoadSession.
func (pp *ProcessPool) spawn(ctx context.Context, poolKey string) (*ACPProcess, error) {
	procCtx, cancel := context.WithCancel(context.Background())

	cmd := exec.CommandContext(procCtx, pp.agentBinary, pp.agentArgs...)
	cmd.Dir = pp.workDir
	cmd.Env = append(filterACPEnv(os.Environ()),
		"GEMINI_TELEMETRY_ENABLED=false",
	)
	cmd.SysProcAttr = sysProcAttr()

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
	cmd.Stderr = &limitedWriter{max: 4096}

	slog.Info("acp: starting subprocess", "pool_key", poolKey, "binary", pp.agentBinary, "args", pp.agentArgs)
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("acp: start %s: %w", pp.agentBinary, err)
	}

	proc := &ACPProcess{
		cmd:          cmd,
		lastActive:   time.Now(),
		ctx:          procCtx,
		cancel:       cancel,
		exited:       make(chan struct{}),
		workDir:      pp.workDir,
		mcpServersFn: pp.getMcpServersFn(),
	}

	// Notification handler: log all notifications and dispatch session/update to callers
	notifyHandler := func(method string, params json.RawMessage) {
		slog.Info("acp: notification received", "method", method)
		slog.Debug("acp: notification params", "method", method, "params", string(params))
		if method == "session/update" {
			var update SessionUpdate
			if err := json.Unmarshal(params, &update); err != nil {
				slog.Warn("acp: session/update parse failed", "error", err)
				return
			}
			proc.dispatchUpdate(update)
		}
	}

	proc.conn = NewConn(stdinPipe, stdoutPipe, pp.getToolHandler(), notifyHandler)
	proc.conn.Start()

	stderrWriter := cmd.Stderr.(*limitedWriter)
	go func() {
		_ = cmd.Wait()
		if s := stderrWriter.String(); s != "" {
			slog.Debug("acp: process stderr", "pool_key", poolKey, "stderr", s)
		}
		close(proc.exited)
	}()

	slog.Info("acp: performing handshake (initialize)", "pool_key", poolKey)
	if err := proc.Initialize(ctx); err != nil {
		cancel()
		return nil, err
	}

	pp.processes.Store(poolKey, proc)
	slog.Info("acp: process spawned", "pool_key", poolKey, "binary", pp.agentBinary)
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
				if proc.inUse.Load() > 0 {
					return true
				}
				proc.mu.Lock()
				idle := time.Since(proc.lastActive) > pp.idleTTL
				proc.mu.Unlock()
				if idle {
					slog.Info("acp: reaping idle process", "pool_key", key)
					pp.processes.Delete(key) // delete before cancel so a concurrent GetOrSpawn sees no stale entry
					proc.cancel()
				}
				return true
			})
		case <-pp.done:
			return
		}
	}
}

// processCloseTimeout is the per-process max wait during ProcessPool.Close.
// Exposed as a package var so tests can shorten it.
var processCloseTimeout = 5 * time.Second

// Close shuts down all processes gracefully.
func (pp *ProcessPool) Close() error {
	pp.closeOnce.Do(func() {
		close(pp.done)
		pp.processes.Range(func(key, value any) bool {
			proc := value.(*ACPProcess)
			proc.cancel()
			select {
			case <-proc.exited:
			case <-time.After(processCloseTimeout):
				slog.Warn("acp: process did not exit in time", "pool_key", key)
			}
			pp.processes.Delete(key)
			return true
		})
	})
	return nil
}
