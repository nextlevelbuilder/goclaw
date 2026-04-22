package channels

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ChannelStream is the per-run streaming handle stored on RunContext.
// Each channel implementation returns a ChannelStream from CreateStream().
// RunContext owns the stream so concurrent runs in the same group chat
// each get their own stream — no sync.Map collision on chatID.
type ChannelStream interface {
	// Update sends or edits the streaming message with the latest accumulated text.
	Update(ctx context.Context, text string)
	// Stop finalizes the stream (final edit/flush). Called on run.completed.
	Stop(ctx context.Context) error
	// MessageID returns the platform message ID of the streaming message (0 if none).
	// Used to hand the message back to Send() via the channel's placeholder map.
	MessageID() int
}

// RunContext tracks an active agent run for streaming/reaction event forwarding.
type RunContext struct {
	ChannelName       string
	ChatID            string
	MessageID         string            // platform message ID (string to support Feishu "om_xxx", Telegram "12345", etc.)
	Metadata          map[string]string // outbound routing metadata (thread_id, local_key, group_id)
	TenantID          uuid.UUID         // tenant scope for per-tenant TTS
	Streaming         bool              // whether run uses streaming (to avoid double-delivery of block replies)
	BlockReplyEnabled bool              // whether block.reply delivery is enabled for this run (resolved at RegisterRun time)
	ToolStatusEnabled bool              // whether tool name shows in streaming preview during tool execution
	mu                sync.Mutex
	streamBuffer      string        // accumulated streaming text (chunks are deltas)
	inToolPhase       bool          // true after tool.call, reset on next chunk (new LLM iteration)
	stream            ChannelStream // per-run stream handle (replaces per-chat sync.Map in channel impls)
	thinkingBuffer    string        // accumulated thinking/reasoning text
	hasThinking       bool          // true if any thinking events received this iteration
	thinkingDone      bool          // true after first chunk arrives (reasoning→answer transition complete)
	tagParseSkipped   bool          // true after first chunk with no <think> tags (skip re-parsing)
}

// Manager manages all registered channels, handling their lifecycle
// and routing outbound messages to the correct channel.
type Manager struct {
	channels         map[string]Channel
	health           map[string]ChannelHealth
	bus              *bus.MessageBus
	runs             sync.Map // runID string → *RunContext
	dispatchTask     *asyncTask
	mu               sync.RWMutex
	contactCollector *store.ContactCollector
}

type asyncTask struct {
	cancel context.CancelFunc
}

// NewManager creates a new channel manager.
// Channels are registered externally via RegisterChannel.
func NewManager(msgBus *bus.MessageBus) *Manager {
	return &Manager{
		channels: make(map[string]Channel),
		health:   make(map[string]ChannelHealth),
		bus:      msgBus,
	}
}

// StartAll starts all registered channels and the outbound dispatch loop.
// The dispatcher is always started even when no channels exist yet,
// because channels may be loaded dynamically later via Reload().
func (m *Manager) StartAll(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Always start the outbound dispatcher — channels may be added later via Reload().
	dispatchCtx, cancel := context.WithCancel(ctx)
	m.dispatchTask = &asyncTask{cancel: cancel}
	go m.dispatchOutbound(dispatchCtx)

	if len(m.channels) == 0 {
		slog.Warn("no channels enabled")
		return nil
	}

	slog.Info("starting all channels")

	for name, channel := range m.channels {
		slog.Info("starting channel", "channel", name)
		if hc, ok := channel.(interface{ MarkStarting(string) }); ok {
			hc.MarkStarting("Starting")
		}
		m.syncChannelHealthLocked(name, channel)
		if err := channel.Start(ctx); err != nil {
			m.recordChannelStartFailureLocked(name, channel, "", err)
			slog.Error("failed to start channel", "channel", name, "error", err)
			continue
		}
		m.syncChannelHealthLocked(name, channel)
	}

	slog.Info("all channels started")
	return nil
}

// StopAll gracefully stops all channels and the outbound dispatch loop.
func (m *Manager) StopAll(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	slog.Info("stopping all channels")

	if m.dispatchTask != nil {
		m.dispatchTask.cancel()
		m.dispatchTask = nil
	}

	for name, channel := range m.channels {
		slog.Info("stopping channel", "channel", name)
		if err := channel.Stop(ctx); err != nil {
			m.recordHealthLocked(name, NewFailedChannelHealth("Failed to stop channel", err))
			slog.Error("error stopping channel", "channel", name, "error", err)
			continue
		}
		if hc, ok := channel.(interface{ MarkStopped(string) }); ok {
			hc.MarkStopped("Stopped")
		}
		m.syncChannelHealthLocked(name, channel)
	}

	slog.Info("all channels stopped")
	return nil
}

// GetChannel returns a channel by name.
func (m *Manager) GetChannel(name string) (Channel, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	channel, ok := m.channels[name]
	return channel, ok
}

// GetStatus returns the running status of all channels.
func (m *Manager) GetStatus() map[string]any {
	m.mu.RLock()
	defer m.mu.RUnlock()

	status := make(map[string]any, len(m.health)+len(m.channels))
	for name, snapshot := range m.health {
		status[name] = snapshot
	}
	for name, channel := range m.channels {
		status[name] = snapshotChannelHealth(channel)
	}
	return status
}

// GetEnabledChannels returns the names of all enabled channels.
func (m *Manager) GetEnabledChannels() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.channels))
	for name := range m.channels {
		names = append(names, name)
	}
	return names
}

