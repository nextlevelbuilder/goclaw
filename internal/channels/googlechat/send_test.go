package googlechat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// mockChatAPI creates an httptest server mimicking Google Chat API.
func mockChatAPI(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) (*httptest.Server, string) {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(handler))
	return ts, ts.URL
}

func TestSendMessage_ShortDM(t *testing.T) {
	var sentBody map[string]any
	ts, baseURL := mockChatAPI(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&sentBody)
		json.NewEncoder(w).Encode(map[string]any{
			"name": "spaces/DM1/messages/123",
		})
	})
	defer ts.Close()

	msg := bus.OutboundMessage{
		ChatID:  "spaces/DM1",
		Content: "Hello world",
		Metadata: map[string]string{
			"peer_kind": "direct",
		},
	}

	err := sendTextMessage(context.Background(), baseURL, "fake-token", &http.Client{}, msg, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if sentBody["text"] == nil {
		t.Error("expected text in sent body")
	}
}

func TestSendMessage_EmptyContent(t *testing.T) {
	msg := bus.OutboundMessage{
		ChatID:  "spaces/DM1",
		Content: "",
	}

	err := sendTextMessage(context.Background(), "http://unused", "fake", &http.Client{}, msg, "", "")
	if err != nil {
		t.Fatal("empty content should not error")
	}
}

func TestBuildCardMessage_Table(t *testing.T) {
	content := "# Results\n\n| Name | Score |\n|---|---|\n| Alice | 95 |\n| Bob | 87 |"
	card := buildCardMessage(content)
	if card == nil {
		t.Fatal("expected card for table content")
	}
	cardJSON, _ := json.Marshal(card)
	s := string(cardJSON)
	if !strings.Contains(s, "cardsV2") {
		t.Error("card JSON should contain cardsV2")
	}
}
