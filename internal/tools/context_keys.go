package tools

import (
	"context"
	"log/slog"
	"sync"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/sandbox"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Tool execution context keys.
// These replace mutable setter fields on tool instances, making tools thread-safe
// for concurrent execution. Values are injected into context by the registry
// and read by individual tools during Execute().

type toolContextKey string

const (
	ctxChannel     toolContextKey = "tool_channel"
	ctxChannelType toolContextKey = "tool_channel_type"
	ctxChatID      toolContextKey = "tool_chat_id"
	ctxPeerKind    toolContextKey = "tool_peer_kind"
	ctxLocalKey    toolContextKey = "tool_local_key" // composite key with topic/thread suffix for routing
	ctxSandboxKey  toolContextKey = "tool_sandbox_key"
	ctxAsyncCB     toolContextKey = "tool_async_cb"
	ctxWorkspace   toolContextKey = "tool_workspace"
	ctxAgentKey    toolContextKey = "tool_agent_key"
	ctxSessionKey  toolContextKey = "tool_session_key" // origin session key for announce routing
	ctxRunKind     toolContextKey = "tool_run_kind"    // "notification", "announce", "delegation"
)

// Well-known channel names used for routing and access control.
const (
	ChannelSystem    = "system"
	ChannelDashboard = "dashboard"
	ChannelTeammate  = "teammate"
)

// MediaPathLoader resolves a media ID to a local file path.
// Used by media analysis tools (read_document, read_audio, read_video).
type MediaPathLoader interface {
	LoadPath(id string) (string, error)
}

func WithToolChannel(ctx context.Context, channel string) context.Context {
	return context.WithValue(ctx, ctxChannel, channel)
}

func ToolChannelFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(ctxChannel).(string)
	return v
}

func WithToolChannelType(ctx context.Context, channelType string) context.Context {
	return context.WithValue(ctx, ctxChannelType, channelType)
}

func ToolChannelTypeFromCtx(ctx context.Context) string {
	if v, _ := ctx.Value(ctxChannelType).(string); v != "" {
		return v
	}
	if rc := store.RunContextFromCtx(ctx); rc != nil {
		return rc.ChannelType
	}
	return ""
}

func WithToolChatID(ctx context.Context, chatID string) context.Context {
	return context.WithValue(ctx, ctxChatID, chatID)
}

func ToolChatIDFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(ctxChatID).(string)
	return v
}

func WithToolPeerKind(ctx context.Context, peerKind string) context.Context {
	return context.WithValue(ctx, ctxPeerKind, peerKind)
}

func ToolPeerKindFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(ctxPeerKind).(string)
	return v
}

// WithToolLocalKey injects the composite local key (e.g. "-100123:topic:42") into context.
// Used by delegation/subagent to preserve topic routing info for announce-back.
func WithToolLocalKey(ctx context.Context, localKey string) context.Context {
	return context.WithValue(ctx, ctxLocalKey, localKey)
}

func ToolLocalKeyFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(ctxLocalKey).(string)
	return v
}

func WithToolSandboxKey(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, ctxSandboxKey, key)
}

func ToolSandboxKeyFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(ctxSandboxKey).(string)
	return v
}

func WithToolAsyncCB(ctx context.Context, cb AsyncCallback) context.Context {
	return context.WithValue(ctx, ctxAsyncCB, cb)
}

func ToolAsyncCBFromCtx(ctx context.Context) AsyncCallback {
	v, _ := ctx.Value(ctxAsyncCB).(AsyncCallback)
	return v
}

func WithToolWorkspace(ctx context.Context, ws string) context.Context {
	return context.WithValue(ctx, ctxWorkspace, ws)
}

func ToolWorkspaceFromCtx(ctx context.Context) string {
	if v, _ := ctx.Value(ctxWorkspace).(string); v != "" {
		return v
	}
	if rc := store.RunContextFromCtx(ctx); rc != nil {
		return rc.Workspace
	}
	return ""
}

// WithToolAgentKey injects the calling agent's key into context.
// Multiple agents share a single tool registry; the agent key
// lets tools like spawn/subagent identify which agent is the parent.
func WithToolAgentKey(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, ctxAgentKey, key)
}

