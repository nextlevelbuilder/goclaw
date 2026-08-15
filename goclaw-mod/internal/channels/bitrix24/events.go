package bitrix24

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Event types emitted by Bitrix24 for imbot handlers.
// Source: https://apidocs.bitrix24.com/api-reference/imbot/index.html
const (
	EventMessageAdd    = "ONIMBOTMESSAGEADD"
	EventMessageUpdate = "ONIMBOTMESSAGEUPDATE"
	EventMessageDelete = "ONIMBOTMESSAGEDELETE"
	EventJoinChat      = "ONIMBOTJOINCHAT"
	EventBotDelete     = "ONIMBOTDELETE"
	EventAppUninstall  = "ONAPPUNINSTALL"
)

// Event is the normalised shape of a Bitrix24 outbound webhook call.
// Bitrix posts BOTH application/x-www-form-urlencoded with square-bracket
// indexing (most common) and JSON (rare); this struct is the canonical output
// of ParseEvent regardless of wire format.
type Event struct {
	Type   string
	Auth   EventAuth
	Params EventParams
	Ts     time.Time
	// Raw keeps a copy of the form values for debugging. Only populated for
	// form-urlencoded inputs; nil for JSON.
	Raw url.Values
}

// EventAuth mirrors the `auth[*]` section of a Bitrix24 event POST.
// These fields are what we validate against the stored portal state to
// reject spoofed webhooks — AppToken is the stable per-install secret,
// MemberID is the stable portal id (stable across domain renames).
type EventAuth struct {
	Domain         string
	AppToken       string
	AccessToken    string
	RefreshToken   string
	MemberID       string
	ExpiresIn      int
	Scope          string
	ServerEndpoint string
	ClientEndpoint string
	Status         string
}

