package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	orchestration "github.com/nextlevelbuilder/goclaw/internal/childrun"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/eventbus"
	"github.com/nextlevelbuilder/goclaw/internal/hooks"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
)

const (
	delegationArtifactFailureTTL    = 60 * time.Minute
	delegationArtifactSweepInterval = time.Minute
	delegationArtifactSweepBatch    = 32
)

// DelegateResult carries the delegatee's response content and any media produced.
type DelegateResult struct {
	Content string
	Media   []bus.MediaFile
	TraceID uuid.UUID
}

// DelegateRunFunc dispatches a delegation to a target agent.
// Injected by the gateway to avoid circular dependency with agent package.
// Returns the delegatee's response content + media, or error.
type DelegateRunFunc func(ctx context.Context, req DelegateRequest) (DelegateResult, error)

// DelegateRequest describes a delegation dispatch.
type DelegateRequest struct {
	FromAgentID         uuid.UUID
	FromAgentKey        string
	ToAgentKey          string
	Task                string
	DelegateInputsPath  string
	DelegateOutputsPath string
	DelegationID        string
	UserID              string
	SenderID            string // real acting sender preserved through delegate announce re-ingress (#915)
	Role                string // caller's RBAC role; bypasses per-user grants for admin/operator/owner (#915)
	TenantID            string
	Channel             string
	ChannelType         string
	ChatID              string
	PeerKind            string
	SessionKey          string
	OriginTraceID       uuid.UUID
	OriginRootSpanID    uuid.UUID
	OnTraceCreated      func(uuid.UUID)
}

// DelegateTool implements the `delegate` tool for inter-agent task delegation.
// Uses existing agent_links infrastructure for permission checks.
type DelegateTool struct {
	links          store.AgentLinkStore
	agents         store.AgentCRUDStore
	eventBus       eventbus.DomainEventBus
	runFn          DelegateRunFunc
	msgBus         *bus.MessageBus         // for async announce back to parent
	taskStore      store.SubagentTaskStore // durable async completion ledger
	hookDispatcher hooks.Dispatcher        // optional; nil-safe
	postTurn       PostTurnProcessor       // optional; dispatches team tasks a detached delegatee creates
	admission      *orchestration.ChildRunAdmission
	workspace      string
	dataDir        string
	removeExchange func(string, uuid.UUID) error

	retainedMu     sync.Mutex
	retained       map[string]retainedDelegationArtifact
	sweeperStarted bool
	sweeperClosed  bool
	sweeperStop    chan struct{}
	sweeperDone    chan struct{}

	activeArtifactMu sync.RWMutex
	activeArtifacts  map[string]uint32

	completionMu     sync.Mutex
	completionClosed bool
	completionWG     sync.WaitGroup
	closeOnce        sync.Once
	closeDone        chan struct{}
}

// SetMsgBus sets the message bus for async result delivery to parent agent.
func (t *DelegateTool) SetMsgBus(mb *bus.MessageBus) { t.msgBus = mb }

// SetTaskStore configures the durable ledger used by async delegations.
func (t *DelegateTool) SetTaskStore(s store.SubagentTaskStore) { t.taskStore = s }

// SetPostTurnProcessor wires post-turn team-task dispatch for async delegations.
func (t *DelegateTool) SetPostTurnProcessor(p PostTurnProcessor) { t.postTurn = p }

// SetHookDispatcher sets the hook dispatcher for SubagentStart/Stop events.
func (t *DelegateTool) SetHookDispatcher(d hooks.Dispatcher) { t.hookDispatcher = d }

// SetWorkspace sets the global managed workspace used to derive canonical
// tenant-scoped delegation exchanges. The caller's effective workspace is
// captured separately from the tool context for every dispatch.
func (t *DelegateTool) SetWorkspace(workspace string) {
	t.workspace = workspace
	t.recoverRetainedDelegationExchanges()
}

// SetDataDir supplies the second managed root used by Team workspaces. It is
// needed only to retry publication-temp cleanup after restart.
func (t *DelegateTool) SetDataDir(dataDir string) { t.dataDir = dataDir }

// Close stops the failed-exchange retention sweeper and drains accepted async
// completion persistence/announcement work.
func (t *DelegateTool) Close() {
	_ = t.CloseContext(context.Background())
}

// CloseContext prevents new async completion ownership and waits for accepted
// completion work. Gateway callers close child-run admission before this drain.
func (t *DelegateTool) CloseContext(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	t.closeOnce.Do(func() {
		t.completionMu.Lock()
		t.completionClosed = true
		t.completionMu.Unlock()

		t.retainedMu.Lock()
		t.sweeperClosed = true
		sweeperStarted := t.sweeperStarted
		if sweeperStarted {
			close(t.sweeperStop)
		}
		t.retainedMu.Unlock()

		go func() {
			if sweeperStarted {
				<-t.sweeperDone
			}
			t.completionWG.Wait()
			close(t.closeDone)
		}()
	})
	select {
	case <-t.closeDone:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("delegate completion drain timeout: %w", ctx.Err())
	}
}

