package personal

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/personal/protocol"
)

func (c *Channel) handleReaction(ev protocol.ReactionEvent) {
	if ev.ThreadID == "" || ev.MsgID == "" || ev.UIDFrom == protocol.DefaultUIDSelf {
		return
	}
	slog.Debug("zalo_personal reaction",
		"thread", ev.ThreadID,
		"msg_id", ev.MsgID,
		"cli_msg_id", ev.CliMsgID,
		"uid", ev.UIDFrom,
		"icon", ev.Icon,
		"group", ev.IsGroup,
	)

	// Record a lightweight note for the next mentioned turn. Reactions must not
	// wake the agent by default; ReactionWakeup opts into that explicitly.
	if ev.IsGroup && c.GroupHistory() != nil {
		c.GroupHistory().Record(ev.ThreadID, channels.HistoryEntry{
			Sender:    "[reaction]",
			SenderID:  ev.UIDFrom,
			Body:      fmt.Sprintf("%s reacted %s to message %s", displayNameOrID("", ev.UIDFrom), ev.Icon, ev.MsgID),
			Timestamp: time.Now(),
			MessageID: ev.MsgID + ":react:" + ev.UIDFrom,
		}, c.HistoryLimit())
	}
	if !c.config.ReactionWakeup {
		return
	}

	peerKind := "direct"
	if ev.IsGroup {
		peerKind = "group"
	}
	c.HandleAuthorizedMessage(ev.UIDFrom, ev.ThreadID,
		fmt.Sprintf("[Reaction] %s reacted %s to message %s", displayNameOrID("", ev.UIDFrom), ev.Icon, ev.MsgID),
		nil,
		map[string]string{
			"platform":   channels.TypeZaloPersonal,
			"message_id": ev.MsgID,
			"reaction":   ev.Icon,
		},
		peerKind,
	)
}

func (c *Channel) handleUndo(ev protocol.UndoEvent) {
	if ev.ThreadID == "" {
		return
	}
	ids := []string{ev.MsgID, ev.CliMsgID}
	if c.GroupHistory() != nil {
		for _, id := range ids {
			c.GroupHistory().DeleteMessage(ev.ThreadID, id)
		}
	}
	if c.replyCache != nil {
		c.replyCache.Delete(c.replyScope(ev.ThreadID), ids...)
	}
	for _, id := range ids {
		c.quoteCache.Delete(id)
	}
	slog.Debug("zalo_personal message recalled",
		"thread", ev.ThreadID,
		"msg_id", ev.MsgID,
		"cli_msg_id", ev.CliMsgID,
		"group", ev.IsGroup,
	)
}

func (c *Channel) handleGroupEvent(ev protocol.GroupEvent) {
	slog.Debug("zalo_personal group event",
		"type", ev.Type,
		"group_id", ev.GroupID,
		"actors", ev.ActorIDs,
	)
}
