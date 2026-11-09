package protocol

import (
	"encoding/json"
	"strings"
)

// ReactionEvent is emitted when a user reacts to a message.
type ReactionEvent struct {
	ThreadID string
	MsgID    string
	CliMsgID string
	UIDFrom  string
	Icon     string
	IsGroup  bool
}

// UndoEvent is emitted when a user recalls a message.
type UndoEvent struct {
	ThreadID string
	MsgID    string
	CliMsgID string
	IsGroup  bool
}

// GroupEvent is emitted for group/friend control events (join, leave, etc.).
type GroupEvent struct {
	Type     string
	GroupID  string
	ActorIDs []string
	Extra    map[string]any
}

// undoEventFromMessage extracts a deleteMsg payload from a message content
// object. Zalo delivers recall events through the normal 501/521 message
// envelopes with content = {deleteMsg, globalMsgId, cliMsgId, ...}.
func undoEventFromMessage(content Content, threadID string, isGroup bool) *UndoEvent {
	if len(content.Raw) == 0 {
		return nil
	}
	var raw struct {
		GlobalMsgID json.RawMessage `json:"globalMsgId"`
		CliMsgID    json.RawMessage `json:"cliMsgId"`
		DeleteMsg   json.RawMessage `json:"deleteMsg"`
	}
	if err := json.Unmarshal(content.Raw, &raw); err != nil {
		return nil
	}
	if !rawTruthy(raw.DeleteMsg) {
		return nil
	}
	return &UndoEvent{
		ThreadID: threadID,
		MsgID:    rawJSONToDecimalString(raw.GlobalMsgID),
		CliMsgID: rawJSONToDecimalString(raw.CliMsgID),
		IsGroup:  isGroup,
	}
}

func rawTruthy(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" || trimmed == "false" || trimmed == "0" || trimmed == `""` {
		return false
	}
	return true
}

// parseReaction parses one react/reactGroup payload. Zalo's react content
// carries rMsg[0] with the target IDs and rIcon with the emoji.
func parseReaction(raw json.RawMessage, isGroup bool) *ReactionEvent {
	var payload struct {
		UIDFrom string `json:"uidFrom"`
		IDTo    string `json:"idTo"`
		Content struct {
			RMsg []struct {
				GMsgID json.RawMessage `json:"gMsgID"`
				CMsgID json.RawMessage `json:"cMsgID"`
			} `json:"rMsg"`
			RIcon string `json:"rIcon"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}
	if len(payload.Content.RMsg) == 0 {
		return nil
	}
	threadID := payload.UIDFrom
	if isGroup || payload.UIDFrom == DefaultUIDSelf {
		threadID = payload.IDTo
	}
	return &ReactionEvent{
		ThreadID: threadID,
		MsgID:    rawJSONToDecimalString(payload.Content.RMsg[0].GMsgID),
		CliMsgID: rawJSONToDecimalString(payload.Content.RMsg[0].CMsgID),
		UIDFrom:  payload.UIDFrom,
		Icon:     payload.Content.RIcon,
		IsGroup:  isGroup,
	}
}

func parseGroupEvent(actType, act, groupID string, actorIDs []string, data json.RawMessage) *GroupEvent {
	extra := make(map[string]any)
	if len(data) > 0 {
		if data[0] == '{' {
			_ = json.Unmarshal(data, &extra)
		} else {
			var encoded string
			if json.Unmarshal(data, &encoded) == nil {
				_ = json.Unmarshal([]byte(encoded), &extra)
			}
		}
	}
	if groupID == "" {
		for _, key := range []string{"groupId", "grid", "group_id"} {
			if v, ok := extra[key].(string); ok && v != "" {
				groupID = v
				break
			}
		}
	}
	eventType := actType
	if act != "" {
		eventType = actType + ":" + act
	}
	return &GroupEvent{Type: eventType, GroupID: groupID, ActorIDs: actorIDs, Extra: extra}
}