func ToolAgentKeyFromCtx(ctx context.Context) string {
	if v, _ := ctx.Value(ctxAgentKey).(string); v != "" {
		return v
	}
	if rc := store.RunContextFromCtx(ctx); rc != nil {
		return rc.AgentToolKey
	}
	return ""
}

// WithToolSessionKey injects the parent's session key so subagent announce
// can route results back to the exact same session (required for WS where
// session keys don't follow BuildScopedSessionKey format).
func WithToolSessionKey(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, ctxSessionKey, key)
}

func ToolSessionKeyFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(ctxSessionKey).(string)
	return v
}

// WithRunKind injects the run classification (e.g. "notification") into context.
func WithRunKind(ctx context.Context, kind string) context.Context {
	return context.WithValue(ctx, ctxRunKind, kind)
}

// RunKindFromCtx returns the run kind from context, or empty string.
func RunKindFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(ctxRunKind).(string)
	return v
}

// RunKindNotification is the run kind for team task notification runs.
// Leader agents in this mode can only relay status — mutations are blocked.
const RunKindNotification = "notification"

// --- Builtin tool settings (global DB overrides) ---

const ctxBuiltinToolSettings toolContextKey = "tool_builtin_settings"

// BuiltinToolSettings maps tool name → settings JSON bytes.
type BuiltinToolSettings map[string][]byte

func WithBuiltinToolSettings(ctx context.Context, settings BuiltinToolSettings) context.Context {
	return context.WithValue(ctx, ctxBuiltinToolSettings, settings)
}

func BuiltinToolSettingsFromCtx(ctx context.Context) BuiltinToolSettings {
	if v, _ := ctx.Value(ctxBuiltinToolSettings).(BuiltinToolSettings); v != nil {
		return v
	}
	if rc := store.RunContextFromCtx(ctx); rc != nil {
		return BuiltinToolSettings(rc.BuiltinToolSettings)
	}
	return nil
}

// --- Per-agent restrict_to_workspace override ---

const ctxRestrictWs toolContextKey = "tool_restrict_to_workspace"

// WithRestrictToWorkspace injects a per-agent restrict_to_workspace override into context.
func WithRestrictToWorkspace(ctx context.Context, restrict bool) context.Context {
	return context.WithValue(ctx, ctxRestrictWs, restrict)
}

// RestrictFromCtx returns the per-agent restrict_to_workspace override.
func RestrictFromCtx(ctx context.Context) (bool, bool) {
	v, ok := ctx.Value(ctxRestrictWs).(bool)
	return v, ok
}

func effectiveRestrict(ctx context.Context, toolDefault bool) bool {
	// Multi-tenant security: always restrict agents to their workspace.
	// Agents must not access files outside their tenant-scoped workspace.
	return true
}

// --- Parent agent model (for subagent inheritance) ---

const ctxParentModel toolContextKey = "tool_parent_model"

// WithParentModel sets the parent agent's model in context so subagents can inherit it.
func WithParentModel(ctx context.Context, model string) context.Context {
	return context.WithValue(ctx, ctxParentModel, model)
}

// ParentModelFromCtx returns the parent agent's model from context.
func ParentModelFromCtx(ctx context.Context) string {
	if v, _ := ctx.Value(ctxParentModel).(string); v != "" {
		return v
	}
	if rc := store.RunContextFromCtx(ctx); rc != nil {
		return rc.ParentModel
	}
	return ""
}

// --- Parent agent provider (for subagent inheritance) ---

const ctxParentProvider toolContextKey = "tool_parent_provider"

// WithParentProvider sets the parent agent's provider name in context so subagents inherit it.
func WithParentProvider(ctx context.Context, providerName string) context.Context {
	return context.WithValue(ctx, ctxParentProvider, providerName)
}

// ParentProviderFromCtx returns the parent agent's provider name from context.
func ParentProviderFromCtx(ctx context.Context) string {
	if v, _ := ctx.Value(ctxParentProvider).(string); v != "" {
		return v
	}
	if rc := store.RunContextFromCtx(ctx); rc != nil {
		return rc.ParentProvider
	}
	return ""
}

// --- Per-agent subagent config override ---

const ctxSubagentCfg toolContextKey = "tool_subagent_config"

func WithSubagentConfig(ctx context.Context, cfg *config.SubagentsConfig) context.Context {
	return context.WithValue(ctx, ctxSubagentCfg, cfg)
}

