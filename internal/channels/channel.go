// Package channels provides the channel abstraction layer for multi-platform messaging.
// Channels connect external platforms (Telegram, Discord, Slack, etc.) to the agent runtime
// via the message bus.
//
// Adapted from PicoClaw's pkg/channels with GoClaw-specific additions:
// - DM/Group policies (pairing, allowlist, open, disabled)
// - Mention gating for group chats
// - Rich MsgContext metadata
package channels

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// InternalChannels are system channels excluded from outbound dispatch.
// "browser" uses WebSocket directly — no outbound channel routing needed.
var InternalChannels = map[string]bool{
	"cli":      true,
	"system":   true,
	"subagent": true,
	"browser":  true,
	"ws":       true, // WebSocket — responses delivered via events/RPC, not outbound dispatch
}

// IsInternalChannel checks if a channel name is internal.
func IsInternalChannel(name string) bool {
	return InternalChannels[name]
}

// DMPolicy controls how DMs from unknown senders are handled.
type DMPolicy string

const (
	DMPolicyPairing   DMPolicy = "pairing"   // Require pairing code
	DMPolicyAllowlist DMPolicy = "allowlist" // Only whitelisted senders
	DMPolicyOpen      DMPolicy = "open"      // Accept all
	DMPolicyDisabled  DMPolicy = "disabled"  // Reject all DMs
)

// GroupPolicy controls how group messages are handled.
type GroupPolicy string

const (
	GroupPolicyOpen      GroupPolicy = "open"      // Accept all groups
	GroupPolicyAllowlist GroupPolicy = "allowlist" // Only whitelisted groups
	GroupPolicyDisabled  GroupPolicy = "disabled"  // No group messages
)

// Channel type constants used across channel packages and gateway wiring.
const (
	TypeTelegram     = "telegram"
	TypeDiscord      = "discord"
	TypeSlack        = "slack"
	TypeFeishu       = "feishu"
	TypeWhatsApp     = "whatsapp"
	TypeZaloOA       = "zalo_oa"
	TypeZaloPersonal = "zalo_personal"
	TypeGoogleChat   = "google_chat"
)

// Channel defines the interface that all channel implementations must satisfy.
type Channel interface {
	// Name returns the channel instance name (e.g., "telegram", "discord", "slack").
	Name() string

	// Type returns the platform type (e.g., "telegram", "zalo_personal").
	// For config-based channels this equals Name(); for DB instances it may differ.
	Type() string

	// Start begins listening for messages. Should be non-blocking after setup.
	Start(ctx context.Context) error

	// Stop gracefully shuts down the channel.
	Stop(ctx context.Context) error

	// Send delivers an outbound message to the channel.
	Send(ctx context.Context, msg bus.OutboundMessage) error

	// IsRunning returns whether the channel is actively processing messages.
	IsRunning() bool

	// IsAllowed checks if a sender is permitted by the channel's allowlist.
	IsAllowed(senderID string) bool
}

// StreamingChannel extends Channel with real-time streaming preview support.
// Channels that implement this interface can show incremental response updates
// (e.g., editing a Telegram message as chunks arrive) instead of waiting for the full response.
type StreamingChannel interface {
	Channel
	// StreamEnabled reports whether the channel currently wants LLM streaming.
	// When false the agent loop uses non-streaming Chat() instead of ChatStream(),
	// which gives more accurate token usage from providers that don't support
	// stream_options (e.g. MiniMax). The channel still implements the interface
	// so it can be toggled at runtime via config.
	//
	// isGroup indicates whether this is a group chat (true) or DM (false).
	// Channels may choose to always stream for DMs while gating group streaming
	// behind config (e.g. Telegram uses sendMessageDraft for DMs).
	StreamEnabled(isGroup bool) bool
	// CreateStream creates a new per-run streaming handle for the given chatID.
	// The returned ChannelStream is stored on RunContext so each concurrent run
	// gets its own stream — eliminates the chatID-keyed sync.Map collision bug.
	// firstStream: true for the first stream in a run (may become reasoning lane —
	// must use message transport so it persists as a real message). false for
	// subsequent streams (answer lane — may use draft transport for stealth preview).
	CreateStream(ctx context.Context, chatID string, firstStream bool) (ChannelStream, error)
	// FinalizeStream is called after the stream has been stopped to hand off
	// the stream's messageID (if any) back to the channel's placeholder map
	// so that Send() can edit it with the final formatted response.
	FinalizeStream(ctx context.Context, chatID string, stream ChannelStream)
	// ReasoningStreamEnabled returns whether reasoning should be shown as a
	// separate message. Default: true. Channels that don't support lanes can
	// return false to skip reasoning routing.
	ReasoningStreamEnabled() bool
}