func (t *DelegateTool) beginAsyncCompletion() (func(), bool) {
	t.completionMu.Lock()
	defer t.completionMu.Unlock()
	if t.completionClosed {
		return nil, false
	}
	t.completionWG.Add(1)
	var once sync.Once
	return func() {
		once.Do(t.completionWG.Done)
	}, true
}

// NewDelegateTool creates a delegate tool.
func NewDelegateTool(links store.AgentLinkStore, agents store.AgentCRUDStore, eb eventbus.DomainEventBus, runFn DelegateRunFunc) *DelegateTool {
	return NewDelegateToolWithAdmission(
		links,
		agents,
		eb,
		runFn,
		orchestration.NewChildRunAdmission(32, 128),
	)
}

func NewDelegateToolWithAdmission(
	links store.AgentLinkStore,
	agents store.AgentCRUDStore,
	eb eventbus.DomainEventBus,
	runFn DelegateRunFunc,
	admission *orchestration.ChildRunAdmission,
) *DelegateTool {
	if admission == nil {
		admission = orchestration.NewChildRunAdmission(32, 128)
	}
	return &DelegateTool{
		links:           links,
		agents:          agents,
		eventBus:        eb,
		runFn:           runFn,
		admission:       admission,
		retained:        make(map[string]retainedDelegationArtifact),
		sweeperStop:     make(chan struct{}),
		sweeperDone:     make(chan struct{}),
		activeArtifacts: make(map[string]uint32),
		closeDone:       make(chan struct{}),
	}
}

func (t *DelegateTool) Name() string { return "delegate" }

func (t *DelegateTool) Description() string {
	return "Delegate a task to a linked agent. The target agent must be connected via an agent link."
}

func (t *DelegateTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"agent_key": map[string]any{
				"type":        "string",
				"description": "The agent_key of the target agent to delegate to",
			},
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"delegate", "get"},
				"description": "delegate (default) starts work; get retrieves a durable async result",
			},
			"delegation_id": map[string]any{
				"type":        "string",
				"description": "Delegation UUID returned by async mode (required for action=get)",
			},
			"task": map[string]any{
				"type":        "string",
				"description": "Description of the task to delegate",
			},
			"mode": map[string]any{
				"type":        "string",
				"enum":        []string{"async", "sync"},
				"description": "async: fire-and-forget (default), sync: wait for completion",
			},
			"timeout": map[string]any{
				"type":        "integer",
				"description": "Timeout in seconds for sync mode (default: 300)",
			},
			"inputs": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"maxItems":    DelegationArtifactMaxFiles,
				"description": "Caller-workspace relative files to stage as read-only delegation inputs",
			},
		},
	}
}

func (t *DelegateTool) Execute(ctx context.Context, args map[string]any) *Result {
	action, _ := args["action"].(string)
	if action == "" {
		action = "delegate"
	}
	if action == "get" {
		return t.executeGetCompletion(ctx, args)
	}
	if action != "delegate" {
		return ErrorResult(fmt.Sprintf("unknown delegate action %q", action))
	}
	agentKey, _ := args["agent_key"].(string)
	task, _ := args["task"].(string)
	mode, _ := args["mode"].(string)
	if mode == "" {
		mode = "async"
	}
	timeoutSec := 300
	if ts, ok := args["timeout"].(float64); ok && int(ts) > 0 {
		timeoutSec = int(ts)
	}
	if timeoutSec > 600 {
		timeoutSec = 600 // hard cap to prevent resource exhaustion
	}

	if agentKey == "" || task == "" {
		return ErrorResult("agent_key and task are required")
	}
	if err := validateDelegationChildRunMode(ctx, "delegate", mode); err != nil {
		return ErrorResult(err.Error())
	}
	explicitInputs, err := parseDelegateInputs(args["inputs"])
	if err != nil {
		return ErrorResult(err.Error())
	}

	// Resolve calling agent from context
	fromAgentID := store.AgentIDFromContext(ctx)
	if fromAgentID == uuid.Nil {
		return ErrorResult("delegate requires agent context")
	}

	// Resolve target agent
	target, err := t.agents.GetByKey(ctx, agentKey)
	if err != nil {
		return ErrorResult(fmt.Sprintf("target agent %q not found", agentKey))
	}

	// Resolve the effective directional link for permission only.
	// max_concurrent is retained as compatibility metadata and is not enforced.
	link, err := t.links.GetLinkBetween(ctx, fromAgentID, target.ID)
	if err != nil {
		slog.Warn("delegate.permission_check_error", "from", fromAgentID, "to", target.ID, "error", err)
		return ErrorResult("failed to check delegation permission")
	}
	if link == nil {
		return ErrorResult(fmt.Sprintf("no delegation link from current agent to %q", agentKey))
	}
	_ = link

	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil || t.workspace == "" {
		return ErrorResult("delegation artifact workspace is unavailable")
	}
	callerWorkspace := ToolWorkspaceFromCtx(ctx)
	if callerWorkspace == "" {
		return ErrorResult("delegation caller workspace is unavailable")
	}
	callerWorkspace, err = filepath.Abs(callerWorkspace)
	if err != nil {
		return ErrorResult("delegation caller workspace is unavailable")
	}
	callerRoot, err := OpenDelegationArtifactRoot(callerWorkspace)
	if err != nil {
		return ErrorResult(err.Error())
	}
	inputs, err := collectDelegateInputs(callerWorkspace, explicitInputs, RunMediaPathsFromCtx(ctx))
	if err != nil {
		_ = callerRoot.Close()
		return ErrorResult(err.Error())
	}

	delegationUUID := uuid.New()
	delegationID := delegationUUID.String()

	req := DelegateRequest{
		FromAgentID:  fromAgentID,
		FromAgentKey: store.AgentKeyFromContext(ctx),
		ToAgentKey:   agentKey,
		Task:         task,
		DelegationID: delegationID,
		// Preserve the authorization scope separately from the acting sender.
		// Group file permissions require both the group principal (UserID) and
		// the real individual (SenderID), matching teammate task dispatch.
		UserID:           store.UserIDFromContext(ctx),
		SenderID:         store.SenderIDFromContext(ctx),
		Role:             store.RoleFromContext(ctx),
		TenantID:         tenantID.String(),
		Channel:          ToolChannelFromCtx(ctx),
		ChannelType:      ToolChannelTypeFromCtx(ctx),
		ChatID:           ToolChatIDFromCtx(ctx),
		PeerKind:         ToolPeerKindFromCtx(ctx),
		SessionKey:       ToolSessionKeyFromCtx(ctx),
		OriginTraceID:    tracing.TraceIDFromContext(ctx),
		OriginRootSpanID: tracing.ParentSpanIDFromContext(ctx),
	}
	job := &delegateArtifactJob{
		req:             req,
		mode:            mode,
		callerRoot:      callerRoot,
		callerWorkspace: callerWorkspace,
		tenantWorkspace: config.TenantWorkspace(t.workspace, tenantID, store.TenantSlugFromContext(ctx)),
		tenantID:        tenantID,
		tenantSlug:      store.TenantSlugFromContext(ctx),
		delegationID:    delegationUUID,
		inputs:          inputs,
	}
	job.callerLocation = t.resolveDelegationCallerLocation(job)

	if mode == "sync" {
		return t.executeSyncMode(ctx, job, timeoutSec)
	}
	return t.executeAsyncMode(ctx, job)
}

