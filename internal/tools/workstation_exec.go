package tools

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/eventbus"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/workstation"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// PermCheckFn is the signature for workstation permission checks.
// Phase 6 wires the real implementation; Phase 5 ships with a deny-all sentinel.
// env is passed so the checker can also call CheckEnv to block forbidden env vars.
type PermCheckFn func(ctx context.Context, ws *store.Workstation, cmd string, args []string, env map[string]string) error

// denyAllSentinel is the default permCheck that blocks all exec until Phase 6 wires real checks.
var denyAllSentinel PermCheckFn = func(_ context.Context, _ *store.Workstation, _ string, _ []string, _ map[string]string) error {
	return errors.New("workstation permissions not configured; Phase 6 required")
}

const (
	execChunkSize   = 64 * 1024 // 64 KiB max chunk
	execTailSize    = 2 * 1024  // last 2 KiB of stdout/stderr
	execMaxCmdBytes = 4 * 1024
	execMaxArgBytes = 1024
	execMaxCWDBytes = 500
	execMaxEnvKey   = 256
	execMaxEnvVal   = 256
	execMaxEnvCount = 50
)

// WorkstationExecTool executes commands on a remote workstation backend.
// Streams stdout/stderr as eventbus chunks; returns exit code + tails in *Result.
// Registered Standard-edition only. Deny-all by default until Phase 6 wires permCheck.
type WorkstationExecTool struct {
	wsStore      store.WorkstationStore
	linkStore    store.AgentWorkstationLinkStore
	backendCache *workstation.BackendCache
	eventBus     eventbus.DomainEventBus
	permCheck    PermCheckFn
}

// NewWorkstationExecTool creates a WorkstationExecTool.
// permCheck defaults to deny-all sentinel — tools are non-functional until Phase 6 wires real checker.
func NewWorkstationExecTool(
	wsStore store.WorkstationStore,
	linkStore store.AgentWorkstationLinkStore,
	backendCache *workstation.BackendCache,
	eb eventbus.DomainEventBus,
) *WorkstationExecTool {
	return &WorkstationExecTool{
		wsStore:      wsStore,
		linkStore:    linkStore,
		backendCache: backendCache,
		eventBus:     eb,
		// M7 fix: deny-all by default — tool is registered but non-functional until
		// Phase 6 merges and calls SetPermCheck with a real implementation.
		permCheck: denyAllSentinel,
	}
}

// SetPermCheck replaces the default deny-all sentinel with a real permission checker.
// Called by Phase 6 during gateway wiring.
func (t *WorkstationExecTool) SetPermCheck(fn PermCheckFn) {
	t.permCheck = fn
}

func (t *WorkstationExecTool) Name() string { return "workstation_exec" }

func (t *WorkstationExecTool) Description() string {
	return "Execute a command on a remote user-owned workstation (SSH or Docker backend). " +
		"Streams stdout/stderr as events. Returns exit code and output tail."
}

func (t *WorkstationExecTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"workstation_id": map[string]any{
				"type":        "string",
				"description": "Workstation UUID or workstation_key (optional if agent has a default binding)",
			},
			"command": map[string]any{
				"type":        "string",
				"description": "Command to execute",
			},
			"args": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
			"cwd": map[string]any{
				"type":        "string",
				"description": "Working directory on the remote workstation",
			},
			"env": map[string]any{
				"type":                 "object",
				"additionalProperties": map[string]any{"type": "string"},
				"description":          "Extra environment variables to inject",
			},
			"timeout_sec": map[string]any{
				"type":    "integer",
				"default": 300,
			},
			"persistent": map[string]any{
				"type":        "boolean",
				"default":     false,
				"description": "Use persistent tmux session (Phase 4 deferred; currently unsupported)",
			},
		},
		"required": []string{"command"},
	}
}

