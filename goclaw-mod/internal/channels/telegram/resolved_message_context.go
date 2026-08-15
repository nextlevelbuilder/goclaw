package telegram

// resolvedMessageContext bundles the post-gate, post-mention-strip state that
// `handleMessage` computes BEFORE media resolution and downstream dispatch.
// It is the input contract for `processResolvedMessage` (single message) and
// for `dispatchAlbum` (album members[0] as the representative).
//
// Captured at gate-pass time for the representative message:
//   - identity:        userID, senderID, senderLabel
//   - chat addressing: chatID, chatIDStr, localKey
//   - thread metadata: isGroup, isForum, messageThreadID, dmThreadID
//   - resolved cfg:    topicCfg (groupPolicy, systemPrompt, skills, tools, allowFrom, ...)
//   - cleaned content: content (text + caption + lightweight tags + reply/forward/location
//                       enrichment + stripBotMention applied). For an album flush, this
//                       is members[0]'s content snapshot — Telegram puts captions on
//                       the first album message only.
//
// The carrier *telego.Message is NOT a field here; callers pass it alongside
// (single: []{message}, album: members). Keeping the message out of the struct
// lets the album path swap a list of N messages in cleanly without per-member
// rctx copies.
type resolvedMessageContext struct {
	content     string
	userID      string
	senderID    string
	senderLabel string

	chatID    int64
	chatIDStr string
	localKey  string

	isGroup         bool
	isForum         bool
	messageThreadID int
	dmThreadID      int

	topicCfg resolvedTopicConfig
}