// executeSyncMode blocks until the delegatee completes or timeout.
func (t *DelegateTool) executeSyncMode(ctx context.Context, job *delegateArtifactJob, timeoutSec int) *Result {
	req := job.req
	syncCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	var dr DelegateResult
	var runErr error
	constraints := delegateAdmissionConstraints(ctx, req)
	run := func(runCtx context.Context, lease *orchestration.ChildRunLease) {
		defer job.closeCallerRoot()
		runCtx = withDelegatedAgentExecution(runCtx, lease)
		dr, runErr = t.runArtifactExchange(runCtx, job)
		lease.Release()
	}
	if parentLease := childRunLeaseFromContext(ctx); parentLease != nil {
		continueErr := parentLease.Continue(syncCtx, constraints, run)
		if continueErr != nil {
			job.closeCallerRoot()
			runErr = continueErr
		}
	} else {
		ticket, err := t.admission.Enqueue(syncCtx, constraints, run)
		if err != nil {
			job.closeCallerRoot()
			return ErrorResult(err.Error())
		}
		if err := ticket.Activate(); err != nil {
			ticket.Cancel()
			job.closeCallerRoot()
			return ErrorResult(err.Error())
		}
		<-ticket.Done()
		job.closeCallerRoot()
		if runErr == nil {
			runErr = ticket.Err()
		}
	}
	if runErr != nil {
		t.emitEvent(ctx, eventbus.EventDelegateFailed, eventbus.DelegateFailedPayload{
			DelegationID: req.DelegationID,
			FromAgent:    req.FromAgentKey,
			ToAgent:      req.ToAgentKey,
			Error:        runErr.Error(),
		})
		return ErrorResult(fmt.Sprintf("delegation to %q failed: %v", req.ToAgentKey, runErr))
	}

	t.emitEvent(ctx, eventbus.EventDelegateCompleted, eventbus.DelegateCompletedPayload{
		DelegationID: req.DelegationID,
		FromAgent:    req.FromAgentKey,
		ToAgent:      req.ToAgentKey,
		Content:      truncate(dr.Content, 500),
		MediaCount:   len(dr.Media),
	})

	resultJSON, _ := json.Marshal(map[string]any{
		"delegation_id": req.DelegationID,
		"agent":         req.ToAgentKey,
		"status":        "completed",
		"content":       dr.Content,
	})
	r := NewResult(string(resultJSON))
	r.Media = dr.Media
	return r
}