// Execute resolves the target workstation, runs the command, streams chunks, returns result.
func (t *WorkstationExecTool) Execute(ctx context.Context, args map[string]any) *Result {
	locale := store.LocaleFromContext(ctx)
	agentUUID := store.AgentIDFromContext(ctx)
	agentID := agentUUID.String()

	// Validate command.
	cmd, _ := args["command"].(string)
	if cmd == "" {
		return ErrorResult(i18n.T(locale, i18n.MsgRequired, "command"))
	}
	if strings.ContainsRune(cmd, '\x00') {
		return ErrorResult("command contains invalid NUL byte")
	}
	if len(cmd) > execMaxCmdBytes {
		return ErrorResult(fmt.Sprintf("command exceeds %d byte limit", execMaxCmdBytes))
	}

	// Validate and coerce args.
	execArgs, err := coerceStringSlice(args["args"], execMaxArgBytes)
	if err != nil {
		return ErrorResult("args: " + err.Error())
	}

	// Validate cwd.
	cwd, _ := args["cwd"].(string)
	if len(cwd) > execMaxCWDBytes {
		return ErrorResult(fmt.Sprintf("cwd exceeds %d byte limit", execMaxCWDBytes))
	}

	// Validate env.
	envMap, err := coerceStringMap(args["env"], execMaxEnvKey, execMaxEnvVal, execMaxEnvCount)
	if err != nil {
		return ErrorResult("env: " + err.Error())
	}

	// Reject persistent=true until Phase 4 SessionManager is wired.
	if persistent, _ := args["persistent"].(bool); persistent {
		return ErrorResult("persistent sessions not yet supported (Phase 4 deferred)")
	}

	// 1. Resolve workstation.
	ws, err := t.resolveWorkstation(ctx, args, agentUUID)
	if err != nil {
		return ErrorResult(err.Error())
	}

	// 2. Permission check — deny-all by default until Phase 6.
	// env is passed so the checker can invoke CheckEnv for env var blocklist.
	if permErr := t.permCheck(ctx, ws, cmd, execArgs, envMap); permErr != nil {
		slog.Warn("security.workstation_exec_denied",
			"workstation_id", ws.ID,
			"agent_id", agentID,
			"cmd_hash", fmt.Sprintf("%x", sha256.Sum256([]byte(cmd)))[:12],
		)
		return ErrorResult(i18n.T(locale, i18n.MsgWorkstationAccessDenied, agentID, ws.WorkstationKey))
	}

	// 3. Get backend from cache.
	backend, err := t.backendCache.Get(ctx, ws.ID)
	if err != nil {
		return ErrorResult(i18n.T(locale, i18n.MsgBackendNotReady, err.Error()))
	}

	// 4. Build timeout context.
	timeoutSec, _ := args["timeout_sec"].(float64)
	if timeoutSec <= 0 {
		timeoutSec = 300
	}
	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	// 5. Open session and exec.
	sessionKey := ToolSessionKeyFromCtx(ctx)
	if sessionKey == "" {
		sessionKey = uuid.New().String()
	}
	sess, err := backend.OpenSession(execCtx, sessionKey)
	if err != nil {
		return ErrorResult(i18n.T(locale, i18n.MsgBackendNotReady, err.Error()))
	}
	defer func() { _ = sess.Close(context.Background()) }()

	// Build exec request with defaults from workstation.
	req := buildExecRequest(cmd, execArgs, cwd, envMap, ws, timeoutSec)

	slog.Info("workstation.exec.start",
		"workstation_id", ws.ID,
		"agent_id", agentID,
		"session_key", sessionKey,
	)

	stream, err := sess.Exec(execCtx, req)
	if err != nil {
		return ErrorResult(i18n.T(locale, i18n.MsgBackendNotReady, err.Error()))
	}

	// 6. Stream output and collect result.
	// I3 fix: pass full command string so activity sink can compute meaningful cmd_hash/preview.
	cmdFull := cmd
	if len(execArgs) > 0 {
		cmdFull = cmd + " " + strings.Join(execArgs, " ")
	}
	result := t.streamAndCollect(execCtx, stream, ws, agentID, sessionKey, cmdFull)

	slog.Info("workstation.exec.done",
		"workstation_id", ws.ID,
		"agent_id", agentID,
		"session_key", sessionKey,
		"exit_code", result.ForLLM,
	)
	return result
}

