package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

// --- jsonrpcError tests ---

func TestJsonrpcError_Error(t *testing.T) {
	e := &jsonrpcError{Code: -32601, Message: "method not found"}
	got := e.Error()
	if !strings.Contains(got, "-32601") {
		t.Errorf("expected code in error string, got %q", got)
	}
	if !strings.Contains(got, "method not found") {
		t.Errorf("expected message in error string, got %q", got)
	}
}

// --- Conn read loop + dispatch tests ---

// makeConnPair creates two in-memory Conn pairs communicating via io.Pipe.
// Returns (clientConn, serverReader, serverWriter) where:
//   - clientConn reads from serverWriter and writes to serverReader
//   - the test can inject messages by writing to serverWriter
//   - the test can read messages by reading from serverReader
func makeLoopbackConn(handler RequestHandler, notify NotifyHandler) (*Conn, io.Writer, io.ReadCloser) {
	// client writes → serverR reads (for the test to inspect outbound)
	serverR, clientW := io.Pipe()
	// serverW writes → client reads (for the test to inject inbound)
	clientR, serverW := io.Pipe()

	conn := NewConn(clientW, clientR, handler, notify)
	return conn, serverW, serverR
}

func TestNewConn_NotNil(t *testing.T) {
	pr, pw := io.Pipe()
	defer pr.Close()
	defer pw.Close()
	conn := NewConn(pw, pr, nil, nil)
	if conn == nil {
		t.Fatal("expected non-nil Conn")
	}
}

func TestConn_Done_ClosedOnReaderEOF(t *testing.T) {
	pr, pw := io.Pipe()
	conn := NewConn(io.Discard, pr, nil, nil)
	conn.Start()

	// Close the write end to signal EOF → read loop exits → done closed
	pw.Close()

	select {
	case <-conn.Done():
		// expected
	case <-time.After(2 * time.Second):
		t.Fatal("expected Done to be closed after reader EOF")
	}
}

