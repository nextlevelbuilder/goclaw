package feishu

import (
	"encoding/json"
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// feishuCreds maps the credentials JSON from the channel_instances table.
type feishuCreds struct {
	AppID             string `json:"app_id"`
	AppSecret         string `json:"app_secret"`
	EncryptKey        string `json:"encrypt_key,omitempty"`
	VerificationToken string `json:"verification_token,omitempty"`
}

// feishuInstanceConfig maps the non-secret config JSONB from the channel_instances table.
type feishuInstanceConfig struct {
	Domain           string   `json:"domain,omitempty"`
	ConnectionMode   string   `json:"connection_mode,omitempty"`
	WebhookPort      int      `json:"webhook_port,omitempty"`
	WebhookPath      string   `json:"webhook_path,omitempty"`
	AllowFrom        []string `json:"allow_from,omitempty"`
	DMPolicy         string   `json:"dm_policy,omitempty"`
	GroupPolicy      string   `json:"group_policy,omitempty"`
	GroupAllowFrom   []string `json:"group_allow_from,omitempty"`
	RequireMention   *bool    `json:"require_mention,omitempty"`
	TopicSessionMode string   `json:"topic_session_mode,omitempty"`
	TextChunkLimit   int      `json:"text_chunk_limit,omitempty"`
	MediaMaxMB       int      `json:"media_max_mb,omitempty"`
	RenderMode       string   `json:"render_mode,omitempty"`
	Streaming        *bool    `json:"streaming,omitempty"`
	ReactionLevel    string   `json:"reaction_level,omitempty"`
	HistoryLimit     int      `json:"history_limit,omitempty"`
	BlockReply       *bool    `json:"block_reply,omitempty"`
}

// Factory creates a Feishu/Lark channel from DB instance data.
func Factory(name string, creds json.RawMessage, cfg json.RawMessage,
	msgBus *bus.MessageBus, pairingSvc store.PairingStore) (channels.Channel, error) {

	var c feishuCreds
	if len(creds) > 0 {
		if err := json.Unmarshal(creds, &c); err != nil {
			return nil, fmt.Errorf("decode feishu credentials: %w", err)
		}
	}
	if c.AppID == "" || c.AppSecret == "" {
		return nil, fmt.Errorf("feishu app_id and app_secret are required")
	}

	var ic feishuInstanceConfig
	if len(cfg) > 0 {
		if err := json.Unmarshal(cfg, &ic); err != nil {
			return nil, fmt.Errorf("decode feishu config: %w", err)
		}
	}

	fsCfg := config.FeishuConfig{
		Enabled:           true,
		AppID:             c.AppID,
		AppSecret:         c.AppSecret,
		EncryptKey:        c.EncryptKey,
		VerificationToken: c.VerificationToken,
		Domain:            ic.Domain,
		ConnectionMode:    ic.ConnectionMode,
		WebhookPort:       ic.WebhookPort,
		WebhookPath:       ic.WebhookPath,
		AllowFrom:         ic.AllowFrom,
		DMPolicy:          ic.DMPolicy,
		GroupPolicy:       ic.GroupPolicy,
		GroupAllowFrom:    ic.GroupAllowFrom,
		RequireMention:    ic.RequireMention,
		TopicSessionMode:  ic.TopicSessionMode,
		TextChunkLimit:    ic.TextChunkLimit,
		MediaMaxMB:        ic.MediaMaxMB,
		RenderMode:        ic.RenderMode,
		Streaming:         ic.Streaming,
		ReactionLevel:     ic.ReactionLevel,
		HistoryLimit:      ic.HistoryLimit,
		BlockReply:        ic.BlockReply,
	}

	// DB instances default to "pairing" for groups (secure by default).
	if fsCfg.GroupPolicy == "" {
		fsCfg.GroupPolicy = "pairing"
	}

	ch, err := New(fsCfg, msgBus, pairingSvc)
	if err != nil {
		return nil, err
	}

	ch.SetName(name)
	return ch, nil
}