// BlockReplyChannel is optionally implemented by channels that override
// the gateway-level block_reply setting. Returns nil to inherit the gateway default.
type BlockReplyChannel interface {
	BlockReplyEnabled() *bool
}

// WebhookChannel extends Channel with an HTTP handler that can be mounted
// on the main gateway mux instead of starting a separate HTTP server.
// This allows webhook-based channels (e.g. Feishu/Lark) to share the main
// server port, avoiding the need to expose additional ports in Docker.
type WebhookChannel interface {
	Channel
	// WebhookHandler returns the HTTP handler and the path it should be mounted on.
	// Returns ("", nil) if the channel doesn't use webhook mode.
	WebhookHandler() (path string, handler http.Handler)
}

// ReactionChannel extends Channel with status reaction support.
// Channels that implement this interface can show emoji reactions on user messages
// to indicate agent status (thinking, tool call, done, error, stall).
// messageID is a string to support platforms with non-integer IDs (e.g., Feishu "om_xxx").
type ReactionChannel interface {
	Channel
	OnReactionEvent(ctx context.Context, chatID string, messageID string, status string) error
	ClearReaction(ctx context.Context, chatID string, messageID string) error
}

// BaseChannel provides shared functionality for all channel implementations.
// Channel implementations should embed this struct.
type BaseChannel struct {
	name             string
	channelType      string // platform type; defaults to name if unset
	bus              *bus.MessageBus
	running          bool
	stateMu          sync.RWMutex
	health           ChannelHealth
	allowList        []string
	agentID          string                  // for DB instances: routes to specific agent (empty = use resolveAgentRoute)
	tenantID         uuid.UUID               // for DB instances: tenant scope (zero = master tenant fallback)
	contactCollector *store.ContactCollector // optional: auto-collect contacts from channel messages
}

// NewBaseChannel creates a new BaseChannel with the given parameters.
func NewBaseChannel(name string, msgBus *bus.MessageBus, allowList []string) *BaseChannel {
	return &BaseChannel{
		name:      name,
		bus:       msgBus,
		health:    NewChannelHealthForType(name, ChannelHealthStateRegistered, "Configured", "", ChannelFailureKindUnknown, false),
		allowList: allowList,
	}
}

// Name returns the channel instance name.
func (c *BaseChannel) Name() string { return c.name }

// Type returns the platform type. Falls back to name if unset (config-based channels).
func (c *BaseChannel) Type() string {
	if c.channelType != "" {
		return c.channelType
	}
	return c.name
}

// SetName overrides the channel name (used by InstanceLoader for DB instances).
func (c *BaseChannel) SetName(name string) { c.name = name }

// SetType sets the platform type (used by InstanceLoader for DB instances).
func (c *BaseChannel) SetType(t string) { c.channelType = t }

// AgentID returns the explicit agent ID for this channel (empty = use resolveAgentRoute).
func (c *BaseChannel) AgentID() string { return c.agentID }

// SetAgentID sets the explicit agent ID for routing (used by InstanceLoader for DB instances).
func (c *BaseChannel) SetAgentID(id string) { c.agentID = id }

// TenantID returns the tenant UUID for this channel (zero = master tenant fallback).
func (c *BaseChannel) TenantID() uuid.UUID { return c.tenantID }

// SetTenantID sets the tenant scope (used by InstanceLoader for DB instances).
func (c *BaseChannel) SetTenantID(id uuid.UUID) { c.tenantID = id }

// SetContactCollector sets the contact collector for auto-collecting contacts from messages.
func (c *BaseChannel) SetContactCollector(cc *store.ContactCollector) { c.contactCollector = cc }

// ContactCollector returns the contact collector (may be nil).
func (c *BaseChannel) ContactCollector() *store.ContactCollector { return c.contactCollector }

// IsRunning returns whether the channel is running.
func (c *BaseChannel) IsRunning() bool {
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()
	return c.running
}

