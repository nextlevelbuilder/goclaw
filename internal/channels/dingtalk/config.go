package dingtalk

import (
	"fmt"
	"slices"
	"strings"
	"time"

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

// Reaction levels. DingTalk exposes exactly one usable reaction (the thinking
// face), so unlike Feishu there is no "minimal" tier to degrade to.
const (
	ReactionLevelOff = "off"
	ReactionLevelOn  = "on"
)

// Valid policy values, mirroring BaseChannel.CheckDMPolicy (channel.go:358) and
// CheckGroupPolicy (:395). Both fall through to their most permissive branch on
// an unrecognized value, so a typo would silently open the bot to everyone.
var (
	validDMPolicies    = []string{"pairing", "open", "allowlist", "disabled"}
	validGroupPolicies = []string{"pairing", "open", "allowlist", "disabled"}
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

	// ReactionLevel controls the 🤔 reaction posted on the user's message while
	// the agent works. "on" (default) or "off".
	ReactionLevel string

	// CardUpdateIntervalMS is how often a streaming card is repainted. Raising it
	// cuts billed card API calls without making the typing look chunky — the
	// DingTalk client animates between frames.
	CardUpdateIntervalMS int

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

// cardUpdateInterval is the repaint cadence, clamped to a sane floor.
func (c Config) cardUpdateInterval() time.Duration {
	if c.CardUpdateIntervalMS <= 0 {
		return defaultCardUpdateInterval
	}
	return max(time.Duration(c.CardUpdateIntervalMS)*time.Millisecond, minCardUpdateInterval)
}

// ReactionsEnabled reports whether to post the thinking reaction.
func (c Config) ReactionsEnabled() bool { return c.ReactionLevel != ReactionLevelOff }

// StreamingOrDefault reports whether AI Card streaming is permitted at all.
// Unset means true, matching the connector's `config.streaming !== false`.
func (c Config) StreamingOrDefault() bool {
	if c.Streaming == nil {
		return true
	}
	return *c.Streaming
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
	if c.ReactionLevel == "" {
		c.ReactionLevel = ReactionLevelOn
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

	if err := requireOneOf("dm_policy", c.DMPolicy, validDMPolicies); err != nil {
		return err
	}
	if err := requireOneOf("group_policy", c.GroupPolicy, validGroupPolicies); err != nil {
		return err
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

	switch c.ReactionLevel {
	case ReactionLevelOn, ReactionLevelOff:
	default:
		return fmt.Errorf("dingtalk reaction_level %q: want %q or %q",
			c.ReactionLevel, ReactionLevelOn, ReactionLevelOff)
	}

	if c.CardUpdateIntervalMS < 0 {
		return fmt.Errorf("dingtalk card_update_interval_ms %d: must not be negative", c.CardUpdateIntervalMS)
	}

	return nil
}

// requireOneOf rejects a value outside the allowed set.
//
// This matters more than it looks: BaseChannel's policy switches treat an
// unknown value as their default branch, which is the *permissive* one. Without
// this check, `group_policy: "opne"` would make the bot answer every group
// message from anyone, with nothing in the logs to say why.
func requireOneOf(field, value string, allowed []string) error {
	if slices.Contains(allowed, value) {
		return nil
	}
	return fmt.Errorf("dingtalk %s %q: want one of %s", field, value, strings.Join(allowed, ", "))
}
