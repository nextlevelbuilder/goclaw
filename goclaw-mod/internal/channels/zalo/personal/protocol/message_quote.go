package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// TQuote is the raw quoted message payload attached to Zalo reply messages.
type TQuote struct {
	OwnerID     string          `json:"ownerId"`
	CliMsgID    string          `json:"cliMsgId"`
	GlobalMsgID string          `json:"globalMsgId"`
	CliMsgType  int             `json:"cliMsgType"`
	TS          string          `json:"ts"`
	Msg         string          `json:"msg"`
	Attach      QuoteAttachment `json:"attach,omitempty"`
	FromD       string          `json:"fromD"`
	TTL         int             `json:"ttl"`
}

func (q *TQuote) UnmarshalJSON(data []byte) error {
	var raw struct {
		OwnerID     json.RawMessage `json:"ownerId"`
		CliMsgID    json.RawMessage `json:"cliMsgId"`
		GlobalMsgID json.RawMessage `json:"globalMsgId"`
		CliMsgType  json.RawMessage `json:"cliMsgType"`
		TS          json.RawMessage `json:"ts"`
		Msg         string          `json:"msg"`
		Attach      QuoteAttachment `json:"attach,omitempty"`
		FromD       string          `json:"fromD"`
		TTL         json.RawMessage `json:"ttl"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	q.OwnerID = rawJSONToDecimalString(raw.OwnerID)
	q.CliMsgID = rawJSONToDecimalString(raw.CliMsgID)
	q.GlobalMsgID = rawJSONToDecimalString(raw.GlobalMsgID)
	q.CliMsgType = rawJSONToInt(raw.CliMsgType)
	q.TS = rawJSONToDecimalString(raw.TS)
	q.Msg = raw.Msg
	q.Attach = raw.Attach
	q.FromD = raw.FromD
	q.TTL = rawJSONToInt(raw.TTL)
	return nil
}

// Text returns the quoted text plus any quoted attachment placeholder.
func (q *TQuote) Text() string {
	if q == nil {
		return ""
	}
	var parts []string
	if msg := strings.TrimSpace(q.Msg); msg != "" {
		parts = append(parts, msg)
	}
	if att := q.Attach.AttachmentText(); att != "" && !slices.Contains(parts, att) {
		parts = append(parts, att)
	}
	return strings.Join(parts, "\n")
}

// QuoteAttachment preserves quote.attach, which Zalo may send either as a JSON
// object or as a string containing a JSON object.
type QuoteAttachment struct {
	Raw json.RawMessage
}

func (a *QuoteAttachment) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return nil
	}

	var encoded string
	if err := json.Unmarshal(data, &encoded); err == nil {
		encoded = strings.TrimSpace(encoded)
		if encoded == "" {
			return nil
		}
		if json.Valid([]byte(encoded)) {
			a.Raw = slices.Clone([]byte(encoded))
			return nil
		}
		a.Raw = slices.Clone(data)
		return nil
	}

	a.Raw = slices.Clone(data)
	return nil
}

func (a QuoteAttachment) MarshalJSON() ([]byte, error) {
	if len(a.Raw) == 0 {
		return []byte("null"), nil
	}
	return a.Raw, nil
}

// ParseAttachment decodes the quoted attachment payload into an Attachment.
// Returns nil when there is no payload or it does not decode as an object.
func (a QuoteAttachment) ParseAttachment() *Attachment {
	if len(a.Raw) == 0 {
		return nil
	}
	var att Attachment
	if err := json.Unmarshal(a.Raw, &att); err != nil {
		return nil
	}
	return &att
}

func (a QuoteAttachment) AttachmentText() string {
	if len(a.Raw) == 0 {
		return ""
	}
	var att Attachment
	if err := json.Unmarshal(a.Raw, &att); err == nil {
		if att.IsImage() || strings.Contains(strings.ToLower(att.Type), "image") {
			if att.Title != "" {
				return fmt.Sprintf("[Quoted image: %s]", att.Title)
			}
			return "[Quoted image]"
		}
		if att.URL() != "" {
			if att.Title != "" {
				return fmt.Sprintf("[Quoted file: %s]", att.Title)
			}
			return "[Quoted file]"
		}
		if att.Title != "" {
			return fmt.Sprintf("[Quoted attachment: %s]", att.Title)
		}
	}

	var text string
	if err := json.Unmarshal(a.Raw, &text); err == nil {
		if text = strings.TrimSpace(text); text != "" {
			return text
		}
	}
	return "[Quoted non-text message]"
}

func rawJSONToDecimalString(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var n json.Number
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&n); err == nil {
		return n.String()
	}
	return string(raw)
}

func rawJSONToInt(raw json.RawMessage) int {
	text := rawJSONToDecimalString(raw)
	if text == "" {
		return 0
	}
	n, err := strconv.Atoi(text)
	if err != nil {
		return 0
	}
	return n
}