// SetRunning updates the running state.
func (c *BaseChannel) SetRunning(running bool) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()

	next := c.health
	next.ChannelType = c.Type()
	next.Running = running
	switch {
	case running && (next.State == "" ||
		next.State == ChannelHealthStateRegistered ||
		next.State == ChannelHealthStateStarting ||
		next.State == ChannelHealthStateStopped):
		next.State = ChannelHealthStateHealthy
		if next.Summary == "" ||
			next.Summary == "Configured" ||
			next.Summary == "Starting" ||
			next.Summary == "Stopped" {
			next.Summary = "Connected"
		}
		next.Detail = ""
		next.FailureKind = ChannelFailureKindUnknown
		next.Retryable = false
		next.CheckedAt = time.Now().UTC()
	case !running && next.State == ChannelHealthStateHealthy:
		next.State = ChannelHealthStateStopped
		next.Summary = "Stopped"
		next.Detail = ""
		next.FailureKind = ChannelFailureKindUnknown
		next.Retryable = false
		next.CheckedAt = time.Now().UTC()
	default:
		c.running = running
		c.health.Running = running
		return
	}

	next = mergeChannelHealth(c.health, next)
	next.Running = running
	c.running = running
	c.health = next
}

// HealthSnapshot returns the current runtime health snapshot for the channel.
func (c *BaseChannel) HealthSnapshot() ChannelHealth {
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()

	snapshot := c.health
	snapshot.ChannelType = c.Type()
	snapshot.Enabled = true
	snapshot.Running = c.running
	return snapshot
}

// MarkRegistered records that the channel was configured and registered successfully.
func (c *BaseChannel) MarkRegistered(summary string) {
	if summary == "" {
		summary = "Configured"
	}
	c.setHealth(NewChannelHealth(ChannelHealthStateRegistered, summary, "", ChannelFailureKindUnknown, false))
}

// MarkStarting records that the channel is in startup validation / connection setup.
func (c *BaseChannel) MarkStarting(summary string) {
	if summary == "" {
		summary = "Starting"
	}
	c.setHealth(NewChannelHealth(ChannelHealthStateStarting, summary, "", ChannelFailureKindUnknown, true))
}

// MarkHealthy records a healthy connected state.
func (c *BaseChannel) MarkHealthy(summary string) {
	if summary == "" {
		summary = "Connected"
	}
	c.setHealth(NewChannelHealth(ChannelHealthStateHealthy, summary, "", ChannelFailureKindUnknown, false))
}

// MarkDegraded records a non-fatal warning state.
func (c *BaseChannel) MarkDegraded(summary, detail string, kind ChannelFailureKind, retryable bool) {
	if summary == "" {
		summary = "Running with warnings"
	}
	c.setHealth(NewChannelHealth(ChannelHealthStateDegraded, summary, detail, kind, retryable))
}

// MarkFailed records a startup or runtime failure.
func (c *BaseChannel) MarkFailed(summary, detail string, kind ChannelFailureKind, retryable bool) {
	if summary == "" {
		summary = "Channel failed"
	}
	c.setHealth(NewChannelHealth(ChannelHealthStateFailed, summary, detail, kind, retryable))
}

// MarkStopped records a cleanly stopped state.
func (c *BaseChannel) MarkStopped(summary string) {
	if summary == "" {
		summary = "Stopped"
	}
	c.setHealth(NewChannelHealth(ChannelHealthStateStopped, summary, "", ChannelFailureKindUnknown, false))
}

func (c *BaseChannel) setHealth(snapshot ChannelHealth) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()

	snapshot.ChannelType = c.Type()
	snapshot = mergeChannelHealth(c.health, snapshot)
	snapshot.Running = snapshot.State == ChannelHealthStateHealthy || snapshot.State == ChannelHealthStateDegraded
	c.running = snapshot.Running
	c.health = snapshot
}

// Bus returns the message bus reference.
func (c *BaseChannel) Bus() *bus.MessageBus { return c.bus }

// HasAllowList returns true if an allowlist is configured (non-empty).
func (c *BaseChannel) HasAllowList() bool { return len(c.allowList) > 0 }

// IsAllowed checks if a sender is permitted by the allowlist.
// Supports compound senderID format: "123456|username".
// Empty allowlist means all senders are allowed.
func (c *BaseChannel) IsAllowed(senderID string) bool {
	if len(c.allowList) == 0 {
		return true
	}

	// Extract parts from compound senderID like "123456|username"
	idPart := senderID
	userPart := ""
	if idx := strings.Index(senderID, "|"); idx > 0 {
		idPart = senderID[:idx]
		userPart = senderID[idx+1:]
	}

	for _, allowed := range c.allowList {
		// Strip leading "@" from allowed value for username matching
		trimmed := strings.TrimPrefix(allowed, "@")
		allowedID := trimmed
		allowedUser := ""
		if idx := strings.Index(trimmed, "|"); idx > 0 {
			allowedID = trimmed[:idx]
			allowedUser = trimmed[idx+1:]
		}

		// Support either side using "id|username" compound form.
		if senderID == allowed ||
			idPart == allowed ||
			senderID == trimmed ||
			idPart == trimmed ||
			idPart == allowedID ||
			(allowedUser != "" && senderID == allowedUser) ||
			(userPart != "" && (userPart == allowed || userPart == trimmed || userPart == allowedUser)) {
			return true
		}
	}

	return false
}