// EventParams covers the `data[PARAMS]` section plus resolved bot/user ids.
// BotID is lifted from `data[BOT][<botID>][BOT_ID]` since multiple bots may
// coexist on a portal and the payload tags which bot the event targets.
type EventParams struct {
	MessageID       string
	BotID           int
	DialogID        string // "chatNN" for group chats, numeric user id for DMs
	ChatID          string // data[PARAMS][CHAT_ID] (numeric, DM side may be 0)
	FromUserID      string
	ToUserID        string
	Message         string // stripped text — Bitrix removes @mentions on group chats
	MessageOriginal string // raw BBCode (`[USER=<id>]…[/USER]`); group chat only, "" on DMs
	MessageType     string // "private" | "chat"
	SystemMessage   bool
	// ReplyMessage is the quoted/replied-to message Bitrix24 ships inline on
	// ONIMBOTMESSAGEADD when the user replies to an earlier message. nil when
	// the event is not a reply. See EventReplyMessage for field semantics.
	ReplyMessage *EventReplyMessage
	Files        []EventFile
	// MentionedList is the structured map data[PARAMS][MENTIONED_LIST][<id>]=<id>
	// Bitrix24 emits on group messages. Highest-authority mention source —
	// no regex / Unicode edge cases. Absent (nil) on DMs.
	MentionedList map[string]string

	// ChatEntityType + ChatEntityID expose the entity binding for chats that
	// belong to a Bitrix24 module (CRM Deal/Lead/Contact, Tasks task,
	// Workgroup, Open Channel session). Examples:
	//
	//   CRM Deal:  ChatEntityType="CRM"        ChatEntityID="DEAL|2064"
	//   CRM Lead:  ChatEntityType="CRM"        ChatEntityID="LEAD|123"
	//   Task:      ChatEntityType="TASKS_TASK" ChatEntityID="2704"
	//   Plain:     ChatEntityType=""           ChatEntityID=""
	//
	// Forwarded to the agent via metadata so MCP tools can resolve "this
	// deal/task" deterministically without parsing CHAT_TITLE strings.
	ChatEntityType string
	ChatEntityID   string

	// ChatTitle mirrors data[PARAMS][CHAT_TITLE]. Bitrix pre-formats
	// human-readable names — e.g. "Thân Công Huy - Zalo Synity 0964575404"
	// for Openline connector chats, "Tích hợp channel bitrix24 vào goclaw"
	// for Task chats. Absent on 1-1 DMs. Forwarded as metadata so agents
	// can display / reason about "who am I talking to" without extra RPCs.
	ChatTitle string

	// ChatType mirrors data[PARAMS][CHAT_TYPE]. Single-letter code that
	// classifies the chat surface more precisely than ChatEntityType:
	//   P = private (1-1 DM)      C = group chat / CRM chat
	//   L = Open Line             X = external chat (Tasks)
	//   B = collab / workgroup    O = open chat
	//   S = system/notify         N = channel
	//   J = open channel          T = comment thread
	//   A = copilot chat
	ChatType string

	// ChatEntityData1/2/3 are opaque Bitrix-internal payloads keyed to the
	// chat entity. For Openline sessions:
	//   DATA_1 = "Y|DEAL|2054|N|N|1020|1782879518|0|0|0"
	//            (10 tokens — session state incl. active CRM entity, line id, started_at)
	//   DATA_2 = "LEAD|0|COMPANY|0|CONTACT|1266|DEAL|2054"
	//            (4 slots — CRM linkage: LEAD, COMPANY, CONTACT, DEAL ids)
	//   DATA_3 = "N" (flag, meaning TBD)
	// Kept as raw strings; decoding lives in entity_context.go so the parse
	// path stays a pure copy.
	ChatEntityData1 string
	ChatEntityData2 string
	ChatEntityData3 string

	// FromIsConnector mirrors data[USER][IS_CONNECTOR]. In Bitrix24 Open
	// Channel sessions (MESSAGE_TYPE=L), real customers come in through a
	// connector (Zalo, FB, etc.) and IS_CONNECTOR=Y. Internal staff who join
	// the session report IS_CONNECTOR=N. The flag drives the Open Channel
	// gating in handleMessage: bot drops connector traffic (humans handle
	// customers) and only replies to staff who @-mention it.
	FromIsConnector bool

	// IsHiddenMessage mirrors data[PARAMS][PARAMS][COMPONENT_ID]=HiddenMessage,
	// which Bitrix24 sets for whisper / internal-only messages in Open
	// Channel sessions. Whisper messages MUST NOT be forwarded to external
	// connectors (Zalo, FB, etc.) — Send() routes whisper replies through
	// imbot.message.add with SKIP_CONNECTOR=Y instead of the v2 path.
	// Absent or any value other than "HiddenMessage" → false (public).
	IsHiddenMessage bool
}

// EventReplyMessage captures the message a user replied to (quoted). Bitrix24
// ships it inline on the reply event as `data[PARAMS][REPLY_MESSAGE][...]`:
//   - ID       — MESSAGE_ID of the quoted message (fallback: PARAMS[REPLY_ID])
//   - AuthorID — user id who authored the quoted message
//   - Text     — full quoted body (BBCode); EMPTY when the quoted message is
//     media-only (image/audio/file/video), in which case the attachment must
//     be fetched separately via im.dialog.messages.get (see reply_context.go).
type EventReplyMessage struct {
	ID       string
	AuthorID string
	Text     string
}

// newReplyMessage builds an EventReplyMessage from the raw REPLY_MESSAGE fields.
// replyID is the fallback id from the nested PARAMS[REPLY_ID] used when
// REPLY_MESSAGE[ID] is absent. Returns nil when there is no reply signal at all
// so callers can treat "not a reply" as the natural zero case.
func newReplyMessage(id, authorID, text, replyID string) *EventReplyMessage {
	if id == "" {
		id = replyID
	}
	if id == "" {
		return nil
	}
	return &EventReplyMessage{ID: id, AuthorID: authorID, Text: text}
}