// executeAsyncMode spawns a goroutine and returns immediately.
func (t *DelegateTool) executeAsyncMode(ctx context.Context, job *delegateArtifactJob) *Result {
	req := job.req
	finishCompletion, ok := t.beginAsyncCompletion()
	if !ok {
		job.closeCallerRoot()
		return ErrorResult("delegate tool is closing")
	}
	completionTransferred := false
	defer func() {
		if !completionTransferred {
			finishCompletion()
		}
	}()
	// Detach from parent cancellation but keep a bounded admitted callback.
	bgCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Minute)
	// The delegatee runs after the caller's turn has ended, so the caller's
	// post-turn drain has already fired by the time it calls team_tasks. Give
	// this run its own tracker, drained when the delegatee finishes, so tasks it
	// creates are dispatched and the team create lock it takes is released.
	bgCtx, drainTeamDispatch := InjectTeamDispatch(bgCtx, t.postTurn)
	announceCtx := context.WithoutCancel(ctx)
	var dr DelegateResult
	var runErr error
	runStarted := make(chan struct{})
	ticket, err := t.admission.Enqueue(bgCtx, delegateAdmissionConstraints(ctx, req), func(runCtx context.Context, lease *orchestration.ChildRunLease) {
		defer job.closeCallerRoot()
		defer drainTeamDispatch()
		close(runStarted)
		runCtx = withDelegatedAgentExecution(runCtx, lease)
		dr, runErr = t.runArtifactExchange(runCtx, job)
		lease.Release()
	})
	if err != nil {
		cancel()
		job.closeCallerRoot()
		return ErrorResult(err.Error())
	}
	if err := t.createDelegateCompletion(ctx, req); err != nil {
		ticket.Cancel()
		cancel()
		job.closeCallerRoot()
		return ErrorResult(err.Error())
	}
	if err := ticket.Activate(); err != nil {
		ticket.Cancel()
		cancel()
		job.closeCallerRoot()
		result := err.Error()
		_ = t.updateDelegateCompletion(req, TaskStatusFailed, &result)
		return ErrorResult(err.Error())
	}

	// Nonterminal observability persistence runs outside the admitted callback.
	// Terminal persistence and announcements wait for the callback to return,
	// which proves its execution permit has been released.
	go func() {
		defer finishCompletion()
		started := false
		select {
		case <-runStarted:
			started = true
		case <-ticket.Done():
			select {
			case <-runStarted:
				started = true
			default:
			}
		}
		if started {
			_ = t.updateDelegateRunning(req)
		}
		<-ticket.Done()
		job.closeCallerRoot()
		cancel()
		if runErr == nil {
			runErr = ticket.Err()
		}
		if runErr != nil {
			result := runErr.Error()
			terminalPersisted := t.updateDelegateCompletion(req, TaskStatusFailed, &result) == nil
			t.emitEvent(announceCtx, eventbus.EventDelegateFailed, eventbus.DelegateFailedPayload{
				DelegationID: req.DelegationID,
				FromAgent:    req.FromAgentKey,
				ToAgent:      req.ToAgentKey,
				Error:        runErr.Error(),
			})
			slog.Warn("delegate.async.failed", "to", req.ToAgentKey, "error", runErr)
			content := fmt.Sprintf("[Delegation to %s failed: %v]", req.ToAgentKey, runErr)
			delivered := t.announceToParent(req, content, nil)
			if terminalPersisted {
				t.updateDelegateAnnouncement(req, delivered)
			} else {
				slog.Error("delegate.async.announce_without_durable_terminal",
					"delegation_id", req.DelegationID,
					"to", req.ToAgentKey,
					"delivered", delivered,
				)
			}
			if !delivered {
				slog.Warn("delegate.async.announce_deferred_to_ledger",
					"delegation_id", req.DelegationID,
					"to", req.ToAgentKey,
					"reason", "inbound_bus_full",
				)
			}
			return
		}
		completionMedia := completionMediaDescriptors(dr.Media, job.callerWorkspace, "")
		terminalPersisted := t.updateDelegateCompletionMedia(req, completionMedia) == nil
		if terminalPersisted {
			terminalPersisted = t.updateDelegateCompletion(req, TaskStatusCompleted, &dr.Content) == nil
		}
		t.emitEvent(announceCtx, eventbus.EventDelegateCompleted, eventbus.DelegateCompletedPayload{
			DelegationID: req.DelegationID,
			FromAgent:    req.FromAgentKey,
			ToAgent:      req.ToAgentKey,
			Content:      truncate(dr.Content, 500),
			MediaCount:   len(dr.Media),
		})
		content := fmt.Sprintf("[Delegation result from %s]\n\n%s", req.ToAgentKey, dr.Content)
		delivered := t.announceToParent(req, content, dr.Media)
		if terminalPersisted {
			t.updateDelegateAnnouncement(req, delivered)
		} else {
			slog.Error("delegate.async.announce_without_durable_terminal",
				"delegation_id", req.DelegationID,
				"to", req.ToAgentKey,
				"delivered", delivered,
			)
		}
		if !delivered {
			slog.Warn("delegate.async.announce_deferred_to_ledger",
				"delegation_id", req.DelegationID,
				"to", req.ToAgentKey,
				"reason", "inbound_bus_full",
			)
		}
	}()
	completionTransferred = true

	result, _ := json.Marshal(map[string]any{
		"delegation_id": req.DelegationID,
		"agent":         req.ToAgentKey,
		"status":        "delegated",
		"message":       fmt.Sprintf("Task delegated to %s. You will be notified when complete.", req.ToAgentKey),
	})
	return NewResult(string(result))
}