// resolveWorkstation resolves the target workstation from args or agent's default link.
// Applies tenant check on all resolution paths (C3 fix).
func (t *WorkstationExecTool) resolveWorkstation(ctx context.Context, args map[string]any, agentUUID uuid.UUID) (*store.Workstation, error) {
	locale := store.LocaleFromContext(ctx)
	tid := store.TenantIDFromContext(ctx)

	if raw, ok := args["workstation_id"].(string); ok && raw != "" {
		if id, parseErr := uuid.Parse(raw); parseErr == nil {
			ws, err := t.wsStore.GetByID(ctx, id)
			if err != nil {
				return nil, errors.New(i18n.T(locale, i18n.MsgWorkstationNotFound, raw))
			}
			// C3 fix: tenant check on explicit UUID path.
			if ws.TenantID != tid {
				return nil, errors.New(i18n.T(locale, i18n.MsgWorkstationAccessDenied, agentUUID.String(), raw))
			}
			return ws, nil
		}
		// Treat as workstation_key; store impl already filters by tenant via ctx.
		ws, err := t.wsStore.GetByKey(ctx, raw)
		if err != nil {
			return nil, errors.New(i18n.T(locale, i18n.MsgWorkstationNotFound, raw))
		}
		return ws, nil
	}

	// Fall back to agent's default binding.
	if agentUUID == uuid.Nil {
		return nil, errors.New(i18n.T(locale, i18n.MsgWorkstationRequired))
	}
	links, err := t.linkStore.ListForAgent(ctx, agentUUID)
	if err != nil || len(links) == 0 {
		return nil, errors.New(i18n.T(locale, i18n.MsgWorkstationRequired))
	}

	// Prefer the link marked as default; fall back to sole link if exactly one exists.
	var chosen *store.AgentWorkstationLink
	for i := range links {
		if links[i].IsDefault {
			chosen = &links[i]
			break
		}
	}
	if chosen == nil && len(links) == 1 {
		chosen = &links[0]
	}
	if chosen == nil {
		return nil, errors.New(i18n.T(locale, i18n.MsgWorkstationRequired))
	}

	ws, err := t.wsStore.GetByID(ctx, chosen.WorkstationID)
	if err != nil {
		return nil, errors.New(i18n.T(locale, i18n.MsgWorkstationNotFound, chosen.WorkstationID.String()))
	}
	// C3 fix: tenant check on default-link path prevents cross-tenant leak via stale cache / impersonation.
	if ws.TenantID != tid {
		slog.Warn("security.workstation_cross_tenant_default_link",
			"agent_id", agentUUID,
			"workstation_id", ws.ID,
			"expected_tenant", tid,
			"actual_tenant", ws.TenantID,
		)
		return nil, errors.New(i18n.T(locale, i18n.MsgWorkstationAccessDenied, agentUUID.String(), chosen.WorkstationID.String()))
	}
	return ws, nil
}

// streamAndCollect reads stdout/stderr from stream, emits eventbus chunks, and waits for exit.
// Returns *Result with exit code and last 2 KB of each stream.
// cmdFull is the full command string (cmd + args joined) embedded in the done event so
// the activity sink can compute a meaningful cmd_hash and cmd_preview.
func (t *WorkstationExecTool) streamAndCollect(
	ctx context.Context,
	stream workstation.Stream,
	ws *store.Workstation,
	agentID, sessionKey string,
	cmdFull string,
) *Result {
	var (
		stdoutTail tailBuffer
		stderrTail tailBuffer
		seq        atomic.Int64
		wg         sync.WaitGroup
	)

	startTime := time.Now()

	emitChunk := func(kind, data string) {
		s := seq.Add(1)
		if t.eventBus != nil {
			t.eventBus.Publish(eventbus.DomainEvent{
				ID:       uuid.New().String(),
				Type:     eventbus.EventType(protocol.EventWorkstationExecChunk),
				SourceID: sessionKey,
				TenantID: ws.TenantID.String(),
				AgentID:  agentID,
				Payload: map[string]any{
					"workstation_id": ws.ID.String(),
					"agent_id":       agentID,
					"session_key":    sessionKey,
					"stream":         kind,
					"seq":            s,
					"data":           data,
				},
			})
		}
	}

	readStream := func(r io.Reader, kind string, tail *tailBuffer) {
		defer wg.Done()
		buf := make([]byte, execChunkSize)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				chunk := string(buf[:n])
				tail.Write(buf[:n])
				emitChunk(kind, chunk)
			}
			if err != nil {
				break
			}
			// Respect context cancellation.
			select {
			case <-ctx.Done():
				return
			default:
			}
		}
	}

	wg.Add(2)
	go readStream(stream.Stdout(), "stdout", &stdoutTail)
	go readStream(stream.Stderr(), "stderr", &stderrTail)
	wg.Wait()

	exitCode, waitErr := stream.Wait()
	durationMs := time.Since(startTime).Milliseconds()

	// Emit done event.
	if t.eventBus != nil {
		t.eventBus.Publish(eventbus.DomainEvent{
			ID:       uuid.New().String(),
			Type:     eventbus.EventType(protocol.EventWorkstationExecDone),
			SourceID: sessionKey,
			TenantID: ws.TenantID.String(),
			AgentID:  agentID,
			Payload: map[string]any{
				"workstation_id": ws.ID.String(),
				"agent_id":       agentID,
				"session_key":    sessionKey,
				"exit_code":      exitCode,
				"duration_ms":    durationMs,
				"stdout_tail":    stdoutTail.String(),
				"stderr_tail":    stderrTail.String(),
				// I3 fix: include command for meaningful cmd_hash/cmd_preview in activity sink.
				"command": cmdFull,
			},
		})
	}

	if waitErr != nil && exitCode == 0 {
		exitCode = 1
	}

	out := fmt.Sprintf("exit_code: %d\nstdout:\n%s\nstderr:\n%s",
		exitCode, stdoutTail.String(), stderrTail.String())
	if exitCode != 0 {
		return ErrorResult(out)
	}
	return SilentResult(out)
}