func TestConn_Notify_WritesMessage(t *testing.T) {
	// We only need a writer; reader can be /dev/null equiv
	pr, pw := io.Pipe()
	defer pr.Close()

	// Write to a buffer-based pipe so we can inspect
	outR, outW := io.Pipe()

	conn := NewConn(outW, pr, nil, nil)
	// Don't start read loop — we're only testing write path

	done := make(chan []byte, 1)
	go func() {
		data, _ := io.ReadAll(outR)
		done <- data
	}()

	err := conn.Notify("test/event", map[string]string{"key": "val"})
	if err != nil {
		t.Fatalf("Notify error: %v", err)
	}
	outW.Close()
	pw.Close()

	select {
	case data := <-done:
		if !strings.Contains(string(data), "test/event") {
			t.Errorf("expected method in output, got %q", string(data))
		}
		if !strings.Contains(string(data), `"jsonrpc":"2.0"`) {
			t.Errorf("expected jsonrpc version in output, got %q", string(data))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout reading notify output")
	}
}

func TestConn_Call_ResponseDispatched(t *testing.T) {
	// serverW → clientR (injecting responses); clientW → serverR (reading requests)
	clientR, serverW := io.Pipe()
	serverR, clientW := io.Pipe()

	conn := NewConn(clientW, clientR, nil, nil)
	conn.Start()

	// Read the request the client sends, then write the response back.
	go func() {
		buf := make([]byte, 4096)
		n, err := serverR.Read(buf)
		if err != nil {
			return
		}
		var req jsonrpcMessage
		json.Unmarshal(buf[:n], &req)

		// Send response with same ID
		resp := jsonrpcMessage{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  json.RawMessage(`{"sessionId":"sess-1"}`),
		}
		data, _ := json.Marshal(resp)
		data = append(data, '\n')
		serverW.Write(data)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var result NewSessionResponse
	err := conn.Call(ctx, "session/new", NewSessionRequest{}, &result)
	if err != nil {
		t.Fatalf("Call error: %v", err)
	}
	if result.SessionID != "sess-1" {
		t.Errorf("expected sess-1, got %q", result.SessionID)
	}

	serverW.Close()
	serverR.Close()
}

func TestConn_Call_ErrorResponse(t *testing.T) {
	clientR, serverW := io.Pipe()
	serverR, clientW := io.Pipe()

	conn := NewConn(clientW, clientR, nil, nil)
	conn.Start()

	go func() {
		buf := make([]byte, 4096)
		n, _ := serverR.Read(buf)
		var req jsonrpcMessage
		json.Unmarshal(buf[:n], &req)

		resp := jsonrpcMessage{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &jsonrpcError{Code: -32600, Message: "invalid request"},
		}
		data, _ := json.Marshal(resp)
		data = append(data, '\n')
		serverW.Write(data)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := conn.Call(ctx, "bad/method", nil, nil)
	if err == nil {
		t.Fatal("expected error response, got nil")
	}
	if !strings.Contains(err.Error(), "invalid request") {
		t.Errorf("expected 'invalid request' in error, got %q", err.Error())
	}

	serverW.Close()
	serverR.Close()
}

func TestConn_Call_ContextCancel(t *testing.T) {
	// Server accepts writes but never sends responses.
	clientR, serverW := io.Pipe()
	serverR, clientW := io.Pipe()

	conn := NewConn(clientW, clientR, nil, nil)
	conn.Start()

	// Drain the client writes so writeMessage doesn't block; never send a response.
	go func() {
		buf := make([]byte, 4096)
		for {
			_, err := serverR.Read(buf)
			if err != nil {
				return
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := conn.Call(ctx, "slow/method", nil, nil)
	if err == nil {
		t.Fatal("expected context timeout error")
	}

	serverW.Close()
	serverR.Close()
}

func TestConn_Call_ConnectionClosed(t *testing.T) {
	clientR, serverW := io.Pipe()
	serverR, clientW := io.Pipe()

	conn := NewConn(clientW, clientR, nil, nil)
	conn.Start()

	// Close the server write end immediately — causes EOF → done closed
	go func() {
		// Drain the request first so the client write doesn't block
		buf := make([]byte, 4096)
		serverR.Read(buf)
		serverW.Close()
	}()

	ctx := context.Background()
	err := conn.Call(ctx, "any/method", nil, nil)
	if err == nil {
		t.Fatal("expected error after connection close")
	}
}

func TestConn_ReadLoop_SkipsMalformedJSON(t *testing.T) {
	clientR, serverW := io.Pipe()
	serverR, clientW := io.Pipe()
	_ = serverR

	var notifyCalled bool
	notifyDone := make(chan struct{})
	notify := func(method string, params json.RawMessage) {
		if method == "good/event" {
			notifyCalled = true
			close(notifyDone)
		}
	}

	conn := NewConn(clientW, clientR, nil, notify)
	conn.Start()

	// Send malformed line, then valid notification
	serverW.Write([]byte("{{not valid json}}\n"))
	goodMsg := jsonrpcMessage{JSONRPC: "2.0", Method: "good/event", Params: json.RawMessage(`{}`)}
	data, _ := json.Marshal(goodMsg)
	serverW.Write(append(data, '\n'))

	select {
	case <-notifyDone:
		if !notifyCalled {
			t.Error("expected notify to be called after malformed line")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for good/event notification")
	}

	serverW.Close()
	clientW.Close()
}

func TestConn_ReadLoop_EmptyLines(t *testing.T) {
	clientR, serverW := io.Pipe()
	_, clientW := io.Pipe()

	notifyDone := make(chan struct{})
	notify := func(method string, params json.RawMessage) {
		if method == "ping" {
			close(notifyDone)
		}
	}

	conn := NewConn(clientW, clientR, nil, notify)
	conn.Start()

	// Send empty lines then a valid notification
	serverW.Write([]byte("\n\n\n"))
	msg := jsonrpcMessage{JSONRPC: "2.0", Method: "ping"}
	data, _ := json.Marshal(msg)
	serverW.Write(append(data, '\n'))

	select {
	case <-notifyDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout: notification after empty lines not received")
	}
	serverW.Close()
}

func TestConn_ReadLoop_HandlesRequestFromAgent(t *testing.T) {
	clientR, serverW := io.Pipe()
	serverR, clientW := io.Pipe()

	handlerCalled := make(chan string, 1)
	handler := func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		handlerCalled <- method
		return map[string]string{"result": "ok"}, nil
	}

	conn := NewConn(clientW, clientR, handler, nil)
	conn.Start()

	// Agent sends a request (has ID + method)
	id := int64(99)
	req := jsonrpcMessage{
		JSONRPC: "2.0",
		ID:      &id,
		Method:  "fs/readTextFile",
		Params:  json.RawMessage(`{"path":"/tmp/test"}`),
	}
	data, _ := json.Marshal(req)
	serverW.Write(append(data, '\n'))

	select {
	case method := <-handlerCalled:
		if method != "fs/readTextFile" {
			t.Errorf("expected fs/readTextFile, got %q", method)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout: handler not called")
	}

	// Drain the response
	go func() {
		buf := make([]byte, 4096)
		serverR.Read(buf)
	}()

	serverW.Close()
}

func TestConn_ReadLoop_NoHandlerReturnsError(t *testing.T) {
	clientR, serverW := io.Pipe()
	serverR, clientW := io.Pipe()

	conn := NewConn(clientW, clientR, nil, nil) // no handler
	conn.Start()

	id := int64(1)
	req := jsonrpcMessage{
		JSONRPC: "2.0",
		ID:      &id,
		Method:  "fs/readTextFile",
		Params:  json.RawMessage(`{}`),
	}
	data, _ := json.Marshal(req)
	serverW.Write(append(data, '\n'))

	// Read the error response
	respDone := make(chan *jsonrpcMessage, 1)
	go func() {
		buf := make([]byte, 4096)
		n, _ := serverR.Read(buf)
		var msg jsonrpcMessage
		json.Unmarshal(buf[:n], &msg)
		respDone <- &msg
	}()

	select {
	case resp := <-respDone:
		if resp.Error == nil {
			t.Error("expected error response when no handler registered")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for error response")
	}

	serverW.Close()
}

func TestConn_Notify_MarshalError(t *testing.T) {
	// Use a writer that always errors to test error return
	pr, _ := io.Pipe()
	pw := &errorWriter{}
	conn := NewConn(pw, pr, nil, nil)

	err := conn.Notify("test", map[string]string{"k": "v"})
	if err == nil {
		t.Error("expected error from failing writer")
	}
}

// errorWriter always returns an error on Write.
type errorWriter struct{}

func (w *errorWriter) Write(p []byte) (int, error) {
	return 0, fmt.Errorf("forced write error")
}

func TestConn_Call_NilResult(t *testing.T) {
	clientR, serverW := io.Pipe()
	serverR, clientW := io.Pipe()

	conn := NewConn(clientW, clientR, nil, nil)
	conn.Start()

	go func() {
		buf := make([]byte, 4096)
		n, _ := serverR.Read(buf)
		var req jsonrpcMessage
		json.Unmarshal(buf[:n], &req)
		resp := jsonrpcMessage{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  json.RawMessage(`{}`),
		}
		data, _ := json.Marshal(resp)
		serverW.Write(append(data, '\n'))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// nil result pointer — should not panic
	err := conn.Call(ctx, "ping", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	serverW.Close()
	serverR.Close()
}

// TestConn_ReadLoop_MalformedInputNoPanic verifies adversarial inputs don't panic.
func TestConn_ReadLoop_MalformedInputNoPanic(t *testing.T) {
	adversarialInputs := []string{
		"null\n",
		"[]\n",
		`{"id":null,"method":null}\n`,
		strings.Repeat("x", 1024) + "\n",
		`{"jsonrpc":"2.0","id":1,"method":` + strings.Repeat(`"a"`, 100) + `}\n`,
		"\x00\x01\x02\x03\n",
		`{"jsonrpc":"2.0","result":` + strings.Repeat("[", 100) + "\n",
	}

	for _, input := range adversarialInputs {
		t.Run("input_"+input[:min(10, len(input))], func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("panic on input %q: %v", input[:min(20, len(input))], r)
				}
			}()

			clientR, serverW := io.Pipe()
			_, clientW := io.Pipe()

			conn := NewConn(clientW, clientR, nil, nil)
			conn.Start()

			serverW.Write([]byte(input))
			// Small delay for read loop to process
			time.Sleep(10 * time.Millisecond)
			serverW.Close()
		})
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestConn_IDIncrement(t *testing.T) {
	// Verify each Call gets a unique ID by capturing two requests
	clientR, serverW := io.Pipe()
	serverR, clientW := io.Pipe()

	conn := NewConn(clientW, clientR, nil, nil)
	conn.Start()

	ids := make(chan int64, 2)

	// Goroutine that reads requests and responds
	go func() {
		for i := 0; i < 2; i++ {
			buf := make([]byte, 4096)
			n, _ := serverR.Read(buf)
			var req jsonrpcMessage
			json.Unmarshal(buf[:n], &req)
			if req.ID != nil {
				ids <- *req.ID
			}
			resp := jsonrpcMessage{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  json.RawMessage(`{}`),
			}
			data, _ := json.Marshal(resp)
			serverW.Write(append(data, '\n'))
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conn.Call(ctx, "m1", nil, nil)
	conn.Call(ctx, "m2", nil, nil)

	id1 := <-ids
	id2 := <-ids
	if id1 == id2 {
		t.Errorf("expected different IDs, got %d and %d", id1, id2)
	}

	serverW.Close()
	serverR.Close()
}