func (t *DelegateTool) runAdmitted(
	ctx context.Context,
	req DelegateRequest,
	onDispatch func(context.Context),
) (DelegateResult, error) {
	if t.hookDispatcher != nil {
		evt := hooks.Event{
			EventID:   uuid.NewString(),
			SessionID: req.SessionKey,
			TenantID:  parseUUIDOrNil(req.TenantID),
			AgentID:   req.FromAgentID,
			HookEvent: hooks.EventSubagentStart,
			Depth:     hooks.DepthFrom(ctx),
		}
		result, err := t.hookDispatcher.Fire(ctx, evt)
		if err != nil {
			return DelegateResult{}, fmt.Errorf("subagent_start hook error: %w", err)
		}
		if result.Decision == hooks.DecisionBlock {
			return DelegateResult{}, fmt.Errorf("delegation to %q blocked by hook policy", req.ToAgentKey)
		}
		ctx = hooks.IncDepth(ctx)
	}
	if onDispatch != nil {
		onDispatch(ctx)
	}
	return t.runFn(ctx, req)
}

type delegateInput struct {
	relativePath   string
	taskReferences []string
}

type delegateArtifactJob struct {
	req             DelegateRequest
	mode            string
	callerRoot      *DelegationArtifactRoot
	callerWorkspace string
	tenantWorkspace string
	tenantID        uuid.UUID
	tenantSlug      string
	delegationID    uuid.UUID
	inputs          []delegateInput
	callerLocation  *delegationArtifactCallerLocation
	closeOnce       sync.Once
}

func (j *delegateArtifactJob) closeCallerRoot() {
	j.closeOnce.Do(func() {
		_ = j.callerRoot.Close()
	})
}

func parseDelegateInputs(raw any) ([]string, error) {
	if raw == nil {
		return nil, nil
	}
	values, ok := raw.([]any)
	if !ok {
		if typed, typedOK := raw.([]string); typedOK {
			values = make([]any, len(typed))
			for i, value := range typed {
				values[i] = value
			}
		} else {
			return nil, fmt.Errorf("inputs must be an array of relative paths")
		}
	}
	if len(values) > DelegationArtifactMaxFiles {
		return nil, fmt.Errorf("inputs may contain at most %d paths", DelegationArtifactMaxFiles)
	}
	inputs := make([]string, 0, len(values))
	for i, value := range values {
		rawPath, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("input %d must be a relative path", i+1)
		}
		normalized, err := validateArtifactRelativePath(rawPath)
		if err != nil {
			return nil, fmt.Errorf("input %d: %w", i+1, err)
		}
		inputs = append(inputs, normalized)
	}
	return inputs, nil
}

func collectDelegateInputs(callerWorkspace string, explicitInputs, currentMedia []string) ([]delegateInput, error) {
	inputs := make([]delegateInput, 0, len(explicitInputs)+len(currentMedia))
	positions := make(map[string]int, cap(inputs))
	add := func(relativePath string, taskReferences ...string) {
		if position, exists := positions[relativePath]; exists {
			for _, reference := range taskReferences {
				if reference != "" && !slices.Contains(inputs[position].taskReferences, reference) {
					inputs[position].taskReferences = append(inputs[position].taskReferences, reference)
				}
			}
			return
		}
		positions[relativePath] = len(inputs)
		inputs = append(inputs, delegateInput{
			relativePath: relativePath,
			taskReferences: slices.DeleteFunc(taskReferences, func(reference string) bool {
				return reference == ""
			}),
		})
	}
	for _, relativePath := range explicitInputs {
		add(relativePath)
	}
	for i, mediaPath := range currentMedia {
		absolutePath, err := filepath.Abs(mediaPath)
		if err != nil {
			return nil, fmt.Errorf("current media input %d is unavailable", i+1)
		}
		relativePath, err := filepath.Rel(callerWorkspace, absolutePath)
		if err != nil {
			return nil, fmt.Errorf("current media input %d is outside the caller workspace", i+1)
		}
		relativePath = filepath.ToSlash(relativePath)
		normalized, err := validateArtifactRelativePath(relativePath)
		if err != nil || normalized == "." || strings.HasPrefix(normalized, "../") {
			return nil, fmt.Errorf("current media input %d is outside the caller workspace", i+1)
		}
		add(normalized, absolutePath, filepath.ToSlash(absolutePath))
	}
	if len(inputs) > DelegationArtifactMaxFiles {
		return nil, fmt.Errorf("delegation inputs may contain at most %d files", DelegationArtifactMaxFiles)
	}
	return inputs, nil
}