// buildExecRequest builds a workstation.ExecRequest from validated inputs.
// Merges workstation DefaultCWD + DefaultEnv, then overlays call-time values.
func buildExecRequest(
	cmd string,
	args []string,
	cwd string,
	env map[string]string,
	ws *store.Workstation,
	timeoutSec float64,
) workstation.ExecRequest {
	// Base env from workstation defaults.
	merged := make(map[string]string)
	if len(ws.DefaultEnv) > 0 {
		// DefaultEnv is stored as a JSON map of env overrides (plaintext after decrypt).
		var defaults map[string]string
		if err := json.Unmarshal(ws.DefaultEnv, &defaults); err == nil {
			maps.Copy(merged, defaults)
		}
	}
	// Call-time env overrides defaults.
	maps.Copy(merged, env)

	// Default CWD from workstation if not specified.
	if cwd == "" {
		cwd = ws.DefaultCWD
	}

	return workstation.ExecRequest{
		Cmd:     cmd,
		Args:    args,
		Env:     merged,
		CWD:     cwd,
		Timeout: time.Duration(timeoutSec) * time.Second,
	}
}

// tailBuffer keeps the last N bytes written to it (ring-buffer semantics).
type tailBuffer struct {
	mu   sync.Mutex
	data []byte
}

func (tb *tailBuffer) Write(p []byte) {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.data = append(tb.data, p...)
	if len(tb.data) > execTailSize {
		tb.data = tb.data[len(tb.data)-execTailSize:]
	}
}

func (tb *tailBuffer) String() string {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	return string(tb.data)
}

// coerceStringSlice converts an interface{} (expected []any from JSON decode) to []string.
// Returns an error if any element exceeds maxBytes or contains a NUL byte.
func coerceStringSlice(raw any, maxBytes int) ([]string, error) {
	if raw == nil {
		return nil, nil
	}
	switch v := raw.(type) {
	case []string:
		for _, s := range v {
			if err := validateExecString(s, maxBytes); err != nil {
				return nil, err
			}
		}
		return v, nil
	case []any:
		out := make([]string, 0, len(v))
		for _, elem := range v {
			s, ok := elem.(string)
			if !ok {
				return nil, fmt.Errorf("each arg must be a string")
			}
			if err := validateExecString(s, maxBytes); err != nil {
				return nil, err
			}
			out = append(out, s)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("args must be an array of strings")
	}
}

// coerceStringMap converts an interface{} (expected map[string]any from JSON decode) to map[string]string.
func coerceStringMap(raw any, maxKey, maxVal, maxCount int) (map[string]string, error) {
	if raw == nil {
		return nil, nil
	}
	switch v := raw.(type) {
	case map[string]string:
		if len(v) > maxCount {
			return nil, fmt.Errorf("env exceeds %d entry limit", maxCount)
		}
		for k, val := range v {
			if len(k) > maxKey {
				return nil, fmt.Errorf("env key exceeds %d byte limit", maxKey)
			}
			if len(val) > maxVal {
				return nil, fmt.Errorf("env value for %q exceeds %d byte limit", k, maxVal)
			}
		}
		return v, nil
	case map[string]any:
		if len(v) > maxCount {
			return nil, fmt.Errorf("env exceeds %d entry limit", maxCount)
		}
		out := make(map[string]string, len(v))
		for k, val := range v {
			if len(k) > maxKey {
				return nil, fmt.Errorf("env key exceeds %d byte limit", maxKey)
			}
			s, ok := val.(string)
			if !ok {
				return nil, fmt.Errorf("env value for %q must be a string", k)
			}
			if len(s) > maxVal {
				return nil, fmt.Errorf("env value for %q exceeds %d byte limit", k, maxVal)
			}
			out[k] = s
		}
		return out, nil
	default:
		return nil, fmt.Errorf("env must be an object with string values")
	}
}

// validateExecString checks length and NUL byte.
func validateExecString(s string, maxBytes int) error {
	if strings.ContainsRune(s, '\x00') {
		return fmt.Errorf("string contains invalid NUL byte")
	}
	if len(s) > maxBytes {
		return fmt.Errorf("string exceeds %d byte limit", maxBytes)
	}
	return nil
}