// RegisterChannel adds a channel to the manager.
func (m *Manager) RegisterChannel(name string, channel Channel) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Propagate contact collector to channels that embed BaseChannel.
	if m.contactCollector != nil {
		if bc, ok := channel.(interface{ SetContactCollector(*store.ContactCollector) }); ok {
			bc.SetContactCollector(m.contactCollector)
		}
	}
	m.channels[name] = channel
	if hc, ok := channel.(interface{ MarkRegistered(string) }); ok {
		hc.MarkRegistered("Configured")
	}
	m.syncChannelHealthLocked(name, channel)
}

// RecordHealth stores runtime health for an instance, including failures before registration.
func (m *Manager) RecordHealth(name string, snapshot ChannelHealth) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.recordHealthLocked(name, snapshot)
}

// RecordFailure stores a classified failure snapshot for an instance.
func (m *Manager) RecordFailure(name, summary string, err error) {
	m.RecordHealth(name, NewFailedChannelHealth(summary, err))
}

// RecordFailureForType stores a classified failure snapshot for an instance before registration exists.
func (m *Manager) RecordFailureForType(name, channelType, summary string, err error) {
	m.RecordHealth(name, NewFailedChannelHealthForType(channelType, summary, err))
}

func (m *Manager) recordChannelStartFailure(name string, channel Channel, summary string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.recordChannelStartFailureLocked(name, channel, summary, err)
}

func (m *Manager) recordChannelStartFailureLocked(name string, channel Channel, summary string, err error) {
	info := ClassifyChannelError(err)
	if summary == "" {
		summary = info.Summary
	}

	current := snapshotChannelHealth(channel)
	if isFailureState(current.State) {
		if current.ChannelType == "" {
			current.ChannelType = channel.Type()
		}
		if current.Summary == "" {
			current.Summary = summary
		}
		if current.Detail == "" {
			current.Detail = info.Detail
		}
		if current.FailureKind == "" {
			current.FailureKind = info.Kind
		}
		m.recordHealthLocked(name, current)
		return
	}

	if hc, ok := channel.(interface {
		MarkFailed(string, string, ChannelFailureKind, bool)
	}); ok {
		hc.MarkFailed(summary, info.Detail, info.Kind, info.Retryable)
		m.syncChannelHealthLocked(name, channel)
		return
	}

	m.recordHealthLocked(name, NewChannelHealthForType(
		channel.Type(),
		ChannelHealthStateFailed,
		summary,
		info.Detail,
		info.Kind,
		info.Retryable,
	))
}

// SetContactCollector sets the contact collector for all current and future channels.
func (m *Manager) SetContactCollector(cc *store.ContactCollector) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.contactCollector = cc
	for _, ch := range m.channels {
		if bc, ok := ch.(interface{ SetContactCollector(*store.ContactCollector) }); ok {
			bc.SetContactCollector(cc)
		}
	}
}

// ChannelTypeForName returns the platform type for a channel instance name.
// Reads directly from the Channel.Type() method — no separate map needed.
func (m *Manager) ChannelTypeForName(name string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if ch, ok := m.channels[name]; ok {
		return ch.Type()
	}
	return ""
}

// ChannelTenantID returns the tenant UUID for a channel instance.
// Zero UUID means legacy/config-based channel (no tenant scope).
// Returns (tenantID, exists).
func (m *Manager) ChannelTenantID(channelName string) (uuid.UUID, bool) {
	m.mu.RLock()
	ch, ok := m.channels[channelName]
	m.mu.RUnlock()
	if !ok {
		return uuid.Nil, false
	}
	if tc, ok := ch.(interface{ TenantID() uuid.UUID }); ok {
		return tc.TenantID(), true
	}
	return uuid.Nil, true // legacy channel without tenant scope
}

// ListGroupMembers delegates to the channel's GroupMemberProvider if available.
func (m *Manager) ListGroupMembers(ctx context.Context, channelName, chatID string) ([]GroupMember, error) {
	m.mu.RLock()
	ch, ok := m.channels[channelName]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("channel %q not found", channelName)
	}
	gmp, ok := ch.(GroupMemberProvider)
	if !ok {
		return nil, fmt.Errorf("channel %q does not support listing group members", channelName)
	}
	return gmp.ListGroupMembers(ctx, chatID)
}

// UnregisterChannel removes a channel from the manager.
func (m *Manager) UnregisterChannel(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.channels, name)
	delete(m.health, name)
}

func (m *Manager) recordHealthLocked(name string, snapshot ChannelHealth) {
	prev := m.health[name]
	if snapshot.ChannelType == "" {
		switch {
		case prev.ChannelType != "":
			snapshot.ChannelType = prev.ChannelType
		case m.channels[name] != nil:
			snapshot.ChannelType = m.channels[name].Type()
		default:
			snapshot.ChannelType = name
		}
	}
	m.health[name] = mergeChannelHealth(prev, snapshot)
}

func (m *Manager) syncChannelHealthLocked(name string, channel Channel) {
	m.recordHealthLocked(name, snapshotChannelHealth(channel))
}

func snapshotChannelHealth(channel Channel) ChannelHealth {
	if reporter, ok := channel.(interface{ HealthSnapshot() ChannelHealth }); ok {
		snapshot := reporter.HealthSnapshot()
		snapshot.ChannelType = channel.Type()
		snapshot.Enabled = true
		snapshot.Running = channel.IsRunning()
		return snapshot
	}

	state := ChannelHealthStateStopped
	summary := "Stopped"
	if channel.IsRunning() {
		state = ChannelHealthStateHealthy
		summary = "Connected"
	}
	return ChannelHealth{
		ChannelType: channel.Type(),
		Enabled:     true,
		Running:     channel.IsRunning(),
		State:       state,
		Summary:     summary,
	}
}
