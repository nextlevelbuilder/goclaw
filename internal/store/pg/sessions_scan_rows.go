package pg

import (
	"encoding/json"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// sessionListRow is an sqlx scan struct for the List query (SELECT session_key, messages, ...).
// messages column is scanned as raw JSON then decoded post-scan.
type sessionListRow struct {
	Key      string    `db:"session_key"`
	MsgsJSON []byte    `db:"messages"`
	Created  time.Time `db:"created_at"`
	Updated  time.Time `db:"updated_at"`
	Label    *string   `db:"label"`
	Channel  *string   `db:"channel"`
	UserID   *string   `db:"user_id"`
	MetaJSON []byte    `db:"metadata"`
}

// toSessionInfo converts a sessionListRow to store.SessionInfo.
// msgCount is provided externally since List decodes messages to count them.
func (r *sessionListRow) toSessionInfo(msgCount int) store.SessionInfo {
	var meta map[string]string
	if len(r.MetaJSON) > 0 {
		json.Unmarshal(r.MetaJSON, &meta) //nolint:errcheck
	}
	return store.SessionInfo{
		Key:          r.Key,
		MessageCount: msgCount,
		Created:      r.Created,
		Updated:      r.Updated,
		Label:        derefStr(r.Label),
		Channel:      derefStr(r.Channel),
		UserID:       derefStr(r.UserID),
		Metadata:     meta,
	}
}

// sessionPagedRow is an sqlx scan struct for ListPaged (uses jsonb_array_length, not full messages).
type sessionPagedRow struct {
	Key      string    `db:"session_key"`
	MsgCount int       `db:"message_count"`
	Created  time.Time `db:"created_at"`
	Updated  time.Time `db:"updated_at"`
	Label    *string   `db:"label"`
	Channel  *string   `db:"channel"`
	UserID   *string   `db:"user_id"`
	MetaJSON []byte    `db:"metadata"`
}

// toSessionInfo converts a sessionPagedRow to store.SessionInfo.
func (r *sessionPagedRow) toSessionInfo() store.SessionInfo {
	var meta map[string]string
	if len(r.MetaJSON) > 0 {
		json.Unmarshal(r.MetaJSON, &meta) //nolint:errcheck
	}
	return store.SessionInfo{
		Key:          r.Key,
		MessageCount: r.MsgCount,
		Created:      r.Created,
		Updated:      r.Updated,
		Label:        derefStr(r.Label),
		Channel:      derefStr(r.Channel),
		UserID:       derefStr(r.UserID),
		Metadata:     meta,
	}
}

// sessionRichRow is an sqlx scan struct for ListPagedRich (includes model, tokens, agent name, computed fields).
type sessionRichRow struct {
	Key             string    `db:"session_key"`
	MsgCount        int       `db:"message_count"`
	Created         time.Time `db:"created_at"`
	Updated         time.Time `db:"updated_at"`
	Label           *string   `db:"label"`
	Channel         *string   `db:"channel"`
	UserID          *string   `db:"user_id"`
	MetaJSON        []byte    `db:"metadata"`
	Model           *string   `db:"model"`
	Provider        *string   `db:"provider"`
	InputTokens     int64     `db:"input_tokens"`
	OutputTokens    int64     `db:"output_tokens"`
	AgentName       string    `db:"agent_name"`
	EstimatedTokens int       `db:"estimated_tokens"`
	ContextWindow   int       `db:"context_window"`
	CompactionCount int       `db:"compaction_count"`
}

// toSessionInfoRich converts a sessionRichRow to store.SessionInfoRich.
func (r *sessionRichRow) toSessionInfoRich() store.SessionInfoRich {
	var meta map[string]string
	if len(r.MetaJSON) > 0 {
		json.Unmarshal(r.MetaJSON, &meta) //nolint:errcheck
	}
	return store.SessionInfoRich{
		SessionInfo: store.SessionInfo{
			Key:          r.Key,
			MessageCount: r.MsgCount,
			Created:      r.Created,
			Updated:      r.Updated,
			Label:        derefStr(r.Label),
			Channel:      derefStr(r.Channel),
			UserID:       derefStr(r.UserID),
			Metadata:     meta,
		},
		Model:           derefStr(r.Model),
		Provider:        derefStr(r.Provider),
		InputTokens:     r.InputTokens,
		OutputTokens:    r.OutputTokens,
		AgentName:       r.AgentName,
		EstimatedTokens: r.EstimatedTokens,
		ContextWindow:   r.ContextWindow,
		CompactionCount: r.CompactionCount,
	}
}