func SubagentConfigFromCtx(ctx context.Context) *config.SubagentsConfig {
	if v, _ := ctx.Value(ctxSubagentCfg).(*config.SubagentsConfig); v != nil {
		return v
	}
	if rc := store.RunContextFromCtx(ctx); rc != nil {
		return rc.SubagentsCfg
	}
	return nil
}

// --- Per-agent memory config override ---

const ctxMemoryCfg toolContextKey = "tool_memory_config"

func WithMemoryConfig(ctx context.Context, cfg *config.MemoryConfig) context.Context {
	return context.WithValue(ctx, ctxMemoryCfg, cfg)
}

func MemoryConfigFromCtx(ctx context.Context) *config.MemoryConfig {
	if v, _ := ctx.Value(ctxMemoryCfg).(*config.MemoryConfig); v != nil {
		return v
	}
	if rc := store.RunContextFromCtx(ctx); rc != nil {
		return rc.MemoryCfg
	}
	return nil
}

// --- Team ID propagation (task dispatch → workspace tools) ---

const ctxTeamID toolContextKey = "tool_team_id"

// WithToolTeamID injects the dispatching team's ID into context so team
// tools (team_tasks) and the WorkspaceInterceptor resolve
// the correct team when the agent belongs to multiple teams.
func WithToolTeamID(ctx context.Context, teamID string) context.Context {
	return context.WithValue(ctx, ctxTeamID, teamID)
}

// ToolTeamIDFromCtx returns the dispatching team's ID from context.
func ToolTeamIDFromCtx(ctx context.Context) string {
	if v, _ := ctx.Value(ctxTeamID).(string); v != "" {
		return v
	}
	if rc := store.RunContextFromCtx(ctx); rc != nil {
		return rc.TeamID
	}
	return ""
}

// --- Team workspace path (accessible but not default) ---

const ctxTeamWorkspace toolContextKey = "tool_team_workspace"

// WithToolTeamWorkspace stores the team shared workspace directory path.
// File tools allow access to this path even when restrict_to_workspace is true.
func WithToolTeamWorkspace(ctx context.Context, dir string) context.Context {
	return context.WithValue(ctx, ctxTeamWorkspace, dir)
}

// ToolTeamWorkspaceFromCtx returns the team shared workspace directory path.
func ToolTeamWorkspaceFromCtx(ctx context.Context) string {
	if v, _ := ctx.Value(ctxTeamWorkspace).(string); v != "" {
		return v
	}
	if rc := store.RunContextFromCtx(ctx); rc != nil {
		return rc.TeamWorkspace
	}
	return ""
}

// --- Team task ID propagation (delegation origin → workspace tools) ---

const ctxTeamTaskID toolContextKey = "tool_team_task_id"

// WithTeamTaskID injects the delegation's team task ID into context
// so workspace tools can auto-link files to the active task.
func WithTeamTaskID(ctx context.Context, taskID string) context.Context {
	return context.WithValue(ctx, ctxTeamTaskID, taskID)
}

// TeamTaskIDFromCtx returns the delegation's team task ID from context.
func TeamTaskIDFromCtx(ctx context.Context) string {
	if v, _ := ctx.Value(ctxTeamTaskID).(string); v != "" {
		return v
	}
	if rc := store.RunContextFromCtx(ctx); rc != nil {
		return rc.TeamTaskID
	}
	return ""
}

// --- Leader agent ID propagation (team task dispatch → memory interceptor) ---

const ctxLeaderAgentID toolContextKey = "tool_leader_agent_id"

// WithLeaderAgentID injects the team leader's agent UUID string into context
// so the memory interceptor can fallback-read leader's memory for team members.
func WithLeaderAgentID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxLeaderAgentID, id)
}

// LeaderAgentIDFromCtx returns the leader's agent UUID string from context.
func LeaderAgentIDFromCtx(ctx context.Context) string {
	if v, _ := ctx.Value(ctxLeaderAgentID).(string); v != "" {
		return v
	}
	if rc := store.RunContextFromCtx(ctx); rc != nil {
		return rc.LeaderAgentID
	}
	return ""
}

// --- Workspace scope propagation (delegation origin) ---

const (
	ctxWsChannel toolContextKey = "tool_workspace_channel"
	ctxWsChatID  toolContextKey = "tool_workspace_chat_id"
)

