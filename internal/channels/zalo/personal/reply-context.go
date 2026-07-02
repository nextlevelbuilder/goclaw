package personal

import (
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/channels/replycontext"
	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/personal/protocol"
)

func (c *Channel) buildReplyContext(threadID string, quote *protocol.TQuote) string {
	if quote == nil {
		return ""
	}
	return c.replyCache.Build(c.replyScope(threadID), zaloQuote(quote))
}

func (c *Channel) rememberReplyContext(threadID, sender string, msg protocol.TMessage, body string) {
	if c.replyCache == nil {
		return
	}
	c.replyCache.Store(c.replyScope(threadID), replycontext.Message{
		IDs:       zaloMessageIDs(msg),
		ParentIDs: zaloQuoteIDs(msg.Quote),
		Sender:    sender,
		Body:      body,
	})
}

func (c *Channel) replyScope(threadID string) replycontext.Scope {
	channelID := c.Name()
	if channelID == "" {
		channelID = c.Type()
	}
	return replycontext.Scope{
		TenantID:  c.TenantID().String(),
		ChannelID: channelID,
		ThreadID:  threadID,
	}
}

func formatQuoteContext(quote *protocol.TQuote) string {
	return replycontext.RenderQuote(zaloQuote(quote))
}

func zaloQuote(quote *protocol.TQuote) replycontext.Quote {
	if quote == nil {
		return replycontext.Quote{}
	}
	return replycontext.Quote{
		IDs:    zaloQuoteIDs(quote),
		Sender: strings.TrimSpace(quote.FromD),
		Body:   strings.TrimSpace(quote.Text()),
	}
}

func zaloMessageIDs(msg protocol.TMessage) []string {
	var ids []string
	ids = append(ids,
		zaloIDAlias("msg", msg.MsgID),
		zaloIDAlias("global", msg.MsgID),
		zaloIDAlias("cli", msg.CliMsgID),
		zaloIDAlias("real", msg.RealMsgID),
		zaloIDAlias("global", msg.GlobalMsgID),
		zaloOwnerCliAlias(msg.UIDFrom, msg.CliMsgID),
		msg.MsgID,
		msg.CliMsgID,
		msg.RealMsgID,
		msg.GlobalMsgID,
	)
	return replycontext.NormalizeIDs(ids)
}

func zaloQuoteIDs(quote *protocol.TQuote) []string {
	if quote == nil {
		return nil
	}
	var ids []string
	ids = append(ids,
		zaloIDAlias("cli", quote.CliMsgID),
		zaloIDAlias("msg", quote.GlobalMsgID),
		zaloIDAlias("global", quote.GlobalMsgID),
		zaloOwnerCliAlias(quote.OwnerID, quote.CliMsgID),
		quote.CliMsgID,
		quote.GlobalMsgID,
	)
	return replycontext.NormalizeIDs(ids)
}

func zaloIDAlias(kind, id string) string {
	kind = strings.TrimSpace(kind)
	id = strings.TrimSpace(id)
	if kind == "" || id == "" {
		return ""
	}
	return "zalo:" + kind + ":" + id
}

func zaloOwnerCliAlias(ownerID, cliMsgID string) string {
	ownerID = strings.TrimSpace(ownerID)
	cliMsgID = strings.TrimSpace(cliMsgID)
	if ownerID == "" || cliMsgID == "" {
		return ""
	}
	return "zalo:owner-cli:" + ownerID + ":" + cliMsgID
}
