package acp

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Terminal represents a running command spawned by an ACP agent.
type Terminal struct {
	id       string
	cmd      *exec.Cmd
	output   *cappedBuffer
	mu       sync.Mutex
	exited   chan struct{}
	exitCode int
	cancel   context.CancelFunc
}

// cappedBuffer is a thread-safe buffer that caps output at a maximum size.
type cappedBuffer struct {
	buf bytes.Buffer
	mu  sync.Mutex
	max int
}

func (cb *cappedBuffer) Write(p []byte) (int, error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	// If writing would exceed cap, truncate by keeping only the tail
	if cb.buf.Len()+len(p) > cb.max {
		overflow := cb.buf.Len() + len(p) - cb.max
		if overflow >= cb.buf.Len() {
			cb.buf.Reset()
			// Only write the last max bytes of p
			if len(p) > cb.max {
				p = p[len(p)-cb.max:]
			}
		} else {
			data := cb.buf.Bytes()[overflow:]
			cb.buf.Reset()
			cb.buf.Write(data)
		}
	}
	return cb.buf.Write(p)
}

func (cb *cappedBuffer) String() string {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.buf.String()
}

// allowedTerminalBinaries lists binaries that ACP agents may execute.
// All commands run via exec.CommandContext (no shell interpretation),
// so this restricts which programs can be spawned as subprocesses.
var allowedTerminalBinaries = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "fish": true,
	"node": true, "python": true, "python3": true, "ruby": true, "perl": true,
	"go": true, "cargo": true, "rustc": true, "gcc": true, "g++": true, "make": true,
	"git": true, "ls": true, "cat": true, "head": true, "tail": true,
	"grep": true, "rg": true, "find": true, "wc": true, "sort": true, "uniq": true,
	"diff": true, "patch": true, "mkdir": true, "cp": true, "mv": true, "touch": true,
	"echo": true, "printf": true, "env": true, "which": true, "whoami": true,
	"npm": true, "npx": true, "pnpm": true, "yarn": true, "bun": true,
	"pip": true, "pip3": true, "uv": true, "pipx": true,
	"docker": true, "kubectl": true,
	"curl": true, "wget": true, "jq": true, "yq": true,
	"tar": true, "gzip": true, "unzip": true,
	"sed": true, "awk": true, "xargs": true, "tee": true, "tr": true, "cut": true,
	"test": true, "true": true, "false": true,
}

// createTerminal spawns a command and tracks it in the terminal registry.
func (tb *ToolBridge) createTerminal(req CreateTerminalRequest) (*CreateTerminalResponse, error) {
	// Validate binary against allowlist — extract base name for PATH-based commands
	binaryBase := filepath.Base(req.Command)
	if !allowedTerminalBinaries[binaryBase] {
		slog.Warn("security.acp_terminal_denied", "command", req.Command, "binary", binaryBase)
		return nil, fmt.Errorf("command %q not in allowed binary list", binaryBase)
	}

	// Validate command + args against deny patterns
	fullCmd := req.Command
	if len(req.Args) > 0 {
		fullCmd += " " + strings.Join(req.Args, " ")
	}
	for _, pat := range tb.denyPatterns {
		if pat.MatchString(fullCmd) {
			slog.Warn("security.acp_terminal_deny_pattern", "command", fullCmd)
			return nil, fmt.Errorf("command denied by safety policy")
		}
	}

	// Resolve cwd (default to workspace)
	cwd := tb.workspace
	if req.Cwd != "" {
		resolved, err := tb.resolvePath(req.Cwd)
		if err != nil {
			return nil, err
		}
		cwd = resolved
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, req.Command, req.Args...)
	cmd.Dir = cwd

	output := &cappedBuffer{max: tb.maxOutputBytes}
	cmd.Stdout = output
	cmd.Stderr = output

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start failed: %w", err)
	}

	termID := fmt.Sprintf("term-%d", tb.nextTermID.Add(1))
	term := &Terminal{
		id:     termID,
		cmd:    cmd,
		output: output,
		exited: make(chan struct{}),
		cancel: cancel,
	}

	// Wait for process exit in background
	go func() {
		err := cmd.Wait()
		term.mu.Lock()
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				term.exitCode = exitErr.ExitCode()
			} else {
				term.exitCode = -1
			}
		}
		term.mu.Unlock()
		close(term.exited)
	}()

	tb.terminals.Store(termID, term)
	return &CreateTerminalResponse{TerminalID: termID}, nil
}

// terminalOutput returns the current output and exit status if exited.
func (tb *ToolBridge) terminalOutput(req TerminalOutputRequest) (*TerminalOutputResponse, error) {
	val, ok := tb.terminals.Load(req.TerminalID)
	if !ok {
		return nil, fmt.Errorf("terminal not found: %s", req.TerminalID)
	}
	t := val.(*Terminal)
	resp := &TerminalOutputResponse{Output: t.output.String()}
	select {
	case <-t.exited:
		t.mu.Lock()
		code := t.exitCode
		t.mu.Unlock()
		resp.ExitStatus = &code
	default:
	}
	return resp, nil
}

// releaseTerminal kills (if running) and removes a terminal.
func (tb *ToolBridge) releaseTerminal(req ReleaseTerminalRequest) (*ReleaseTerminalResponse, error) {
	val, ok := tb.terminals.LoadAndDelete(req.TerminalID)
	if !ok {
		return &ReleaseTerminalResponse{}, nil
	}
	t := val.(*Terminal)
	t.cancel()
	return &ReleaseTerminalResponse{}, nil
}

// waitForExit blocks until the terminal command exits, with a 10-minute timeout.
// Respects context cancellation so callers are not blocked indefinitely.
func (tb *ToolBridge) waitForExit(ctx context.Context, req WaitForTerminalExitRequest) (*WaitForTerminalExitResponse, error) {
	val, ok := tb.terminals.Load(req.TerminalID)
	if !ok {
		return nil, fmt.Errorf("terminal not found: %s", req.TerminalID)
	}
	t := val.(*Terminal)
	select {
	case <-t.exited:
		t.mu.Lock()
		code := t.exitCode
		t.mu.Unlock()
		return &WaitForTerminalExitResponse{ExitStatus: code}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(10 * time.Minute):
		return nil, fmt.Errorf("terminal %s: wait timed out after 10m", req.TerminalID)
	}
}

// killTerminal sends a kill signal without removing the terminal.
func (tb *ToolBridge) killTerminal(req KillTerminalRequest) (*KillTerminalResponse, error) {
	val, ok := tb.terminals.Load(req.TerminalID)
	if !ok {
		return nil, fmt.Errorf("terminal not found: %s", req.TerminalID)
	}
	t := val.(*Terminal)
	t.cancel()
	return &KillTerminalResponse{}, nil
}
