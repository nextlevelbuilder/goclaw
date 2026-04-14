package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// --- Fragment reassembly ---

func TestReassemble_SingleFragment(t *testing.T) {
	c := NewWSClient("app", "secret", "http://localhost", nil)
	// sum=1 means no fragmentation needed — caller skips reassemble
	// but if called with sum=1 it should complete immediately
	result := c.reassemble("msg_single", 1, 0, []byte("payload"))
	if result == nil {
		t.Error("single fragment should return payload immediately")
	}
	if string(result) != "payload" {
		t.Errorf("got %q, want %q", result, "payload")
	}
}

func TestReassemble_TwoFragments_OutOfOrder(t *testing.T) {
	c := NewWSClient("app", "secret", "http://localhost", nil)

	// Arrive out of order: seq=1 before seq=0
	result1 := c.reassemble("msg_two", 2, 1, []byte("world"))
	if result1 != nil {
		t.Error("first fragment should not complete reassembly")
	}

	result2 := c.reassemble("msg_two", 2, 0, []byte("hello"))
	if result2 == nil {
		t.Fatal("second fragment should complete reassembly")
	}
	// Assembled in seq order: seq0 + seq1
	if string(result2) != "helloworld" {
		t.Errorf("reassembly: got %q, want %q", result2, "helloworld")
	}
}

func TestReassemble_ThreeFragments(t *testing.T) {
	c := NewWSClient("app", "secret", "http://localhost", nil)

	c.reassemble("msg_three", 3, 0, []byte("aaa"))
	c.reassemble("msg_three", 3, 2, []byte("ccc"))
	result := c.reassemble("msg_three", 3, 1, []byte("bbb"))

	if result == nil {
		t.Fatal("all 3 fragments received, should complete")
	}
	if string(result) != "aaabbbccc" {
		t.Errorf("got %q, want %q", result, "aaabbbccc")
	}
}

func TestReassemble_DifferentMessages_Isolated(t *testing.T) {
	c := NewWSClient("app", "secret", "http://localhost", nil)

	// Two separate messages, each with 2 parts
	c.reassemble("msgA", 2, 0, []byte("A0"))
	c.reassemble("msgB", 2, 0, []byte("B0"))

	resultA := c.reassemble("msgA", 2, 1, []byte("A1"))
	resultB := c.reassemble("msgB", 2, 1, []byte("B1"))

	if resultA == nil || string(resultA) != "A0A1" {
		t.Errorf("msgA: got %q, want A0A1", resultA)
	}
	if resultB == nil || string(resultB) != "B0B1" {
		t.Errorf("msgB: got %q, want B0B1", resultB)
	}
}

func TestReassemble_BufferCleanedAfterComplete(t *testing.T) {
	c := NewWSClient("app", "secret", "http://localhost", nil)

	c.reassemble("msg_clean", 2, 0, []byte("x"))
	c.reassemble("msg_clean", 2, 1, []byte("y"))

	// After completion the key is deleted; sending the same msgID again
	// starts a fresh buffer.
	result := c.reassemble("msg_clean", 2, 0, []byte("fresh"))
	if result != nil {
		t.Error("fresh start with 2 frags: first frag should not complete")
	}
}

// --- NewWSClient constructor ---

func TestNewWSClient_Defaults(t *testing.T) {
	c := NewWSClient("myapp", "mysecret", "https://open.larksuite.com", nil)
	if c.appID != "myapp" {
		t.Errorf("appID: got %q", c.appID)
	}
	if c.pingInterval != defaultPingInterval {
		t.Errorf("pingInterval: got %v, want %v", c.pingInterval, defaultPingInterval)
	}
	if c.reconnectMax != -1 {
		t.Errorf("reconnectMax: got %d, want -1 (infinite)", c.reconnectMax)
	}
	if c.fragments == nil {
		t.Error("fragments map must be initialised")
	}
}

// --- Stop idempotent ---

func TestWSClient_Stop_Idempotent(t *testing.T) {
	c := NewWSClient("app", "secret", "http://localhost", nil)
	c.mu.Lock()
	c.stopCh = make(chan struct{})
	c.stopped = false
	c.mu.Unlock()

	// First Stop should not panic
	c.Stop()
	// Second Stop should also not panic
	c.Stop()
}

// --- getWSEndpoint via httptest ---

func TestGetWSEndpoint_Success(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	var wsURL string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/callback/ws/endpoint" {
			// Return a WS URL pointing back to this server
			wsURL = "ws://" + r.Host + "/ws"
			resp := map[string]any{
				"code": 0,
				"msg":  "ok",
				"data": map[string]any{
					"URL": wsURL,
					"ClientConfig": map[string]any{
						"PingInterval":      30,
						"ReconnectCount":    5,
						"ReconnectInterval": 3,
						"ReconnectNonce":    10,
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		if r.URL.Path == "/ws" {
			upgrader.Upgrade(w, r, nil) //nolint
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := NewWSClient("app", "secret", srv.URL, nil)
	ctx := context.Background()
	url, err := c.getWSEndpoint(ctx)
	if err != nil {
		t.Fatalf("getWSEndpoint returned error: %v", err)
	}
	if !strings.HasPrefix(url, "ws://") {
		t.Errorf("expected ws:// URL, got %q", url)
	}
	// Config should be applied
	if c.pingInterval != 30*time.Second {
		t.Errorf("pingInterval not applied: got %v", c.pingInterval)
	}
	if c.reconnectMax != 5 {
		t.Errorf("reconnectMax not applied: got %d", c.reconnectMax)
	}
}

func TestGetWSEndpoint_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"code": 10001,
			"msg":  "invalid credentials",
			"data": map[string]any{},
		})
	}))
	defer srv.Close()

	c := NewWSClient("bad-app", "bad-secret", srv.URL, nil)
	_, err := c.getWSEndpoint(context.Background())
	if err == nil {
		t.Fatal("expected error for non-zero code")
	}
}

