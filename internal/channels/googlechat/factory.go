package googlechat

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// googleChatCreds maps the credentials JSON from the channel_instances table.
type googleChatCreds struct {
	ServiceAccountJSON json.RawMessage `json:"service_account_json"` // embedded SA key JSON
}

// googleChatInstanceConfig maps the non-secret config JSONB from the channel_instances table.
type googleChatInstanceConfig struct {
	Mode              string   `json:"mode,omitempty"`
	ProjectID         string   `json:"project_id,omitempty"`
	SubscriptionID    string   `json:"subscription_id,omitempty"`
	PullIntervalMs    int      `json:"pull_interval_ms,omitempty"`
	BotUser           string   `json:"bot_user,omitempty"`
	DMPolicy          string   `json:"dm_policy,omitempty"`
	GroupPolicy       string   `json:"group_policy,omitempty"`
	AllowFrom         []string `json:"allow_from,omitempty"`
	LongFormThreshold int      `json:"long_form_threshold,omitempty"`
	LongFormFormat    string   `json:"long_form_format,omitempty"`
	MediaMaxMB        int      `json:"media_max_mb,omitempty"`
	DrivePermission   string   `json:"drive_permission,omitempty"`
	BlockReply        *bool    `json:"block_reply,omitempty"`
}

// FactoryWithPendingStore returns a ChannelFactory that includes the pending message store.
func FactoryWithPendingStore(pendingStore store.PendingMessageStore) channels.ChannelFactory {
	return func(name string, creds json.RawMessage, cfg json.RawMessage,
		msgBus *bus.MessageBus, pairingSvc store.PairingStore) (channels.Channel, error) {
		return buildChannel(name, creds, cfg, msgBus, pendingStore)
	}
}

// Factory creates a Google Chat channel from DB instance data (no pending store).
func Factory(name string, creds json.RawMessage, cfg json.RawMessage,
	msgBus *bus.MessageBus, _ store.PairingStore) (channels.Channel, error) {
	return buildChannel(name, creds, cfg, msgBus, nil)
}

func buildChannel(name string, creds json.RawMessage, cfg json.RawMessage,
	msgBus *bus.MessageBus, pendingStore store.PendingMessageStore) (channels.Channel, error) {

	var c googleChatCreds
	if len(creds) > 0 {
		if err := json.Unmarshal(creds, &c); err != nil {
			return nil, fmt.Errorf("decode googlechat credentials: %w", err)
		}
	}

	var ic googleChatInstanceConfig
	if len(cfg) > 0 {
		if err := json.Unmarshal(cfg, &ic); err != nil {
			return nil, fmt.Errorf("decode googlechat config: %w", err)
		}
	}

	if len(c.ServiceAccountJSON) == 0 {
		return nil, fmt.Errorf("googlechat: service_account_json is required in credentials")
	}

	// Write SA JSON to a temp file for NewServiceAccountAuth (it reads from file path).
	saFile, err := writeTempSAFile(c.ServiceAccountJSON)
	if err != nil {
		return nil, fmt.Errorf("googlechat: write SA temp file: %w", err)
	}

	gcCfg := config.GoogleChatConfig{
		Enabled:            true,
		ServiceAccountFile: saFile,
		Mode:               ic.Mode,
		ProjectID:          ic.ProjectID,
		SubscriptionID:     ic.SubscriptionID,
		PullIntervalMs:     ic.PullIntervalMs,
		BotUser:            ic.BotUser,
		DMPolicy:           ic.DMPolicy,
		GroupPolicy:        ic.GroupPolicy,
		AllowFrom:          ic.AllowFrom,
		LongFormThreshold:  ic.LongFormThreshold,
		LongFormFormat:     ic.LongFormFormat,
		MediaMaxMB:         ic.MediaMaxMB,
		DrivePermission:    ic.DrivePermission,
		BlockReply:         ic.BlockReply,
	}

	// DB instances default to "allowlist" for groups.
	if gcCfg.GroupPolicy == "" {
		gcCfg.GroupPolicy = "allowlist"
	}

	ch, err := New(gcCfg, msgBus, pendingStore)
	if err != nil {
		return nil, err
	}

	ch.SetName(name)
	return ch, nil
}

// writeTempSAFile writes the SA JSON to a temp file and returns the path.
func writeTempSAFile(saJSON json.RawMessage) (string, error) {
	tmpPath := filepath.Join(os.TempDir(), "goclaw-sa-"+uuid.New().String()+".json")
	if err := os.WriteFile(tmpPath, saJSON, 0600); err != nil {
		return "", err
	}
	return tmpPath, nil
}
