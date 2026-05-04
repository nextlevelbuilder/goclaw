package bot

import "encoding/json"

type zaloAPIResponse struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result,omitempty"`
	ErrorCode   int             `json:"error_code,omitempty"`
	Description string          `json:"description,omitempty"`
}

type zaloBotInfo struct {
	ID   string `json:"id"`
	Name string `json:"display_name"`
}

type zaloMessage struct {
	MessageID string   `json:"message_id"`
	Text      string   `json:"text"`
	Photo     string   `json:"photo"`
	PhotoURL  string   `json:"photo_url"`
	Caption   string   `json:"caption"`
	From      zaloFrom `json:"from"`
	Chat      zaloChat `json:"chat"`
	Date      int64    `json:"date"`
}

type zaloFrom struct {
	ID       string `json:"id"`
	Username string `json:"display_name"`
}

type zaloChat struct {
	ID   string `json:"id"`
	Type string `json:"chat_type"`
}

type zaloUpdate struct {
	EventName string       `json:"event_name"`
	Message   *zaloMessage `json:"message,omitempty"`
}