func WithWorkspaceChannel(ctx context.Context, channel string) context.Context {
	return context.WithValue(ctx, ctxWsChannel, channel)
}

func WorkspaceChannelFromCtx(ctx context.Context) string {
	if v, _ := ctx.Value(ctxWsChannel).(string); v != "" {
		return v
	}
	if rc := store.RunContextFromCtx(ctx); rc != nil {
		return rc.WorkspaceChannel
	}
	return ""
}

func WithWorkspaceChatID(ctx context.Context, chatID string) context.Context {
	return context.WithValue(ctx, ctxWsChatID, chatID)
}

func WorkspaceChatIDFromCtx(ctx context.Context) string {
	if v, _ := ctx.Value(ctxWsChatID).(string); v != "" {
		return v
	}
	if rc := store.RunContextFromCtx(ctx); rc != nil {
		return rc.WorkspaceChatID
	}
	return ""
}

// --- Pending team task dispatch (post-turn processing) ---

const ctxPendingDispatch toolContextKey = "tool_pending_team_dispatch"

// PendingTeamDispatch tracks team tasks created during an agent turn.
// After the turn ends, the consumer drains and dispatches them.
// Thread-safe: tools may execute in parallel goroutines.
type PendingTeamDispatch struct {
	mu       sync.Mutex
	tasks    map[uuid.UUID][]uuid.UUID // teamID → []taskID
	listed   bool                      // true after list called in this turn
	teamLock *sync.Mutex               // acquired on list, released before post-turn dispatch
}

func NewPendingTeamDispatch() *PendingTeamDispatch {
	return &PendingTeamDispatch{tasks: make(map[uuid.UUID][]uuid.UUID)}
}

// Add records a task created during this turn.
func (p *PendingTeamDispatch) Add(teamID, taskID uuid.UUID) {
	p.mu.Lock()
	p.tasks[teamID] = append(p.tasks[teamID], taskID)
	p.mu.Unlock()
}

// Drain returns all tracked tasks and resets the container.
func (p *PendingTeamDispatch) Drain() map[uuid.UUID][]uuid.UUID {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := p.tasks
	p.tasks = make(map[uuid.UUID][]uuid.UUID)
	return out
}

// MarkListed records that list was called in this turn.
func (p *PendingTeamDispatch) MarkListed() {
	p.mu.Lock()
	p.listed = true
	p.mu.Unlock()
}

// HasListed reports whether list was called in this turn.
func (p *PendingTeamDispatch) HasListed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.listed
}

// SetTeamLock stores the acquired team create lock so it can be released post-turn.
func (p *PendingTeamDispatch) SetTeamLock(m *sync.Mutex) {
	p.mu.Lock()
	p.teamLock = m
	p.mu.Unlock()
}

// ReleaseTeamLock releases the held team create lock, if any.
func (p *PendingTeamDispatch) ReleaseTeamLock() {
	p.mu.Lock()
	if p.teamLock != nil {
		p.teamLock.Unlock()
		p.teamLock = nil
	}
	p.mu.Unlock()
}

func WithPendingTeamDispatch(ctx context.Context, ptd *PendingTeamDispatch) context.Context {
	return context.WithValue(ctx, ctxPendingDispatch, ptd)
}

func PendingTeamDispatchFromCtx(ctx context.Context) *PendingTeamDispatch {
	v, _ := ctx.Value(ctxPendingDispatch).(*PendingTeamDispatch)
	return v
}

// InjectTeamDispatch creates a fresh PendingTeamDispatch context for a direct
// loop.Run() call (WS chat.send, HTTP API, etc.) and returns a drain function
// that must be called after the run completes. The drain function dispatches
// any pending team tasks via the provided PostTurnProcessor. It is safe to
// call even if no tasks were created. Pass nil postTurn if not available.
func InjectTeamDispatch(ctx context.Context, postTurn PostTurnProcessor) (context.Context, func()) {
	ptd := NewPendingTeamDispatch()
	ctx = WithPendingTeamDispatch(ctx, ptd)
	// Detach from caller's cancel/deadline but keep values (tenant_id, user_id, etc.)
	// so post-turn dispatch isn't aborted when the HTTP request or WS handler returns.
	detached := context.WithoutCancel(ctx)
	drain := func() {
		ptd.ReleaseTeamLock()
		if postTurn != nil {
			for teamID, taskIDs := range ptd.Drain() {
				if err := postTurn.ProcessPendingTasks(detached, teamID, taskIDs); err != nil {
					slog.Warn("post_turn: dispatch failed", "team_id", teamID, "error", err)
				}
			}
		}
	}
	return ctx, drain
}

