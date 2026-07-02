package protocol

import "encoding/json"

// Message is the interface for incoming Zalo messages (DM or group).
type Message interface {
	Type() ThreadType
	ThreadID() string
	IsSelf() bool
}

// TMessage is the raw JSON message payload from Zalo WebSocket.
type TMessage struct {
	MsgID       string  `json:"msgId"`
	CliMsgID    string  `json:"cliMsgId,omitempty"`
	RealMsgID   string  `json:"realMsgId,omitempty"`
	GlobalMsgID string  `json:"globalMsgId,omitempty"`
	UIDFrom     string  `json:"uidFrom"`
	IDTo        string  `json:"idTo"`
	DName       string  `json:"dName"`
	TS          string  `json:"ts"`
	Content     Content `json:"content"`
	Quote       *TQuote `json:"quote,omitempty"`
	MsgType     string  `json:"msgType"`
	CMD         int     `json:"cmd"`
	ST          int     `json:"st"`
	AT          int     `json:"at"`
}

// TGroupMessage extends TMessage with group-specific fields.
type TGroupMessage struct {
	TMessage
	Mentions []*TMention `json:"mentions,omitempty"`
}

func (m *TMessage) UnmarshalJSON(data []byte) error {
	var raw struct {
		MsgID       json.RawMessage `json:"msgId"`
		CliMsgID    json.RawMessage `json:"cliMsgId"`
		RealMsgID   json.RawMessage `json:"realMsgId"`
		GlobalMsgID json.RawMessage `json:"globalMsgId"`
		UIDFrom     string          `json:"uidFrom"`
		IDTo        string          `json:"idTo"`
		DName       string          `json:"dName"`
		TS          json.RawMessage `json:"ts"`
		Content     Content         `json:"content"`
		Quote       *TQuote         `json:"quote,omitempty"`
		MsgType     string          `json:"msgType"`
		CMD         int             `json:"cmd"`
		ST          int             `json:"st"`
		AT          int             `json:"at"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	m.MsgID = rawJSONToDecimalString(raw.MsgID)
	m.CliMsgID = rawJSONToDecimalString(raw.CliMsgID)
	m.RealMsgID = rawJSONToDecimalString(raw.RealMsgID)
	m.GlobalMsgID = rawJSONToDecimalString(raw.GlobalMsgID)
	m.UIDFrom = raw.UIDFrom
	m.IDTo = raw.IDTo
	m.DName = raw.DName
	m.TS = rawJSONToDecimalString(raw.TS)
	m.Content = raw.Content
	m.Quote = raw.Quote
	m.MsgType = raw.MsgType
	m.CMD = raw.CMD
	m.ST = raw.ST
	m.AT = raw.AT
	return nil
}

func (m *TGroupMessage) UnmarshalJSON(data []byte) error {
	var base TMessage
	if err := json.Unmarshal(data, &base); err != nil {
		return err
	}
	var raw struct {
		Mentions []*TMention `json:"mentions,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	m.TMessage = base
	m.Mentions = raw.Mentions
	return nil
}

// TMention represents an @mention in a group message.
type TMention struct {
	UID  string      `json:"uid"` // user ID or "-1" for @all
	Pos  int         `json:"pos"`
	Len  int         `json:"len"`
	Type MentionType `json:"type"` // 0=individual, 1=all
}

// MentionType distinguishes individual vs @all mentions.
type MentionType int

const (
	MentionEach   MentionType = 0
	MentionAll    MentionType = 1
	MentionAllUID             = "-1"
)

// UserMessage represents a DM (type=0).
type UserMessage struct {
	Data     TMessage
	threadID string
	isSelf   bool
}

// NewUserMessage creates a UserMessage, resolving self-sent messages.
func NewUserMessage(selfUID string, data TMessage) UserMessage {
	msg := UserMessage{Data: data, threadID: data.UIDFrom}
	msg.isSelf = data.UIDFrom == DefaultUIDSelf

	if data.UIDFrom == DefaultUIDSelf {
		msg.threadID = data.IDTo
		msg.Data.UIDFrom = selfUID
	}
	if data.IDTo == DefaultUIDSelf {
		msg.Data.IDTo = selfUID
	}
	return msg
}

func (m UserMessage) Type() ThreadType { return ThreadTypeUser }
func (m UserMessage) ThreadID() string { return m.threadID }
func (m UserMessage) IsSelf() bool     { return m.isSelf }

// GroupMessage represents a group message (type=1).
type GroupMessage struct {
	Data     TGroupMessage
	threadID string
	isSelf   bool
}

// NewGroupMessage creates a GroupMessage, resolving self-sent messages.
func NewGroupMessage(selfUID string, data TGroupMessage) GroupMessage {
	g := GroupMessage{Data: data, threadID: data.IDTo}
	g.isSelf = data.UIDFrom == DefaultUIDSelf
	if data.UIDFrom == DefaultUIDSelf {
		g.Data.UIDFrom = selfUID
	}
	return g
}

func (m GroupMessage) Type() ThreadType { return ThreadTypeGroup }
func (m GroupMessage) ThreadID() string { return m.threadID }
func (m GroupMessage) IsSelf() bool     { return m.isSelf }