// EventFile is one attachment element extracted from
// `data[PARAMS][FILES][<i>][...]`. URL is the urlMachine (downloadable;
// Bitrix requires `?auth=<access_token>` appended at fetch time).
type EventFile struct {
	ID         string
	Name       string
	Type       string // image | file | video | audio
	URL        string
	URLPreview string
	Size       int64
	Mime       string
}

// MaxEventBodyBytes is the hard cap we accept for a webhook request body.
// Bitrix24 events are small (a few KB for message+files metadata); 1 MiB is
// an order of magnitude above real traffic. The cap matters because the
// /bitrix24/events endpoint is publicly reachable — without it an attacker
// could post an unbounded JSON/form body and exhaust memory before any auth
// check runs (auth happens AFTER parse because we need auth.domain to look
// up the portal).
const MaxEventBodyBytes = 1 << 20 // 1 MiB

// ParseEvent reads the request body and returns a normalised Event.
// It accepts either form-urlencoded (the common case) or JSON.
//
// The function does NOT validate auth; the caller (Router.handleEvent) is
// responsible for checking domain + application_token against the stored
// portal state before trusting any field.
func ParseEvent(r *http.Request) (*Event, error) {
	if r == nil {
		return nil, errors.New("bitrix24 event: nil request")
	}
	// Cap the body BEFORE parsing. http.MaxBytesReader replaces r.Body so
	// both the JSON and form paths inherit the limit.
	if r.Body != nil {
		r.Body = http.MaxBytesReader(nil, r.Body, MaxEventBodyBytes)
	}
	ct := strings.ToLower(r.Header.Get("Content-Type"))
	switch {
	case strings.HasPrefix(ct, "application/json"):
		return parseJSONEvent(r.Body)
	default:
		// Default to form parsing. Bitrix sometimes omits the header entirely.
		if err := r.ParseForm(); err != nil {
			return nil, fmt.Errorf("parse form: %w", err)
		}
		return parseFormEvent(r.Form)
	}
}