func (t *DelegateTool) runArtifactExchange(ctx context.Context, job *delegateArtifactJob) (dr DelegateResult, returnErr error) {
	t.beginDelegationArtifactExchange(job.tenantWorkspace, job.delegationID)
	defer t.endDelegationArtifactExchange(job.tenantWorkspace, job.delegationID)

	exchange, err := NewDelegationArtifactExchange(
		job.tenantWorkspace,
		job.tenantID,
		job.delegationID,
		DelegationArtifactLimits{},
		delegationArtifactFailureTTL,
	)
	if err != nil {
		return DelegateResult{}, err
	}
	durablePublished := false
	publicationTempPath := ""
	var traceID uuid.UUID
	var staged []DelegationArtifact
	var publication DelegationArtifactPublication
	var stagedTraceOnce sync.Once
	emitStaged := func(id uuid.UUID, at time.Time) {
		if id == uuid.Nil {
			return
		}
		stagedTraceOnce.Do(func() {
			traceID = id
			emitDelegationArtifactLifecycleSpan(
				ctx,
				job,
				id,
				"staged",
				at,
				traceOutputsFromStaged(staged),
			)
		})
	}
	defer func() {
		if !durablePublished {
			exchange.RetainFailure(time.Now(), artifactErrorCode(returnErr))
			lifecycleStatus := artifactLifecycleFailed
			traceStatus := "failed"
			if errors.Is(returnErr, context.Canceled) ||
				errors.Is(returnErr, context.DeadlineExceeded) {
				lifecycleStatus = artifactLifecycleCancelled
				traceStatus = "cancelled"
			}
			if err := t.registerRetainedDelegationExchange(exchange, job, lifecycleStatus); err != nil {
				returnErr = errors.Join(returnErr, err)
			}
			if traceID == uuid.Nil {
				traceID = job.req.OriginTraceID
			}
			emitDelegationArtifactLifecycleSpan(
				ctx,
				job,
				traceID,
				traceStatus,
				time.Now(),
				traceOutputsFromStaged(staged),
			)
		}
		if err := exchange.Close(); err != nil {
			slog.Warn("delegate.artifact_exchange_close_failed", "delegation_id", job.req.DelegationID)
		}
		if durablePublished {
			if err := t.tryRemoveDelegationExchange(job.tenantWorkspace, job.delegationID); err != nil {
				slog.Warn("delegate.artifact_exchange_cleanup_failed", "delegation_id", job.req.DelegationID)
				cleanupCtx := context.WithoutCancel(ctx)
				t.registerPublishedDelegationCleanup(job, publicationTempPath, func() {
					emitDelegationArtifactLifecycleSpan(
						cleanupCtx,
						job,
						traceID,
						"cleaned",
						time.Now(),
						traceOutputsFromManifest(publication.Manifest),
					)
				})
			} else {
				emitDelegationArtifactLifecycleSpan(
					ctx,
					job,
					traceID,
					"cleaned",
					time.Now(),
					traceOutputsFromManifest(publication.Manifest),
				)
			}
		}
	}()
	if err := t.updateActiveDelegationLifecycle(exchange, job); err != nil {
		return DelegateResult{}, err
	}

	relativePaths := make([]string, len(job.inputs))
	for i, input := range job.inputs {
		relativePaths[i] = input.relativePath
	}
	stagedAt := time.Now()
	staged, err = exchange.StageInputs(ctx, job.callerRoot, relativePaths)
	if err != nil {
		return DelegateResult{}, err
	}
	if err := t.markDelegationRunning(exchange, job, time.Now()); err != nil {
		return DelegateResult{}, err
	}

	req := job.req
	req.Task = delegationTaskWithInputAliases(req.Task, job.inputs, staged)
	req.DelegateInputsPath = exchange.InputsMount().HostRoot
	req.DelegateOutputsPath = exchange.OutputsHostPath()
	req.OnTraceCreated = func(id uuid.UUID) {
		emitStaged(id, stagedAt)
	}
	dr, err = t.runAdmitted(ctx, req, func(dispatchCtx context.Context) {
		t.emitEvent(dispatchCtx, eventbus.EventDelegateSent, eventbus.DelegateSentPayload{
			DelegationID: req.DelegationID,
			FromAgent:    req.FromAgentKey,
			ToAgent:      req.ToAgentKey,
			Task:         delegationEventTask(req.Task, job.callerWorkspace, exchange),
			Mode:         job.mode,
		})
	})
	if dr.TraceID != uuid.Nil {
		traceID = dr.TraceID
		emitStaged(dr.TraceID, stagedAt)
	} else if traceID == uuid.Nil {
		traceID = job.req.OriginTraceID
		emitStaged(traceID, stagedAt)
	}
	if err != nil {
		return DelegateResult{}, redactDelegationArtifactError(err, exchange)
	}
	dr.Content = redactDelegationArtifactText(dr.Content, exchange)
	dr.Media = nil

	publication, err = exchange.publishWithPreparation(
		ctx,
		job.callerRoot,
		time.Now(),
		func(tempPath string) error {
			publicationTempPath = tempPath
			return t.markDelegationPublishing(exchange, job, tempPath, time.Now())
		},
	)
	if err != nil {
		return DelegateResult{}, err
	}
	durablePublished = true
	if err := t.markDelegationPublished(exchange, job, publication.Manifest.PublishedAt); err != nil {
		// The no-replace rename and directory sync completed before this
		// best-effort lifecycle update. Reporting the delegation as failed here
		// invites a retry that creates a second durable publication even though
		// the caller already owns the first one.
		slog.Warn("delegate.artifact_lifecycle_published_failed",
			"delegation_id", job.req.DelegationID,
			"error", err,
		)
	}
	dr.Media = publicationMedia(job.callerWorkspace, publication)
	emitDelegationArtifactLifecycleSpan(
		ctx,
		job,
		traceID,
		"published",
		publication.Manifest.PublishedAt,
		traceOutputsFromManifest(publication.Manifest),
	)
	return dr, nil
}

