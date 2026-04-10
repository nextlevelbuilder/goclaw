package pancake

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

func TestMessageHandlerSkipsRecentOutboundEchoWithHTMLFormatting(t *testing.T) {
	msgBus := bus.New()
	ch := &Channel{
		BaseChannel: channels.NewBaseChannel(channels.TypePancake, msgBus, nil),
		pageID:      "page-123",
	}
	ch.rememberOutboundEcho("conv-1", "Line 1\nLine 2")

	ch.handleMessagingEvent(MessagingData{
		PageID:         "page-123",
		ConversationID: "conv-1",
		Type:           "INBOX",
		Platform:       "facebook",
		Message: MessagingMessage{
			ID:       "msg-echo-html-1",
			SenderID: "user-1",
			Content:  "<div>Line 1<br key='n_0' />Line 2</div>",
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if _, ok := msgBus.ConsumeInbound(ctx); ok {
		t.Fatal("expected HTML-formatted echo to be dropped")
	}
}

func TestWebhookRouterSkipsNonInboxConversationEvents(t *testing.T) {
	msgBus := bus.New()
	target := &Channel{
		BaseChannel: channels.NewBaseChannel(channels.TypePancake, msgBus, nil),
		pageID:      "page-123",
		platform:    "facebook",
	}
	router := &webhookRouter{
		instances: map[string]*Channel{
			"page-123": target,
		},
	}

	body := `{
		"page_id": "page-123",
		"event_type": "messaging",
		"data": {
			"conversation": {
				"id": "page-123_user-1",
				"type": "COMMENT",
				"from": {
					"id": "user-1",
					"name": "Customer"
				}
			},
			"message": {
				"id": "msg-comment-1",
				"message": "hi"
			}
		}
	}`

	req := httptest.NewRequest(http.MethodPost, "/channels/pancake/webhook", strings.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if _, ok := msgBus.ConsumeInbound(ctx); ok {
		t.Fatal("expected COMMENT conversation webhook to be ignored")
	}
}

func TestWebhookRouterPrefersMessageSenderOverConversationSender(t *testing.T) {
	msgBus := bus.New()
	target := &Channel{
		BaseChannel: channels.NewBaseChannel(channels.TypePancake, msgBus, nil),
		pageID:      "page-123",
		platform:    "facebook",
	}
	router := &webhookRouter{
		instances: map[string]*Channel{
			"page-123": target,
		},
	}

	body := `{
		"page_id": "page-123",
		"event_type": "messaging",
		"data": {
			"conversation": {
				"id": "conv-1",
				"type": "INBOX",
				"from": {
					"id": "user-initiator",
					"name": "Conversation Starter"
				}
			},
			"message": {
				"id": "msg-actual-sender-1",
				"message": "xin chao",
				"from": {
					"id": "user-actual",
					"name": "Actual Sender",
					"page_customer_id": "pc-123"
				}
			}
		}
	}`

	req := httptest.NewRequest(http.MethodPost, "/channels/pancake/webhook", strings.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	msg, ok := msgBus.ConsumeInbound(ctx)
	if !ok {
		t.Fatal("expected inbound message to be published")
	}
	if got, want := msg.SenderID, "user-actual"; got != want {
		t.Fatalf("sender_id = %q, want %q", got, want)
	}
	if got, want := msg.Metadata["display_name"], "Actual Sender"; got != want {
		t.Fatalf("metadata.display_name = %q, want %q", got, want)
	}
}

func TestWebhookRouterSkipsPageAuthoredInboxReply(t *testing.T) {
	msgBus := bus.New()
	target := &Channel{
		BaseChannel: channels.NewBaseChannel(channels.TypePancake, msgBus, nil),
		pageID:      "page-123",
		platform:    "facebook",
	}
	router := &webhookRouter{
		instances: map[string]*Channel{
			"page-123": target,
		},
	}

	body := `{
		"page_id": "page-123",
		"event_type": "messaging",
		"data": {
			"conversation": {
				"id": "conv-1",
				"type": "INBOX",
				"from": {
					"id": "user-1",
					"name": "Customer"
				}
			},
			"message": {
				"id": "msg-page-reply-1",
				"message": "manual page reply",
				"from": {
					"id": "page-123",
					"name": "Page Bot"
				}
			}
		}
	}`

	req := httptest.NewRequest(http.MethodPost, "/channels/pancake/webhook", strings.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if _, ok := msgBus.ConsumeInbound(ctx); ok {
		t.Fatal("expected page-authored reply not to be published inbound")
	}
}

func TestWebhookRouterSkipsAssignedStaffReply(t *testing.T) {
	msgBus := bus.New()
	target := &Channel{
		BaseChannel: channels.NewBaseChannel(channels.TypePancake, msgBus, nil),
		pageID:      "page-123",
		platform:    "facebook",
	}
	router := &webhookRouter{
		instances: map[string]*Channel{
			"page-123": target,
		},
	}

	body := `{
		"page_id": "page-123",
		"event_type": "messaging",
		"data": {
			"conversation": {
				"id": "conv-1",
				"type": "INBOX",
				"assignee_ids": ["staff-1"],
				"from": {
					"id": "user-1",
					"name": "Customer"
				}
			},
			"message": {
				"id": "msg-staff-reply-1",
				"message": "manual staff reply",
				"from": {
					"id": "staff-1",
					"name": "Assigned Staff"
				}
			}
		}
	}`

	req := httptest.NewRequest(http.MethodPost, "/channels/pancake/webhook", strings.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if _, ok := msgBus.ConsumeInbound(ctx); ok {
		t.Fatal("expected assigned staff reply not to be published inbound")
	}
}

func TestSendThenWebhookEchoDoesNotRepublishInbound(t *testing.T) {
	msgBus := bus.New()
	api := NewAPIClient("user-token", "page-token", "page-123")
	sendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true}`)
	}))
	defer sendServer.Close()

	api.pageV1BaseURL = sendServer.URL
	api.httpClient = sendServer.Client()

	ch := &Channel{
		BaseChannel: channels.NewBaseChannel(channels.TypePancake, msgBus, nil),
		apiClient:   api,
		pageID:      "page-123",
		platform:    "facebook",
	}

	if err := ch.Send(context.Background(), bus.OutboundMessage{
		Channel: "pancake",
		ChatID:  "conv-1",
		Content: "Line 1\nLine 2",
	}); err != nil {
		t.Fatalf("Send returned error: %v", err)
	}

	router := &webhookRouter{
		instances: map[string]*Channel{
			"page-123": ch,
		},
	}

	body := `{
		"page_id": "page-123",
		"event_type": "messaging",
		"data": {
			"conversation": {
				"id": "conv-1",
				"type": "INBOX",
				"from": {
					"id": "user-1",
					"name": "Customer"
				}
			},
			"message": {
				"id": "msg-echo-roundtrip-1",
				"message": "<div>Line 1<br key='n_0' />Line 2</div>"
			}
		}
	}`

	req := httptest.NewRequest(http.MethodPost, "/channels/pancake/webhook", strings.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if _, ok := msgBus.ConsumeInbound(ctx); ok {
		t.Fatal("expected echoed outbound send not to republish inbound work")
	}
}

// TestSendRaceConditionEchoArrivesBeforeSendReturns simulates the production race:
// webhook echo arriving while SendMessage HTTP call is still in flight.
// Before the fix, rememberOutboundEcho was called AFTER SendMessage, so an echo
// arriving during the HTTP round-trip would not be recognized.
func TestSendRaceConditionEchoArrivesBeforeSendReturns(t *testing.T) {
	msgBus := bus.New()
	api := NewAPIClient("user-token", "page-token", "page-123")

	ch := &Channel{
		BaseChannel: channels.NewBaseChannel(channels.TypePancake, msgBus, nil),
		apiClient:   api,
		pageID:      "page-123",
		platform:    "facebook",
	}

	router := &webhookRouter{
		instances: map[string]*Channel{
			"page-123": ch,
		},
	}

	const outboundContent = "Em đây, alive luôn 😊\nThoát loop thành công rồi nha."

	// Pancake API server that delivers the echo webhook BEFORE returning the
	// SendMessage response — simulating the real-world race condition.
	sendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// While SendMessage is "in flight", deliver the echo webhook.
		echoBody := `{
			"page_id": "page-123",
			"event_type": "messaging",
			"data": {
				"conversation": {
					"id": "conv-race",
					"type": "INBOX",
					"from": {"id": "user-1", "name": "Customer"}
				},
				"message": {
					"id": "msg-race-echo",
					"message": "<div>Em đây, alive luôn 😊 <br key='n_0' />Thoát loop thành công rồi nha.</div>"
				}
			}
		}`
		echoReq := httptest.NewRequest(http.MethodPost, "/channels/pancake/webhook",
			strings.NewReader(echoBody))
		echoW := httptest.NewRecorder()
		router.ServeHTTP(echoW, echoReq)

		// Now return SendMessage success.
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true}`)
	}))
	defer sendServer.Close()

	api.pageV1BaseURL = sendServer.URL
	api.httpClient = sendServer.Client()

	if err := ch.Send(context.Background(), bus.OutboundMessage{
		Channel: "pancake",
		ChatID:  "conv-race",
		Content: outboundContent,
	}); err != nil {
		t.Fatalf("Send returned error: %v", err)
	}

	// The echo webhook was delivered during Send. It must NOT have been published.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if _, ok := msgBus.ConsumeInbound(ctx); ok {
		t.Fatal("echo arrived during SendMessage HTTP call but was not suppressed — race condition not fixed")
	}
}
