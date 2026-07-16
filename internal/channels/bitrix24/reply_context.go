package bitrix24

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// quotedTextMaxRunes caps how much of a quoted text message we inline into the
// agent prompt. Long quotes add little context but cost tokens; ~500 runes is
// enough for the agent to recognise which message is being answered.
const quotedTextMaxRunes = 500

// resolveReplyContext turns a reply/quote event into (a) a short note to
// prepend to the agent's input and (b) any quoted attachment re-injected as
// media. It is the single entry point for the Hybrid design:
//
//   - Text original  → Bitrix ships REPLY_MESSAGE.MESSAGE inline; no API call.
//   - Media original → REPLY_MESSAGE.MESSAGE is empty and carries no file id, so
//     fetch the original via im.dialog.messages.get and re-inject the file(s)
//     through the same download+size pipeline as a direct upload.
//
// Best-effort and non-blocking: any fetch/download failure degrades to a
// note-only result so the user's message still reaches the agent. Returns
// ("", nil) when the event is not a reply.
func (c *Channel) resolveReplyContext(ctx context.Context, evt *Event) (string, []bus.MediaFile) {
	rm := evt.Params.ReplyMessage
	if rm == nil {
		return "", nil
	}
	author := c.quotedAuthorName(ctx, rm.AuthorID)

	// TEXT quote — the common case. Bitrix delivers the full quoted body inline,
	// so no extra REST call is needed.
	if q := sanitizeQuotedText(rm.Text); q != "" {
		return fmt.Sprintf("[Đang trả lời tin của %s: \"%s\"]\n", author, q), nil
	}

	// MEDIA quote — MESSAGE empty. Recover the quoted attachment(s) so the agent
	// can actually "see" the file being answered (STT for audio, vision for
	// images, read_document for the rest).
	files, err := c.fetchReplyMessageFiles(ctx, evt.Params.DialogID, rm.ID)
	if err != nil || len(files) == 0 {
		if err != nil {
			slog.Debug("bitrix24 reply: could not resolve quoted media, note only",
				"reply_id", rm.ID, "dialog_id", evt.Params.DialogID, "err", err)
		}
		return fmt.Sprintf("[Đang trả lời một tin nhắn của %s]\n", author), nil
	}

	note := quotedMediaNote(files, author)
	// Re-inject through the SAME pipeline as inbound files: oversized files are
	// skipped (note-only fallback) and the rest flow into the media tags /
	// read_* tools exactly like a directly-attached upload. DRY — no new size
	// or MIME logic here.
	extra := c.downloadEventFiles(ctx, c.BotID(), files)
	return note, extra
}

// quotedMediaNote builds the "[Đang trả lời một <loại> ...]" prefix for a
// media-only quoted message. Single file → name + kind; multiple → a count.
func quotedMediaNote(files []EventFile, author string) string {
	if len(files) == 1 {
		f := files[0]
		if f.Name != "" {
			return fmt.Sprintf("[Đang trả lời một %s \"%s\" của %s]\n", quotedKindVN(f.Type), f.Name, author)
		}
		return fmt.Sprintf("[Đang trả lời một %s của %s]\n", quotedKindVN(f.Type), author)
	}
	return fmt.Sprintf("[Đang trả lời %d tệp đính kèm của %s]\n", len(files), author)
}

// quotedAuthorName resolves a Bitrix user id to a display name for the reply
// note, falling back to "người dùng <id>" (and a generic label for an empty id)
// so the note is always human-readable.
func (c *Channel) quotedAuthorName(ctx context.Context, authorID string) string {
	authorID = strings.TrimSpace(authorID)
	if authorID == "" {
		return "người dùng"
	}
	if name, _ := c.resolveContactName(ctx, authorID); strings.TrimSpace(name) != "" {
		return name
	}
	return "người dùng " + authorID
}

// quotedKindVN maps a Bitrix file type to a Vietnamese noun for the reply note.
func quotedKindVN(fileType string) string {
	switch strings.ToLower(strings.TrimSpace(fileType)) {
	case "image":
		return "hình ảnh"
	case "audio":
		return "tin nhắn thoại"
	case "video":
		return "video"
	default:
		return "tệp"
	}
}

// bxResidualBBCodeRE strips the BBCode formatting / container tags Bitrix may
// leave in a quoted body AFTER user mentions are rewritten to readable form.
// It removes the tags only — inner text is kept.
var bxResidualBBCodeRE = regexp.MustCompile(`(?is)\[/?(b|i|u|s|quote|code|color|size|font|table|tr|td|list|\*|img|url|context|disk|br|p)(=[^\]]*)?\]`)