// --- Delivered media tracker (write_file → message self-send dedup) ---

const ctxDeliveredMedia toolContextKey = "tool_delivered_media"

// DeliveredMedia tracks file paths already queued for auto-delivery by write_file.
// Injected once per run via WithDeliveredMedia; write_file marks paths, message reads them.
// Thread-safe: tools may execute in parallel goroutines.
type DeliveredMedia struct {
	mu    sync.Mutex
	paths map[string]bool
}

// NewDeliveredMedia creates an empty tracker.
func NewDeliveredMedia() *DeliveredMedia {
	return &DeliveredMedia{paths: make(map[string]bool)}
}

// Mark records a file path as queued for delivery.
func (dm *DeliveredMedia) Mark(path string) {
	dm.mu.Lock()
	dm.paths[path] = true
	dm.mu.Unlock()
}

// IsDelivered reports whether a file path has already been queued.
func (dm *DeliveredMedia) IsDelivered(path string) bool {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	return dm.paths[path]
}

// WithDeliveredMedia injects a delivered media tracker into context.
func WithDeliveredMedia(ctx context.Context, dm *DeliveredMedia) context.Context {
	return context.WithValue(ctx, ctxDeliveredMedia, dm)
}

// DeliveredMediaFromCtx returns the delivered media tracker, or nil.
func DeliveredMediaFromCtx(ctx context.Context) *DeliveredMedia {
	v, _ := ctx.Value(ctxDeliveredMedia).(*DeliveredMedia)
	return v
}

// --- Run media file paths (for team workspace auto-collect) ---

const ctxRunMediaPaths toolContextKey = "tool_run_media_paths"

// WithRunMediaPaths stores the absolute file paths of media files received
// in the current run. Used by team_tasks to auto-copy files to team workspace.
func WithRunMediaPaths(ctx context.Context, paths []string) context.Context {
	return context.WithValue(ctx, ctxRunMediaPaths, paths)
}

// RunMediaPathsFromCtx returns media file paths from the current run.
func RunMediaPathsFromCtx(ctx context.Context) []string {
	v, _ := ctx.Value(ctxRunMediaPaths).([]string)
	return v
}

const ctxRunMediaNames toolContextKey = "tool_run_media_names"

// WithRunMediaNames stores the mapping from media file path to original filename.
func WithRunMediaNames(ctx context.Context, names map[string]string) context.Context {
	return context.WithValue(ctx, ctxRunMediaNames, names)
}

// RunMediaNamesFromCtx returns the media path → original filename mapping.
func RunMediaNamesFromCtx(ctx context.Context) map[string]string {
	v, _ := ctx.Value(ctxRunMediaNames).(map[string]string)
	return v
}

// --- Iteration progress (loop → tools) ---

const ctxIterProgress toolContextKey = "tool_iter_progress"

// IterationProgress carries the agent loop's current iteration state
// so tools can adapt behaviour (e.g. reduce output size) as the budget shrinks.
type IterationProgress struct {
	Current int
	Max     int
}

func WithIterationProgress(ctx context.Context, p IterationProgress) context.Context {
	return context.WithValue(ctx, ctxIterProgress, p)
}

func IterationProgressFromCtx(ctx context.Context) (IterationProgress, bool) {
	v, ok := ctx.Value(ctxIterProgress).(IterationProgress)
	return v, ok
}

// --- Per-agent sandbox config override ---

const ctxSandboxCfg toolContextKey = "tool_sandbox_config"

func WithSandboxConfig(ctx context.Context, cfg *sandbox.Config) context.Context {
	return context.WithValue(ctx, ctxSandboxCfg, cfg)
}

func SandboxConfigFromCtx(ctx context.Context) *sandbox.Config {
	if v, _ := ctx.Value(ctxSandboxCfg).(*sandbox.Config); v != nil {
		return v
	}
	if rc := store.RunContextFromCtx(ctx); rc != nil {
		return rc.SandboxCfg
	}
	return nil
}