func TestGetWSEndpoint_NetworkError(t *testing.T) {
	c := NewWSClient("app", "secret", "http://localhost:1", nil)
	_, err := c.getWSEndpoint(context.Background())
	if err == nil {
		t.Fatal("expected error for unreachable server")
	}
}

// --- handleFrame: pong updates config ---

func TestHandleFrame_PongUpdatesConfig(t *testing.T) {
	c := NewWSClient("app", "secret", "http://localhost", nil)
	c.mu.Lock()
	c.stopCh = make(chan struct{})
	c.mu.Unlock()

	pongPayload, _ := json.Marshal(map[string]any{
		"PingInterval":      60,
		"ReconnectCount":    3,
		"ReconnectInterval": 10,
		"ReconnectNonce":    5,
	})

	frame := &wsFrame{
		Method:  frameTypeControl,
		Headers: []wsHeader{{Key: "type", Value: "pong"}},
		Payload: pongPayload,
	}

	c.handleFrame(context.Background(), frame)

	if c.pingInterval != 60*time.Second {
		t.Errorf("pingInterval: got %v, want 60s", c.pingInterval)
	}
	if c.reconnectMax != 3 {
		t.Errorf("reconnectMax: got %d, want 3", c.reconnectMax)
	}
	if c.reconnectInterval != 10*time.Second {
		t.Errorf("reconnectInterval: got %v, want 10s", c.reconnectInterval)
	}
	if c.reconnectNonce != 5 {
		t.Errorf("reconnectNonce: got %d, want 5", c.reconnectNonce)
	}
}

// --- handleFrame: data event dispatches to handler ---

type captureHandler struct {
	payloads [][]byte
}

func (h *captureHandler) HandleEvent(_ context.Context, payload []byte) error {
	h.payloads = append(h.payloads, payload)
	return nil
}

func TestHandleFrame_DataEvent_Dispatched(t *testing.T) {
	handler := &captureHandler{}
	c := NewWSClient("app", "secret", "http://localhost", handler)
	c.mu.Lock()
	c.stopCh = make(chan struct{})
	c.mu.Unlock()

	eventPayload := []byte(`{"schema":"2.0","header":{"event_type":"im.message.receive_v1"}}`)
	frame := &wsFrame{
		SeqID:   1,
		Method:  frameTypeData,
		Headers: []wsHeader{{Key: "type", Value: "event"}},
		Payload: eventPayload,
	}

	c.handleFrame(context.Background(), frame)

	if len(handler.payloads) != 1 {
		t.Fatalf("expected 1 dispatched event, got %d", len(handler.payloads))
	}
	if !bytes.Equal(handler.payloads[0], eventPayload) {
		t.Errorf("payload mismatch: got %q", handler.payloads[0])
	}
}

func TestHandleFrame_NonEventDataFrame_Ignored(t *testing.T) {
	handler := &captureHandler{}
	c := NewWSClient("app", "secret", "http://localhost", handler)
	c.mu.Lock()
	c.stopCh = make(chan struct{})
	c.mu.Unlock()

	// "card" type should be ignored
	frame := &wsFrame{
		Method:  frameTypeData,
		Headers: []wsHeader{{Key: "type", Value: "card"}},
		Payload: []byte(`{}`),
	}

	c.handleFrame(context.Background(), frame)

	if len(handler.payloads) != 0 {
		t.Errorf("non-event frame should not be dispatched, got %d events", len(handler.payloads))
	}
}

func TestHandleFrame_DataEvent_Fragmented(t *testing.T) {
	handler := &captureHandler{}
	c := NewWSClient("app", "secret", "http://localhost", handler)
	c.mu.Lock()
	c.stopCh = make(chan struct{})
	c.mu.Unlock()

	part1 := []byte(`{"half":`)
	part2 := []byte(`"data"}`)

	// Send fragment 0 of 2 — should not dispatch
	frame1 := &wsFrame{
		Method: frameTypeData,
		Headers: []wsHeader{
			{Key: "type", Value: "event"},
			{Key: "message_id", Value: "frag_evt_1"},
			{Key: "sum", Value: "2"},
			{Key: "seq", Value: "0"},
		},
		Payload: part1,
	}
	c.handleFrame(context.Background(), frame1)
	if len(handler.payloads) != 0 {
		t.Error("partial fragment should not dispatch")
	}

	// Send fragment 1 of 2 — should complete and dispatch
	frame2 := &wsFrame{
		Method: frameTypeData,
		Headers: []wsHeader{
			{Key: "type", Value: "event"},
			{Key: "message_id", Value: "frag_evt_1"},
			{Key: "sum", Value: "2"},
			{Key: "seq", Value: "1"},
		},
		Payload: part2,
	}
	c.handleFrame(context.Background(), frame2)
	if len(handler.payloads) != 1 {
		t.Fatalf("completed fragments should dispatch once, got %d", len(handler.payloads))
	}
	combined := string(handler.payloads[0])
	if combined != `{"half":"data"}` {
		t.Errorf("reassembled payload: got %q", combined)
	}
}
