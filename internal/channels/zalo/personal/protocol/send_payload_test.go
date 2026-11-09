package protocol

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSendMessageOptionsPayload(t *testing.T) {
	key := []byte("0123456789ABCDEF")
	var got map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
			return
		}
		plain, err := DecodeAESCBC(key, r.PostForm.Get("params"))
		if err != nil {
			t.Errorf("decrypt request: %v", err)
			return
		}
		if err := json.Unmarshal(plain, &got); err != nil {
			t.Errorf("unmarshal request: %v", err)
			return
		}
		inner, _ := json.Marshal(map[string]any{
			"error_code": 0,
			"data":       map[string]any{"msgId": 123},
		})
		enc, err := EncodeAESCBC(key, string(inner), false)
		if err != nil {
			t.Errorf("encrypt response: %v", err)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"error_code": 0, "data": enc})
	}))
	defer server.Close()

	sess := NewSession()
	sess.IMEI = "imei"
	sess.SecretKey = base64.StdEncoding.EncodeToString(key)
	sess.LoginInfo = &LoginInfo{ZpwServiceMapV3: ZpwServiceMapV3{Chat: []string{server.URL}}}

	quoteText := "quoted"
	quote := &TMessage{
		MsgID:    "m1",
		CliMsgID: "c1",
		UIDFrom:  "u1",
		TS:       "10",
		MsgType:  "webchat",
		Content:  Content{String: &quoteText},
	}
	msgID, err := SendMessageOptions(t.Context(), sess, "u2", ThreadTypeUser, SendOptions{
		Text:     "hello",
		Styles:   []TextStyle{{Start: 0, Len: 5, St: "b"}},
		Mentions: []MentionInfo{{Pos: 0, Len: 5, UID: "u3", Type: 0}},
		Quote:    quote,
		TTL:      60,
	})
	if err != nil {
		t.Fatal(err)
	}
	if msgID != "123" {
		t.Fatalf("msgID = %q, want 123", msgID)
	}

	if got["message"] != "hello" || got["toid"] != "u2" || got["ttl"] != float64(60) {
		t.Fatalf("payload = %#v", got)
	}
	var textProps map[string]any
	if err := json.Unmarshal([]byte(got["textProperties"].(string)), &textProps); err != nil {
		t.Fatal(err)
	}
	if textProps["ver"] != float64(0) {
		t.Fatalf("textProperties = %#v", textProps)
	}
	styles := textProps["styles"].([]any)
	if len(styles) != 1 || styles[0].(map[string]any)["st"] != "b" {
		t.Fatalf("styles = %#v", styles)
	}
	var mentions []map[string]any
	if err := json.Unmarshal([]byte(got["mentionInfo"].(string)), &mentions); err != nil {
		t.Fatal(err)
	}
	if len(mentions) != 1 || mentions[0]["uid"] != "u3" {
		t.Fatalf("mentions = %#v", mentions)
	}
	if got["qmsgId"] != "m1" || got["qmsgCliId"] != "c1" || got["qmsg"] != "quoted" {
		t.Fatalf("quote payload = %#v", got)
	}
}
