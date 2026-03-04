package bus

import "context"

// InboundMessage represents a message received from a channel (Telegram, Discord, etc.)
type InboundMessage struct {
	Channel      string            `json:"channel"`
	SenderID     string            `json:"sender_id"`
	ChatID       string            `json:"chat_id"`
	Content      string            `json:"content"`
	Media        []string          `json:"media,omitempty"`
	SessionKey   string            `json:"session_key"`             // deprecated: gateway builds canonical key
	PeerKind     string            `json:"peer_kind,omitempty"`     // "direct" or "group" (used for session key)
	AgentID      string            `json:"agent_id,omitempty"`      // target agent (for multi-agent routing)
	UserID       string            `json:"user_id,omitempty"`       // external user ID for per-user scoping (memory, bootstrap)
	HistoryLimit int               `json:"history_limit,omitempty"` // max turns to keep in context (0=unlimited, from channel config)
	ToolAllow    []string          `json:"tool_allow,omitempty"`    // per-group tool allow list (nil = no restriction)
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// OutboundMessage represents a message to be sent to a channel.
type OutboundMessage struct {
	Channel  string            `json:"channel"`
	ChatID   string            `json:"chat_id"`
	Content  string            `json:"content"`
	Media    []MediaAttachment `json:"media,omitempty"`    // optional media attachments
	Metadata map[string]string `json:"metadata,omitempty"` // channel-specific metadata
}

// MediaAttachment represents a media file to be sent with a message.
type MediaAttachment struct {
	URL         string `json:"url"`                    // file path or URL
	ContentType string `json:"content_type,omitempty"` // MIME type (e.g. "image/jpeg", "video/mp4")
	Caption     string `json:"caption,omitempty"`      // optional caption for media
}

// Event represents a server-side event to broadcast to WebSocket clients.
type Event struct {
	Name    string      `json:"name"` // event name (e.g. "agent", "chat", "health")
	Payload interface{} `json:"payload,omitempty"`
}

// Cache invalidation kind constants.
const (
	CacheKindAgent            = "agent"
	CacheKindBootstrap        = "bootstrap"
	CacheKindSkills           = "skills"
	CacheKindCron             = "cron"
	CacheKindCustomTools      = "custom_tools"
	CacheKindChannelInstances = "channel_instances"
	CacheKindBuiltinTools     = "builtin_tools"
	CacheKindTeam             = "team"
	CacheKindUserWorkspace    = "user_workspace"
	CacheKindGroupFileWriters = "group_file_writers"
	CacheKindSkillGrants      = "skill_grants"
	CacheKindMCP              = "mcp"
)

// Topic constants for msgBus.Subscribe() / Broadcast().
const (
	TopicCacheBootstrap        = "cache:bootstrap"
	TopicCacheAgent            = "cache:agent"
	TopicCacheSkills           = "cache:skills"
	TopicCacheCron             = "cache:cron"
	TopicCacheCustomTools      = "cache:custom_tools"
	TopicCacheBuiltinTools     = "cache:builtin_tools"
	TopicCacheTeam             = "cache:team"
	TopicCacheUserWorkspace    = "cache:user_workspace"
	TopicCacheChannelInstances = "cache:channel_instances"
	TopicCacheGroupFileWriters = "cache:group_file_writers"
	TopicCacheSkillGrants      = "cache:skill_grants"
	TopicCacheMCP              = "cache:mcp"
	TopicChannelStreaming      = "channel-streaming"
	TopicConfigChanged         = "config:changed"
)

// CacheInvalidatePayload signals cache layers to evict stale entries.
// Used with protocol.EventCacheInvalidate events.
type CacheInvalidatePayload struct {
	Kind string `json:"kind"` // CacheKind* constants
	Key  string `json:"key"`  // agent_key, agent_id, etc. Empty = invalidate all
}

// MessageHandler handles an inbound message from a specific channel.
type MessageHandler func(InboundMessage) error

// EventHandler handles a broadcast event.
type EventHandler func(Event)

// EventPublisher abstracts event broadcast + subscription.
// Used by gateway server and agents to decouple from concrete MessageBus.
type EventPublisher interface {
	Subscribe(id string, handler EventHandler)
	Unsubscribe(id string)
	Broadcast(event Event)
}

// MessageRouter abstracts inbound/outbound message routing between channels and the agent runtime.
type MessageRouter interface {
	PublishInbound(msg InboundMessage)
	ConsumeInbound(ctx context.Context) (InboundMessage, bool)
	PublishOutbound(msg OutboundMessage)
	SubscribeOutbound(ctx context.Context) (OutboundMessage, bool)
}