// sanitizeQuotedText normalises a quoted Bitrix message body for inlining into
// the agent prompt: rewrite user mentions to "@Name (ID:..)", strip residual
// BBCode noise, collapse whitespace, and truncate. Returns "" for an
// empty/whitespace-only or media-only quote.
func sanitizeQuotedText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	text = bxConvertUserMentionsToReadable(text)
	text = bxResidualBBCodeRE.ReplaceAllString(text, "")
	text = strings.Join(strings.Fields(text), " ") // collapse newlines + runs of spaces
	return truncateRunes(text, quotedTextMaxRunes)
}

// truncateRunes truncates on rune boundaries (Bitrix bodies are UTF-8 with
// multibyte Vietnamese) and appends an ellipsis when it cuts.
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// fetchReplyMessageFiles fetches the attachment metadata of the quoted message
// via im.dialog.messages.get (scope `im`, available to the bot token — unlike
// imbot.v2.Chat.Message.get which is blocked for TYPE=bot bots). FIRST_ID is
// the anchor for "newer than": msgID-1 so the quoted message itself is included.
func (c *Channel) fetchReplyMessageFiles(ctx context.Context, dialogID, msgID string) ([]EventFile, error) {
	client := c.Client()
	if client == nil {
		return nil, errors.New("bitrix24 reply: no REST client")
	}
	dialogID = strings.TrimSpace(dialogID)
	msgID = strings.TrimSpace(msgID)
	if dialogID == "" || msgID == "" {
		return nil, errors.New("bitrix24 reply: dialog_id/msg_id empty")
	}

	// FIRST_ID returns messages with id strictly greater than it, so anchor at
	// msgID-1 to include the quoted message. LIMIT 2 keeps the payload tiny.
	firstID := msgID
	if n, err := strconv.Atoi(msgID); err == nil && n > 0 {
		firstID = strconv.Itoa(n - 1)
	}

	rr, err := client.Call(ctx, "im.dialog.messages.get", map[string]any{
		"DIALOG_ID": dialogID,
		"FIRST_ID":  firstID,
		"LIMIT":     2,
	})
	if err != nil {
		return nil, err
	}
	return parseReplyFiles(rr.Result, msgID)
}

// parseReplyFiles extracts the EventFile metadata of the message whose id ==
// msgID from an im.dialog.messages.get result. Defensive against Bitrix's PHP
// JSON quirks: `messages` and `files` are decoded as raw first so an empty
// collection encoded as `[]` (object→array) never breaks the whole decode.
func parseReplyFiles(raw json.RawMessage, msgID string) ([]EventFile, error) {
	if len(raw) == 0 {
		return nil, errors.New("bitrix24 reply: empty result")
	}
	var res struct {
		Messages json.RawMessage `json:"messages"`
		Files    json.RawMessage `json:"files"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("bitrix24 reply: decode result: %w", err)
	}

	// messages: [] of { id, params: { FILE_ID: [...] } }.
	var msgs []struct {
		ID     any             `json:"id"`
		Params json.RawMessage `json:"params"`
	}
	_ = json.Unmarshal(res.Messages, &msgs) // tolerate `[]`/absent → no files

	var fileIDs []string
	for _, m := range msgs {
		if asString(m.ID) != msgID {
			continue
		}
		fileIDs = append(fileIDs, extractFileIDs(m.Params)...)
	}
	if len(fileIDs) == 0 {
		return nil, nil
	}

	// files: map keyed by file id → { id, type, name, size }. Absent/`[]` → the
	// ids still let downloadEventFiles fetch by id (name/type just unknown).
	var filesMap map[string]struct {
		ID   any    `json:"id"`
		Type string `json:"type"`
		Name string `json:"name"`
		Size any    `json:"size"`
	}
	_ = json.Unmarshal(res.Files, &filesMap)

	out := make([]EventFile, 0, len(fileIDs))
	for _, fid := range fileIDs {
		ef := EventFile{ID: fid}
		if f, ok := filesMap[fid]; ok {
			ef.Name = f.Name
			ef.Type = f.Type
			ef.Size = int64(asInt(f.Size))
		}
		out = append(out, ef)
	}
	return out, nil
}

// extractFileIDs pulls the FILE_ID list from a message's raw params. FILE_ID is
// an array of ids in live payloads, but tolerate a scalar and an empty/`[]`
// params object without erroring.
func extractFileIDs(rawParams json.RawMessage) []string {
	if len(rawParams) == 0 {
		return nil
	}
	var p struct {
		FileID json.RawMessage `json:"FILE_ID"`
	}
	if err := json.Unmarshal(rawParams, &p); err != nil || len(p.FileID) == 0 {
		return nil
	}
	var arr []any
	if err := json.Unmarshal(p.FileID, &arr); err == nil {
		out := make([]string, 0, len(arr))
		for _, v := range arr {
			if s := asString(v); s != "" && s != "0" {
				out = append(out, s)
			}
		}
		return out
	}
	var one any
	if err := json.Unmarshal(p.FileID, &one); err == nil {
		if s := asString(one); s != "" && s != "0" {
			return []string{s}
		}
	}
	return nil
}
