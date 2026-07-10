package dingtalk

import (
	"encoding/json"
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// dingtalkCreds maps channel_instances.credentials.
//
// client_id is the DingTalk app's AppKey and client_secret its AppSecret; the
// OpenAPI docs use both names interchangeably. We keep the OAuth-flavored names
// because that is what the app console shows and what the upstream connector's
// config uses.
type dingtalkCreds struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

// dingtalkInstanceConfig maps channel_instances.config.
//
// Field names are snake_case to match every other GoClaw channel, not the
// camelCase of the upstream connector's openclaw.plugin.json. The semantics are
// identical; only the casing differs, and nobody copies an OpenClaw plugin
// config into a GoClaw instance row.
//
// Deliberately not ported: the connector's accounts{} / defaultAccount
// multi-account map. One GoClaw channel instance carries one DingTalk app,
// which is what channel_instances already models — a second app is a second row.
type dingtalkInstanceConfig struct {
	Endpoint string `json:"endpoint,omitempty"`

	AllowFrom      []string `json:"allow_from,omitempty"`
	GroupAllowFrom []string `json:"group_allow_from,omitempty"`
	DMPolicy       string   `json:"dm_policy,omitempty"`
	GroupPolicy    string   `json:"group_policy,omitempty"`
	RequireMention *bool    `json:"require_mention,omitempty"`

	TextChunkLimit int `json:"text_chunk_limit,omitempty"`
	MediaMaxMB     int `json:"media_max_mb,omitempty"`
	HistoryLimit   int `json:"history_limit,omitempty"`

	GroupReplyMode    string `json:"group_reply_mode,omitempty"`
	GroupSessionScope string `json:"group_session_scope,omitempty"`
	Streaming         *bool  `json:"streaming,omitempty"`
	ReactionLevel     string `json:"reaction_level,omitempty"`

	ChatBehavior *config.ChatBehaviorConfig `json:"chat_behavior,omitempty"`
}

// resolve decodes the raw credential and config JSON into a Config.
func resolve(creds json.RawMessage, cfg json.RawMessage) (Config, error) {
	var c dingtalkCreds
	if len(creds) > 0 {
		if err := json.Unmarshal(creds, &c); err != nil {
			return Config{}, fmt.Errorf("decode dingtalk credentials: %w", err)
		}
	}
	if c.ClientID == "" || c.ClientSecret == "" {
		return Config{}, fmt.Errorf("dingtalk client_id and client_secret are required")
	}

	var ic dingtalkInstanceConfig
	if len(cfg) > 0 {
		if err := json.Unmarshal(cfg, &ic); err != nil {
			return Config{}, fmt.Errorf("decode dingtalk config: %w", err)
		}
	}

	return Config{
		ClientID:          c.ClientID,
		ClientSecret:      c.ClientSecret,
		Endpoint:          ic.Endpoint,
		AllowFrom:         ic.AllowFrom,
		GroupAllowFrom:    ic.GroupAllowFrom,
		DMPolicy:          ic.DMPolicy,
		GroupPolicy:       ic.GroupPolicy,
		RequireMention:    ic.RequireMention,
		TextChunkLimit:    ic.TextChunkLimit,
		MediaMaxMB:        ic.MediaMaxMB,
		HistoryLimit:      ic.HistoryLimit,
		GroupReplyMode:    ic.GroupReplyMode,
		GroupSessionScope: ic.GroupSessionScope,
		Streaming:         ic.Streaming,
		ReactionLevel:     ic.ReactionLevel,
		ChatBehavior:      ic.ChatBehavior,
	}, nil
}

// Factory creates a DingTalk channel from DB instance data, without persistent
// group history or speech-to-text.
func Factory(name string, creds json.RawMessage, cfg json.RawMessage,
	msgBus *bus.MessageBus, pairingSvc store.PairingStore) (channels.Channel, error) {
	return build(name, creds, cfg, msgBus, pairingSvc, nil, nil)
}

// FactoryWithPendingStore returns a ChannelFactory with persistent group history.
func FactoryWithPendingStore(pendingStore store.PendingMessageStore) channels.ChannelFactory {
	return FactoryWithPendingStoreAndAudio(pendingStore, nil)
}

// FactoryWithPendingStoreAndAudio returns a ChannelFactory with persistent group
// history and speech-to-text. This is the variant the gateway registers.
func FactoryWithPendingStoreAndAudio(pendingStore store.PendingMessageStore, audioMgr *audio.Manager) channels.ChannelFactory {
	return func(name string, creds json.RawMessage, cfg json.RawMessage,
		msgBus *bus.MessageBus, pairingSvc store.PairingStore) (channels.Channel, error) {
		return build(name, creds, cfg, msgBus, pairingSvc, pendingStore, audioMgr)
	}
}

func build(name string, creds json.RawMessage, cfg json.RawMessage,
	msgBus *bus.MessageBus, pairingSvc store.PairingStore,
	pendingStore store.PendingMessageStore, audioMgr *audio.Manager) (channels.Channel, error) {

	resolved, err := resolve(creds, cfg)
	if err != nil {
		return nil, err
	}

	ch, err := New(resolved, msgBus, pairingSvc, pendingStore, audioMgr)
	if err != nil {
		return nil, err
	}

	ch.SetName(name)
	return ch, nil
}