// parseFormEvent decodes url.Values with Bitrix's square-bracket convention.
func parseFormEvent(v url.Values) (*Event, error) {
	if len(v) == 0 {
		return nil, errors.New("bitrix24 event: empty form body")
	}

	evt := &Event{Raw: v}
	evt.Type = firstNonEmpty(v.Get("event"), v.Get("EVENT"))
	if evt.Type == "" {
		return nil, errors.New("bitrix24 event: missing event type")
	}

	// Timestamp: seconds-since-epoch as string. Optional.
	if s := firstNonEmpty(v.Get("ts"), v.Get("TS")); s != "" {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			evt.Ts = time.Unix(n, 0).UTC()
		}
	}

	// auth[...]
	evt.Auth = EventAuth{
		Domain:         formGet(v, "auth", "domain"),
		AppToken:       formGet(v, "auth", "application_token"),
		AccessToken:    formGet(v, "auth", "access_token"),
		RefreshToken:   formGet(v, "auth", "refresh_token"),
		MemberID:       formGet(v, "auth", "member_id"),
		Scope:          formGet(v, "auth", "scope"),
		ServerEndpoint: formGet(v, "auth", "server_endpoint"),
		ClientEndpoint: formGet(v, "auth", "client_endpoint"),
		Status:         formGet(v, "auth", "status"),
	}
	if s := formGet(v, "auth", "expires_in"); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			evt.Auth.ExpiresIn = n
		}
	}

	// data[PARAMS][...]
	p := EventParams{
		MessageID:       formGet(v, "data", "PARAMS", "MESSAGE_ID"),
		DialogID:        formGet(v, "data", "PARAMS", "DIALOG_ID"),
		ChatID:          formGet(v, "data", "PARAMS", "CHAT_ID"),
		FromUserID:      formGet(v, "data", "PARAMS", "FROM_USER_ID"),
		ToUserID:        formGet(v, "data", "PARAMS", "TO_USER_ID"),
		Message:         formGet(v, "data", "PARAMS", "MESSAGE"),
		MessageOriginal: formGet(v, "data", "PARAMS", "MESSAGE_ORIGINAL"),
		MessageType:     formGet(v, "data", "PARAMS", "MESSAGE_TYPE"),
		ChatEntityType:  formGet(v, "data", "PARAMS", "CHAT_ENTITY_TYPE"),
		ChatEntityID:    formGet(v, "data", "PARAMS", "CHAT_ENTITY_ID"),
		ChatTitle:       formGet(v, "data", "PARAMS", "CHAT_TITLE"),
		ChatType:        formGet(v, "data", "PARAMS", "CHAT_TYPE"),
		ChatEntityData1: formGet(v, "data", "PARAMS", "CHAT_ENTITY_DATA_1"),
		ChatEntityData2: formGet(v, "data", "PARAMS", "CHAT_ENTITY_DATA_2"),
		ChatEntityData3: formGet(v, "data", "PARAMS", "CHAT_ENTITY_DATA_3"),
	}
	if s := formGet(v, "data", "PARAMS", "SYSTEM"); s == "Y" {
		p.SystemMessage = true
	}
	// data[PARAMS][PARAMS][COMPONENT_ID]=HiddenMessage marks a whisper /
	// internal-only message in Open Channel. Note the nested PARAMS — the
	// outer PARAMS contains MESSAGE_ID/CHAT_ID/etc., and Bitrix tucks UI
	// component metadata into an inner PARAMS sub-object.
	if s := formGet(v, "data", "PARAMS", "PARAMS", "COMPONENT_ID"); s == "HiddenMessage" {
		p.IsHiddenMessage = true
	}
	// data[USER][IS_CONNECTOR] — Y for messages coming in through an Open
	// Channel connector (Zalo, FB Messenger, etc.), N for internal Bitrix24
	// users. Used by the Open Channel gate in handle.go.
	if s := formGet(v, "data", "USER", "IS_CONNECTOR"); strings.EqualFold(s, "Y") {
		p.FromIsConnector = true
	}

	// REPLY_MESSAGE: the quoted message when the user replied. Bitrix ships the
	// full quoted text inline for text originals; media-only originals carry
	// just ID+AUTHOR_ID (MESSAGE empty) and are resolved later. REPLY_ID lives
	// in the nested inner PARAMS as a numeric fallback id.
	p.ReplyMessage = newReplyMessage(
		formGet(v, "data", "PARAMS", "REPLY_MESSAGE", "ID"),
		formGet(v, "data", "PARAMS", "REPLY_MESSAGE", "AUTHOR_ID"),
		formGet(v, "data", "PARAMS", "REPLY_MESSAGE", "MESSAGE"),
		formGet(v, "data", "PARAMS", "PARAMS", "REPLY_ID"),
	)

	// MENTIONED_LIST: data[PARAMS][MENTIONED_LIST][<user_id>]=<user_id>.
	// Iterate all form keys to discover the structured map; key format is
	// stable across portals. Empty if absent (DMs).
	const mentionedPrefix = "data[PARAMS][MENTIONED_LIST]["
	for key, vals := range v {
		if !strings.HasPrefix(key, mentionedPrefix) || !strings.HasSuffix(key, "]") {
			continue
		}
		id := key[len(mentionedPrefix) : len(key)-1]
		if id == "" || len(vals) == 0 {
			continue
		}
		if p.MentionedList == nil {
			p.MentionedList = make(map[string]string)
		}
		p.MentionedList[id] = strings.TrimSpace(vals[0])
	}

	// BOT_ID: inspect every key starting with `data[BOT][<id>][BOT_ID]`.
	// Bitrix wraps the bot id in both the outer bracket AND the inner BOT_ID
	// field; we prefer the inner one because it's a stable number.
	for key, vals := range v {
		if !strings.HasPrefix(key, "data[BOT][") {
			continue
		}
		if !strings.HasSuffix(key, "][BOT_ID]") {
			continue
		}
		if len(vals) == 0 {
			continue
		}
		if n, err := strconv.Atoi(strings.TrimSpace(vals[0])); err == nil {
			p.BotID = n
			break
		}
	}

	// FILES is keyed by the Bitrix24 file ID, not by a 0-based index — webhook
	// payloads look like `data[PARAMS][FILES][29968][name]=...`. Discover every
	// id present in the form, then look the fields up by that id. Older
	// fixtures using "0","1",... still work — they just appear as ids "0","1".
	const filesPrefix = "data[PARAMS][FILES]["
	seen := make(map[string]struct{})
	var fileIDs []string
	for key := range v {
		if !strings.HasPrefix(key, filesPrefix) {
			continue
		}
		rest := key[len(filesPrefix):]
		end := strings.IndexByte(rest, ']')
		if end <= 0 {
			continue
		}
		id := rest[:end]
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		fileIDs = append(fileIDs, id)
	}
	// Deterministic order: numeric ids ascending; non-numeric ids fall back to
	// lexicographic. Map iteration order in Go is random, so without this the
	// output Files slice ordering would flap between runs.
	sort.Slice(fileIDs, func(i, j int) bool {
		ai, aErr := strconv.Atoi(fileIDs[i])
		bi, bErr := strconv.Atoi(fileIDs[j])
		if aErr == nil && bErr == nil {
			return ai < bi
		}
		return fileIDs[i] < fileIDs[j]
	})
	for _, id := range fileIDs {
		name := formGet(v, "data", "PARAMS", "FILES", id, "name")
		url := firstNonEmpty(
			formGet(v, "data", "PARAMS", "FILES", id, "urlMachine"),
			formGet(v, "data", "PARAMS", "FILES", id, "url"),
			formGet(v, "data", "PARAMS", "FILES", id, "urlDownload"),
		)
		if name == "" && url == "" {
			continue
		}
		size, _ := strconv.ParseInt(formGet(v, "data", "PARAMS", "FILES", id, "size"), 10, 64)
		// Prefer the explicit FILES[<id>][id] field when present; fall back to
		// the bracket key itself (which IS the file id in live payloads).
		fileID := formGet(v, "data", "PARAMS", "FILES", id, "id")
		if fileID == "" {
			fileID = id
		}
		p.Files = append(p.Files, EventFile{
			ID:         fileID,
			Name:       name,
			Type:       formGet(v, "data", "PARAMS", "FILES", id, "type"),
			URL:        url,
			URLPreview: formGet(v, "data", "PARAMS", "FILES", id, "urlPreview"),
			Size:       size,
			Mime:       formGet(v, "data", "PARAMS", "FILES", id, "mime"),
		})
	}

	evt.Params = p
	return evt, nil
}

