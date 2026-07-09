package dingtalk

import (
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/config"
)

// Defaults applied when the instance config leaves a field unset.
const (
	// defaultTextChunkLimit matches both the upstream connector
	// (reply-dispatcher.ts) and Feishu's defaultTextChunkLimit.
	defaultTextChunkLimit = 4000

	// defaultMediaMaxMB is DingTalk's own cap for video/file uploads; larger
	// payloads require the chunked upload transaction (Phase 5).
	defaultMediaMaxMB = 20
)

// Group reply modes. "aicard" streams the reply into a DingTalk AI Card;
// "text" and "markdown" send a discrete message via the session webhook.
const (
	GroupReplyModeAICard   = "aicard"
	GroupReplyModeText     = "text"
	GroupReplyModeMarkdown = "markdown"
)

// Group session scopes. "group" gives one session per conversation;
// "group_sender" isolates each sender within a conversation.
const (
	GroupSessionScopeGroup       = "group"
	GroupSessionScopeGroupSender = "group_sender"
)

// Config is the resolved configuration for one DingTalk channel instance.
//
// DingTalk is a DB-instance-only channel: unlike Feishu there is no
// config.DingtalkConfig counterpart, because there is no config-file setup
// path. Credentials arrive from channel_instances.credentials (encrypted at
// rest) and everything else from channel_instances.config.
type Config struct {
	// Credentials. ClientID is the app's AppKey, ClientSecret its AppSecret.
	ClientID     string
	ClientSecret string

	// Endpoint overrides the Stream-mode gateway host. Empty means the SDK
	// default (https://api.dingtalk.com).
	Endpoint string

	// Policies.
	AllowFrom      []string
	GroupAllowFrom []string
	DMPolicy       string
	GroupPolicy    string
	RequireMention *bool

	// Rendering and limits.
	TextChunkLimit int
	MediaMaxMB     int
	HistoryLimit   int

	// Reply behavior.
	GroupReplyMode    string
	GroupSessionScope string
	Streaming         *bool
	AsyncMode         *bool
	AckText           string

	ChatBehavior *config.ChatBehaviorConfig
}

// RequireMentionOrDefault reports whether an @mention is required in groups.
// Unset means true: a bot that answers every group message by default would be
// a surprise, and it is what the upstream connector's schema promises.
func (c Config) RequireMentionOrDefault() bool {
	if c.RequireMention == nil {
		return true
	}
	return *c.RequireMention
}

// StreamingOrDefault reports whether AI Card streaming is permitted at all.
// Unset means true, matching the connector's `config.streaming !== false`.
func (c Config) StreamingOrDefault() bool {
	if c.Streaming == nil {
		return true
	}
	return *c.Streaming
}

// AsyncModeOrDefault reports whether the channel acks first and pushes the
// result later. Unset means false.
func (c Config) AsyncModeOrDefault() bool {
	return c.AsyncMode != nil && *c.AsyncMode
}

// applyDefaults fills unset fields. DB instances default to "pairing" for both
// DMs and groups: an instance is reachable by anyone who knows the bot, so it
// must be secure by default. This mirrors Feishu's factory.
func (c *Config) applyDefaults() {
	if c.DMPolicy == "" {
		c.DMPolicy = "pairing"
	}
	if c.GroupPolicy == "" {
		c.GroupPolicy = "pairing"
	}
	if c.TextChunkLimit <= 0 {
		c.TextChunkLimit = defaultTextChunkLimit
	}
	if c.MediaMaxMB <= 0 {
		c.MediaMaxMB = defaultMediaMaxMB
	}
	if c.GroupReplyMode == "" {
		c.GroupReplyMode = GroupReplyModeAICard
	}
	if c.GroupSessionScope == "" {
		c.GroupSessionScope = GroupSessionScopeGroup
	}
}

// validate rejects a config that would fail confusingly at runtime.
//
// The enums are checked here rather than at first use so that a typo in
// group_reply_mode surfaces when the operator saves the instance, not hours
// later when the first group message arrives and silently takes the wrong
// branch.
func (c Config) validate() error {
	if c.ClientID == "" || c.ClientSecret == "" {
		return fmt.Errorf("dingtalk client_id and client_secret are required")
	}

	switch c.GroupReplyMode {
	case GroupReplyModeAICard, GroupReplyModeText, GroupReplyModeMarkdown:
	default:
		return fmt.Errorf("dingtalk group_reply_mode %q: want one of %q, %q, %q",
			c.GroupReplyMode, GroupReplyModeAICard, GroupReplyModeText, GroupReplyModeMarkdown)
	}

	switch c.GroupSessionScope {
	case GroupSessionScopeGroup, GroupSessionScopeGroupSender:
	default:
		return fmt.Errorf("dingtalk group_session_scope %q: want %q or %q",
			c.GroupSessionScope, GroupSessionScopeGroup, GroupSessionScopeGroupSender)
	}

	return nil
}
