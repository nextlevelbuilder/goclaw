package bitrix24

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	Domain           string
	AppToken         string
	AccessToken      string
	RefreshToken     string
	MemberID         string
	ExpiresIn        int
	Scope            string
	ServerEndpoint   string
	ClientEndpoint   string
	Status           string
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
	ReplyToMID      string
	Files           []EventFile
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
		ReplyToMID:      formGet(v, "data", "PARAMS", "REPLY_TO_MESSAGE_ID"),
		ChatEntityType:  formGet(v, "data", "PARAMS", "CHAT_ENTITY_TYPE"),
		ChatEntityID:    formGet(v, "data", "PARAMS", "CHAT_ENTITY_ID"),
	}
	if s := formGet(v, "data", "PARAMS", "SYSTEM"); s == "Y" {
		p.SystemMessage = true
	}

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

	// Files iterate indices until name+url both empty.
	for i := 0; i < 32; i++ {
		name := formGet(v, "data", "PARAMS", "FILES", strconv.Itoa(i), "name")
		url := firstNonEmpty(
			formGet(v, "data", "PARAMS", "FILES", strconv.Itoa(i), "urlMachine"),
			formGet(v, "data", "PARAMS", "FILES", strconv.Itoa(i), "url"),
		)
		if name == "" && url == "" {
			break
		}
		size, _ := strconv.ParseInt(formGet(v, "data", "PARAMS", "FILES", strconv.Itoa(i), "size"), 10, 64)
		p.Files = append(p.Files, EventFile{
			ID:         formGet(v, "data", "PARAMS", "FILES", strconv.Itoa(i), "id"),
			Name:       name,
			Type:       formGet(v, "data", "PARAMS", "FILES", strconv.Itoa(i), "type"),
			URL:        url,
			URLPreview: formGet(v, "data", "PARAMS", "FILES", strconv.Itoa(i), "urlPreview"),
			Size:       size,
			Mime:       formGet(v, "data", "PARAMS", "FILES", strconv.Itoa(i), "mime"),
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
			Bot    map[string]map[string]any `json:"BOT"`
			Params struct {
				MessageID       any              `json:"MESSAGE_ID"`
				DialogID        any              `json:"DIALOG_ID"`
				ChatID          any              `json:"CHAT_ID"`
				FromUserID      any              `json:"FROM_USER_ID"`
				ToUserID        any              `json:"TO_USER_ID"`
				Message         string           `json:"MESSAGE"`
				MessageOriginal string           `json:"MESSAGE_ORIGINAL"`
				MentionedList   map[string]any   `json:"MENTIONED_LIST"`
				MessageType     string           `json:"MESSAGE_TYPE"`
				System          string           `json:"SYSTEM"`
				ReplyToMID      any              `json:"REPLY_TO_MESSAGE_ID"`
				ChatEntityType  string           `json:"CHAT_ENTITY_TYPE"`
				ChatEntityID    string           `json:"CHAT_ENTITY_ID"`
				Files           []map[string]any `json:"FILES"`
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
	p.ReplyToMID = asString(raw.Data.Params.ReplyToMID)
	p.ChatEntityType = raw.Data.Params.ChatEntityType
	p.ChatEntityID = raw.Data.Params.ChatEntityID
	if len(raw.Data.Params.MentionedList) > 0 {
		p.MentionedList = make(map[string]string, len(raw.Data.Params.MentionedList))
		for id, val := range raw.Data.Params.MentionedList {
			p.MentionedList[id] = asString(val)
		}
	}

	for _, f := range raw.Data.Params.Files {
		url := asString(f["urlMachine"])
		if url == "" {
			url = asString(f["url"])
		}
		name := asString(f["name"])
		if name == "" && url == "" {
			continue
		}
		p.Files = append(p.Files, EventFile{
			ID:         asString(f["id"]),
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