// CheckPolicy evaluates DM/Group policy for a message.
// Returns true if the message should be accepted, false if rejected.
// peerKind is "direct" or "group".
// dmPolicy/groupPolicy: "open" (default), "allowlist", "disabled".
func (c *BaseChannel) CheckPolicy(peerKind, dmPolicy, groupPolicy, senderID string) bool {
	policy := dmPolicy
	if peerKind == "group" {
		policy = groupPolicy
	}
	if policy == "" {
		policy = "open" // default for non-Telegram channels
	}

	switch policy {
	case "disabled":
		return false
	case "allowlist":
		return c.IsAllowed(senderID)
	case "pairing":
		// Channels with pairing handle this before CheckPolicy.
		// If we reach here, no pairing service → still allow if in allowlist.
		return c.IsAllowed(senderID)
	default: // "open"
		return true
	}
}

// ValidatePolicy logs warnings for common policy misconfigurations.
// Should be called during channel initialization.
func (c *BaseChannel) ValidatePolicy(dmPolicy, groupPolicy string) {
	if dmPolicy == "allowlist" && !c.HasAllowList() {
		slog.Warn("channel policy misconfiguration: dmPolicy=allowlist but allowFrom is empty — all DMs will be rejected",
			"channel", c.name)
	}
	if groupPolicy == "allowlist" && !c.HasAllowList() {
		slog.Warn("channel policy misconfiguration: groupPolicy=allowlist but allowFrom is empty — all group messages will be rejected",
			"channel", c.name)
	}
}

// HandleMessage creates an InboundMessage and publishes it to the bus.
// This is the standard way for channels to forward received messages.
// peerKind should be "direct" or "group" (see sessions.PeerDirect, sessions.PeerGroup).
func (c *BaseChannel) HandleMessage(senderID, chatID, content string, media []string, metadata map[string]string, peerKind string) {
	// For DMs, enforce the allowlist as a safety net.
	// For group messages, skip this check — group access is already enforced
	// by the channel-specific group policy (checkGroupPolicy / CheckPolicy).
	// Re-checking the sender here would incorrectly block users who are not
	// individually listed but are in an allowed (or open-policy) group.
	if peerKind != "group" && !c.IsAllowed(senderID) {
		return
	}

	// Derive userID from senderID: strip "|username" suffix if present (legacy Slack compound format).
	// All channels now pass plain senderID; kept for backward compat with stored compound IDs.
	userID := senderID
	if idx := strings.IndexByte(senderID, '|'); idx > 0 {
		userID = senderID[:idx]
	}

	// Convert string paths to MediaFile (for channels that haven't been updated yet).
	var mediaFiles []bus.MediaFile
	for _, p := range media {
		mediaFiles = append(mediaFiles, bus.MediaFile{Path: p})
	}

	msg := bus.InboundMessage{
		Channel:  c.name,
		SenderID: senderID,
		ChatID:   chatID,
		Content:  content,
		Media:    mediaFiles,
		PeerKind: peerKind,
		UserID:   userID,
		Metadata: metadata,
		TenantID: c.tenantID,
		AgentID:  c.agentID,
	}

	c.bus.PublishInbound(msg)
}

// GroupMember represents a member of a group chat.
type GroupMember struct {
	MemberID string `json:"member_id"`
	Name     string `json:"name"`
}

// GroupMemberProvider is optionally implemented by channels that can list group members.
type GroupMemberProvider interface {
	ListGroupMembers(ctx context.Context, chatID string) ([]GroupMember, error)
}

// PendingCompactable is optionally implemented by channels that have a PendingHistory
// supporting LLM-based compaction. InstanceLoader uses this to wire compaction config
// after channel creation.
type PendingCompactable interface {
	SetPendingCompaction(cfg *CompactionConfig)
}

// Truncate shortens a string to maxLen, appending "..." if truncated.
func Truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