type delegationArtifactTraceOutput struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"size_bytes"`
	SHA256    string `json:"sha256"`
	MediaType string `json:"media_type"`
}

type delegationArtifactTraceEvent struct {
	DelegationID  string                          `json:"delegation_id"`
	OccurredAt    time.Time                       `json:"occurred_at"`
	ArtifactCount int                             `json:"artifact_count"`
	ArtifactBytes int64                           `json:"artifact_bytes"`
	Artifacts     []delegationArtifactTraceOutput `json:"artifacts"`
	Status        string                          `json:"status"`
}

func emitDelegationArtifactLifecycleSpan(
	ctx context.Context,
	job *delegateArtifactJob,
	traceID uuid.UUID,
	status string,
	occurredAt time.Time,
	artifacts []delegationArtifactTraceOutput,
) {
	collector := tracing.CollectorFromContext(ctx)
	if collector == nil || traceID == uuid.Nil {
		return
	}
	var artifactBytes int64
	for _, artifact := range artifacts {
		artifactBytes += artifact.SizeBytes
	}
	event := delegationArtifactTraceEvent{
		DelegationID:  job.delegationID.String(),
		OccurredAt:    occurredAt.UTC(),
		ArtifactCount: len(artifacts),
		ArtifactBytes: artifactBytes,
		Artifacts:     artifacts,
		Status:        status,
	}
	metadata, err := json.Marshal(event)
	if err != nil {
		return
	}
	now := time.Now().UTC()
	collector.EmitSpan(tracing.RedactSpan(ctx, store.SpanData{
		ID:        store.GenNewID(),
		TraceID:   traceID,
		SpanType:  store.SpanTypeEvent,
		Name:      "delegate.artifacts." + status,
		StartTime: now,
		EndTime:   &now,
		Status:    store.SpanStatusCompleted,
		Level:     store.SpanLevelDefault,
		Metadata:  metadata,
		TenantID:  job.tenantID,
		CreatedAt: now,
	}))
}

func traceOutputsFromStaged(
	staged []DelegationArtifact,
) []delegationArtifactTraceOutput {
	outputs := make([]delegationArtifactTraceOutput, len(staged))
	for i, artifact := range staged {
		outputs[i] = delegationArtifactTraceOutput{
			Path:      artifact.Path,
			SizeBytes: artifact.SizeBytes,
			SHA256:    artifact.SHA256,
			MediaType: artifact.MediaType,
		}
	}
	return outputs
}

func traceOutputsFromManifest(
	manifest DelegationArtifactManifest,
) []delegationArtifactTraceOutput {
	outputs := make([]delegationArtifactTraceOutput, len(manifest.Outputs))
	for i, output := range manifest.Outputs {
		outputs[i] = delegationArtifactTraceOutput{
			Path:      output.Path,
			SizeBytes: output.SizeBytes,
			SHA256:    output.SHA256,
			MediaType: output.MediaType,
		}
	}
	return outputs
}

func delegationTaskWithInputAliases(task string, inputs []delegateInput, staged []DelegationArtifact) string {
	if len(staged) == 0 {
		return task
	}
	aliases := make([]string, 0, len(staged))
	for i, artifact := range staged {
		for _, reference := range inputs[i].taskReferences {
			task = replaceExactPathReference(task, reference, artifact.Path)
		}
		source := inputs[i].relativePath
		if source == "" || source == artifact.Path {
			aliases = append(aliases, artifact.Path)
			continue
		}
		aliases = append(aliases, source+" => "+artifact.Path)
	}
	return task + "\n\nRead-only delegation inputs: " + strings.Join(aliases, ", ")
}

func replaceExactPathReference(text, reference, replacement string) string {
	if reference == "" || reference == replacement {
		return text
	}
	var rewritten strings.Builder
	remaining := text
	for {
		index := strings.Index(remaining, reference)
		if index < 0 {
			rewritten.WriteString(remaining)
			return rewritten.String()
		}
		beforeOK := index == 0 || !isPathReferenceByte(remaining[index-1])
		afterIndex := index + len(reference)
		afterOK := afterIndex == len(remaining) || !isPathReferenceByte(remaining[afterIndex])
		if beforeOK && afterOK {
			rewritten.WriteString(remaining[:index])
			rewritten.WriteString(replacement)
			remaining = remaining[afterIndex:]
			continue
		}
		rewritten.WriteString(remaining[:index+len(reference)])
		remaining = remaining[index+len(reference):]
	}
}

func isPathReferenceByte(value byte) bool {
	switch {
	case value >= 'a' && value <= 'z',
		value >= 'A' && value <= 'Z',
		value >= '0' && value <= '9':
		return true
	}
	switch value {
	case '_', '-', '.', '/', '\\':
		return true
	default:
		return false
	}
}

func publicationMedia(callerWorkspace string, publication DelegationArtifactPublication) []bus.MediaFile {
	if len(publication.Manifest.Outputs) == 0 {
		return nil
	}
	media := make([]bus.MediaFile, len(publication.Manifest.Outputs))
	for i, output := range publication.Manifest.Outputs {
		durableRelative := filepath.FromSlash(filepath.Join(publication.RootPath, output.Path))
		media[i] = bus.MediaFile{
			Path:     filepath.Join(callerWorkspace, durableRelative),
			MimeType: output.MediaType,
			Filename: filepath.Base(output.Path),
		}
	}
	return media
}

func redactDelegationArtifactError(err error, exchange *DelegationArtifactExchange) error {
	if err == nil {
		return nil
	}
	return &delegationRedactedError{
		message: redactDelegationArtifactText(err.Error(), exchange),
		cause:   err,
	}
}

type delegationRedactedError struct {
	message string
	cause   error
}

func (e *delegationRedactedError) Error() string { return e.message }
func (e *delegationRedactedError) Unwrap() error { return e.cause }

func redactDelegationArtifactText(text string, exchange *DelegationArtifactExchange) string {
	replacements := []struct {
		hostPath string
		alias    string
	}{
		{exchange.InputsMount().HostRoot, "inputs"},
		{exchange.OutputsHostPath(), "outputs"},
		{exchange.hostRoot, "delegation exchange"},
	}
	for _, replacement := range replacements {
		for _, variant := range artifactPathRedactionVariants(replacement.hostPath) {
			text = strings.ReplaceAll(text, variant, replacement.alias)
		}
	}
	return text
}

func artifactPathRedactionVariants(hostPath string) []string {
	variants := []string{
		hostPath,
		filepath.ToSlash(hostPath),
		filepath.FromSlash(hostPath),
		strings.ReplaceAll(hostPath, `\`, "/"),
		strings.ReplaceAll(hostPath, "/", `\`),
	}
	seen := make(map[string]struct{}, len(variants))
	result := make([]string, 0, len(variants))
	for _, variant := range variants {
		if variant == "" {
			continue
		}
		if _, ok := seen[variant]; ok {
			continue
		}
		seen[variant] = struct{}{}
		result = append(result, variant)
	}
	return result
}

func delegationEventTask(
	task string,
	callerWorkspace string,
	exchange *DelegationArtifactExchange,
) string {
	task = redactDelegationArtifactText(task, exchange)
	if callerWorkspace != "" {
		task = strings.ReplaceAll(task, callerWorkspace, "caller workspace")
		task = strings.ReplaceAll(
			task,
			filepath.ToSlash(callerWorkspace),
			"caller workspace",
		)
	}
	return task
}

func delegateAdmissionConstraints(ctx context.Context, req DelegateRequest) orchestration.ChildRunConstraints {
	parentTaskID, depth := childRunContinuationLineage(
		ctx,
		subagentTaskIDFromContext(ctx),
		subagentDepthFromContext(ctx, 0)+1,
	)
	return orchestration.ChildRunConstraints{
		TenantID:     parseUUIDOrNil(req.TenantID),
		RootAgentID:  uuid.Nil,
		TaskID:       req.DelegationID,
		ParentTaskID: parentTaskID,
		Depth:        depth,
	}
}

// announceToParent delivers the delegate result back to the parent agent's
// conversation via msgBus, following the same pattern as subagent announce.
func (t *DelegateTool) announceToParent(req DelegateRequest, content string, media []bus.MediaFile) bool {
	if t.msgBus == nil || req.ChatID == "" {
		return false
	}
	tenantUUID, _ := uuid.Parse(req.TenantID)
	meta := map[string]string{
		"origin_channel":     req.Channel,
		"origin_peer_kind":   req.PeerKind,
		"origin_session_key": req.SessionKey,
		"delegation_id":      req.DelegationID,
		"delegate_from":      req.FromAgentKey,
		"delegate_to":        req.ToAgentKey,
		MetaParentAgent:      req.FromAgentKey,
		MetaOriginTraceID:    req.OriginTraceID.String(),
		MetaOriginRootSpanID: req.OriginRootSpanID.String(),
	}
	if req.SenderID != "" {
		meta[MetaOriginSenderID] = req.SenderID
	}
	if req.Role != "" {
		meta[MetaOriginRole] = req.Role
	}
	if req.UserID != "" {
		meta[MetaOriginUserID] = req.UserID
	}
	return PublishAsyncCompletion(context.Background(), t.msgBus, bus.InboundMessage{
		Channel:  "system",
		SenderID: fmt.Sprintf("subagent:delegate:%s", req.DelegationID),
		ChatID:   req.ChatID,
		Content:  content,
		Media:    media,
		UserID:   req.UserID,
		TenantID: tenantUUID,
		Metadata: meta,
	})
}

// parseUUIDOrNil parses s as a UUID; returns uuid.Nil on failure.
func parseUUIDOrNil(s string) uuid.UUID {
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil
	}
	return id
}

func (t *DelegateTool) emitEvent(ctx context.Context, eventType eventbus.EventType, payload any) {
	if t.eventBus == nil {
		return
	}
	t.eventBus.Publish(eventbus.DomainEvent{
		ID:        uuid.New().String(),
		Type:      eventType,
		TenantID:  store.TenantIDFromContext(ctx).String(),
		AgentID:   store.AgentIDFromContext(ctx).String(),
		UserID:    store.ActorIDFromContext(ctx), // audit actor, not scope (#915)
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	})
}