// parseJSONEvent decodes the rarer JSON event shape. Bitrix24 uses identical
// key names to the form version but nested objects instead of brackets.
func parseJSONEvent(body io.ReadCloser) (*Event, error) {
	if body == nil {
		return nil, errors.New("bitrix24 event: nil json body")
	}
	defer body.Close()

	var raw struct {
		Event string `json:"event"`
		Ts    any    `json:"ts"`
		Auth  struct {
			Domain           string `json:"domain"`
			ApplicationToken string `json:"application_token"`
			AccessToken      string `json:"access_token"`
			RefreshToken     string `json:"refresh_token"`
			MemberID         string `json:"member_id"`
			ExpiresIn        any    `json:"expires_in"`
			Scope            string `json:"scope"`
			ServerEndpoint   string `json:"server_endpoint"`
			ClientEndpoint   string `json:"client_endpoint"`
			Status           string `json:"status"`
		} `json:"auth"`
		Data struct {
			Bot  map[string]map[string]any `json:"BOT"`
			User struct {
				IsConnector string `json:"IS_CONNECTOR"`
			} `json:"USER"`
			Params struct {
				MessageID       any            `json:"MESSAGE_ID"`
				DialogID        any            `json:"DIALOG_ID"`
				ChatID          any            `json:"CHAT_ID"`
				FromUserID      any            `json:"FROM_USER_ID"`
				ToUserID        any            `json:"TO_USER_ID"`
				Message         string         `json:"MESSAGE"`
				MessageOriginal string         `json:"MESSAGE_ORIGINAL"`
				MentionedList   map[string]any `json:"MENTIONED_LIST"`
				MessageType     string         `json:"MESSAGE_TYPE"`
				System          string         `json:"SYSTEM"`
				ChatEntityType  string         `json:"CHAT_ENTITY_TYPE"`
				ChatEntityID    string         `json:"CHAT_ENTITY_ID"`
				ChatTitle       string         `json:"CHAT_TITLE"`
				ChatType        string         `json:"CHAT_TYPE"`
				ChatEntityData1 string         `json:"CHAT_ENTITY_DATA_1"`
				ChatEntityData2 string         `json:"CHAT_ENTITY_DATA_2"`
				ChatEntityData3 string         `json:"CHAT_ENTITY_DATA_3"`
				// Nested PARAMS holds UI component metadata. COMPONENT_ID=
				// HiddenMessage marks a whisper / internal-only message.
				// REPLY_ID is the numeric fallback id of the quoted message.
				NestedParams struct {
					ComponentID string `json:"COMPONENT_ID"`
					ReplyID     any    `json:"REPLY_ID"`
				} `json:"PARAMS"`
				// REPLY_MESSAGE is the quoted message on reply events (see
				// EventReplyMessage). MESSAGE is empty for media-only originals.
				ReplyMessage struct {
					ID       any    `json:"ID"`
					AuthorID any    `json:"AUTHOR_ID"`
					Message  string `json:"MESSAGE"`
				} `json:"REPLY_MESSAGE"`
				// FILES may arrive as an array OR a map keyed by file id — keep
				// raw and normalize after Decode.
				Files json.RawMessage `json:"FILES"`
			} `json:"PARAMS"`
		} `json:"data"`
	}
	if err := json.NewDecoder(body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode json event: %w", err)
	}
	if raw.Event == "" {
		return nil, errors.New("bitrix24 event: missing event type")
	}

	evt := &Event{Type: raw.Event}

	switch t := raw.Ts.(type) {
	case float64:
		evt.Ts = time.Unix(int64(t), 0).UTC()
	case string:
		if n, err := strconv.ParseInt(t, 10, 64); err == nil {
			evt.Ts = time.Unix(n, 0).UTC()
		}
	}

	evt.Auth = EventAuth{
		Domain:         raw.Auth.Domain,
		AppToken:       raw.Auth.ApplicationToken,
		AccessToken:    raw.Auth.AccessToken,
		RefreshToken:   raw.Auth.RefreshToken,
		MemberID:       raw.Auth.MemberID,
		Scope:          raw.Auth.Scope,
		ServerEndpoint: raw.Auth.ServerEndpoint,
		ClientEndpoint: raw.Auth.ClientEndpoint,
		Status:         raw.Auth.Status,
	}
	evt.Auth.ExpiresIn = asInt(raw.Auth.ExpiresIn)

	for _, inner := range raw.Data.Bot {
		if id := asInt(inner["BOT_ID"]); id > 0 {
			evt.Params.BotID = id
			break
		}
	}

	p := &evt.Params
	p.MessageID = asString(raw.Data.Params.MessageID)
	p.DialogID = asString(raw.Data.Params.DialogID)
	p.ChatID = asString(raw.Data.Params.ChatID)
	p.FromUserID = asString(raw.Data.Params.FromUserID)
	p.ToUserID = asString(raw.Data.Params.ToUserID)
	p.Message = raw.Data.Params.Message
	p.MessageOriginal = raw.Data.Params.MessageOriginal
	p.MessageType = raw.Data.Params.MessageType
	p.SystemMessage = raw.Data.Params.System == "Y"
	p.ReplyMessage = newReplyMessage(
		asString(raw.Data.Params.ReplyMessage.ID),
		asString(raw.Data.Params.ReplyMessage.AuthorID),
		raw.Data.Params.ReplyMessage.Message,
		asString(raw.Data.Params.NestedParams.ReplyID),
	)
	p.ChatEntityType = raw.Data.Params.ChatEntityType
	p.ChatEntityID = raw.Data.Params.ChatEntityID
	p.ChatTitle = raw.Data.Params.ChatTitle
	p.ChatType = raw.Data.Params.ChatType
	p.ChatEntityData1 = raw.Data.Params.ChatEntityData1
	p.ChatEntityData2 = raw.Data.Params.ChatEntityData2
	p.ChatEntityData3 = raw.Data.Params.ChatEntityData3
	p.FromIsConnector = strings.EqualFold(raw.Data.User.IsConnector, "Y")
	p.IsHiddenMessage = raw.Data.Params.NestedParams.ComponentID == "HiddenMessage"
	if len(raw.Data.Params.MentionedList) > 0 {
		p.MentionedList = make(map[string]string, len(raw.Data.Params.MentionedList))
		for id, val := range raw.Data.Params.MentionedList {
			p.MentionedList[id] = asString(val)
		}
	}

	// Normalize FILES to (key, file-fields) pairs. Bitrix24 ships either an
	// array OR an object keyed by the file id; we handle both. The key is the
	// fallback for the file id when the inner "id" field is absent.
	type filePair struct {
		key    string
		fields map[string]any
	}
	var filePairs []filePair
	if len(raw.Data.Params.Files) > 0 {
		var asArr []map[string]any
		if err := json.Unmarshal(raw.Data.Params.Files, &asArr); err == nil {
			for i, f := range asArr {
				filePairs = append(filePairs, filePair{key: strconv.Itoa(i), fields: f})
			}
		} else {
			var asMap map[string]map[string]any
			if err := json.Unmarshal(raw.Data.Params.Files, &asMap); err == nil {
				for k, f := range asMap {
					filePairs = append(filePairs, filePair{key: k, fields: f})
				}
			}
		}
	}
	for _, fp := range filePairs {
		f := fp.fields
		url := asString(f["urlMachine"])
		if url == "" {
			url = asString(f["url"])
		}
		if url == "" {
			url = asString(f["urlDownload"])
		}
		name := asString(f["name"])
		if name == "" && url == "" {
			continue
		}
		id := asString(f["id"])
		if id == "" {
			id = fp.key
		}
		p.Files = append(p.Files, EventFile{
			ID:         id,
			Name:       name,
			Type:       asString(f["type"]),
			URL:        url,
			URLPreview: asString(f["urlPreview"]),
			Size:       int64(asInt(f["size"])),
			Mime:       asString(f["mime"]),
		})
	}

	return evt, nil
}

// formGet returns the first value at a Bitrix24 bracketed path.
// `formGet(v, "data", "PARAMS", "MESSAGE")` → v.Get("data[PARAMS][MESSAGE]").
func formGet(v url.Values, head string, rest ...string) string {
	if len(rest) == 0 {
		return v.Get(head)
	}
	var b strings.Builder
	b.WriteString(head)
	for _, seg := range rest {
		b.WriteByte('[')
		b.WriteString(seg)
		b.WriteByte(']')
	}
	return v.Get(b.String())
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// asString coerces common JSON scalars to string. Covers the schema drift
// where Bitrix returns numeric ids as either "123" or 123 across releases.
func asString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case bool:
		if x {
			return "Y"
		}
		return "N"
	case json.Number:
		return x.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}

// asInt coerces common JSON scalars to int; returns 0 for anything unparseable.
func asInt(v any) int {
	switch x := v.(type) {
	case nil:
		return 0
	case float64:
		return int(x)
	case int:
		return x
	case int64:
		return int(x)
	case string:
		n, _ := strconv.Atoi(x)
		return n
	case json.Number:
		n, _ := x.Int64()
		return int(n)
	default:
		return 0
	}
}
